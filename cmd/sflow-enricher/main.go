package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"sflow-enricher/internal/config"
	"sflow-enricher/internal/sflow"
)

const (
	maxPacketSize = 65535
	bufferPoolSize = 1000
	version = "2.0.0"
)

// Stats holds packet statistics
type Stats struct {
	StartTime        time.Time
	PacketsReceived  uint64
	PacketsForwarded uint64
	PacketsEnriched  uint64
	PacketsDropped   uint64
	PacketsFiltered  uint64
	BytesReceived    uint64
	BytesForwarded   uint64
}

// DestinationStats holds per-destination statistics
type DestinationStats struct {
	Name           string
	Address        string
	Healthy        bool
	LastCheck      time.Time
	LastError      string
	PacketsSent    uint64
	PacketsDropped uint64
	BytesSent      uint64
}

// Destination represents a forwarding destination
type Destination struct {
	Config      config.DestinationConfig
	Conn        *net.UDPConn
	Addr        *net.UDPAddr
	Stats       DestinationStats
	Healthy     atomic.Bool
	FailoverDst *Destination
	mu          sync.RWMutex
}

var (
	stats       Stats
	destinations []*Destination
	cfg         *config.Config
	configPath  string
	debugMode   bool
	logJSON     bool
	bufferPool  sync.Pool
)

func init() {
	bufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, maxPacketSize)
			return &buf
		},
	}
}

// sdNotify sends a notification to systemd via the notify socket
func sdNotify(state string) {
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return // Not running under systemd
	}

	conn, err := net.Dial("unixgram", socketPath)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.Write([]byte(state))
}

// sdReady notifies systemd that the service is ready
func sdReady() {
	sdNotify("READY=1")
	logInfo("Systemd notified: READY", nil)
}

// sdWatchdog sends the heartbeat
func sdWatchdog() {
	sdNotify("WATCHDOG=1")
}

// sdStopping notifies the start of shutdown
func sdStopping() {
	sdNotify("STOPPING=1")
}

// startWatchdog starts the systemd watchdog heartbeat goroutine
func startWatchdog() {
	// Read interval from environment
	watchdogUSec := os.Getenv("WATCHDOG_USEC")
	if watchdogUSec == "" {
		return // Watchdog not configured
	}

	usec, err := strconv.ParseInt(watchdogUSec, 10, 64)
	if err != nil || usec <= 0 {
		return
	}

	// Notify at half the interval (best practice)
	interval := time.Duration(usec/2) * time.Microsecond
	logInfo("Watchdog started", map[string]interface{}{
		"interval": interval.String(),
	})

	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			sdWatchdog()
		}
	}()
}

func main() {
	flag.StringVar(&configPath, "config", "/etc/sflow-enricher/config.yaml", "Path to config file")
	flag.BoolVar(&debugMode, "debug", false, "Enable debug logging")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("sflow-enricher version %s\n", version)
		os.Exit(0)
	}

	// Load configuration
	var err error
	cfg, err = config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logJSON = cfg.Logging.Format == "json"
	stats.StartTime = time.Now()

	logInfo("sFlow ASN Enricher starting", map[string]interface{}{
		"version": version,
		"config":  configPath,
	})

	logInfo("Configuration loaded", map[string]interface{}{
		"listen":      cfg.ListenAddr(),
		"rules_count": len(cfg.Enrichment.Rules),
		"log_format":  cfg.Logging.Format,
	})

	for _, rule := range cfg.Enrichment.Rules {
		logInfo("Enrichment rule", map[string]interface{}{
			"name":     rule.Name,
			"network":  rule.Network,
			"match_as": rule.MatchAS,
			"set_as":   rule.SetAS,
		})
	}

	// Setup destinations
	if err := setupDestinations(); err != nil {
		log.Fatalf("Failed to setup destinations: %v", err)
	}

	// Setup listener
	listenAddr, err := net.ResolveUDPAddr("udp", cfg.ListenAddr())
	if err != nil {
		log.Fatalf("Failed to resolve listen address: %v", err)
	}

	listener, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", cfg.ListenAddr(), err)
	}
	defer listener.Close()

	// Increase socket buffer
	listener.SetReadBuffer(4 * 1024 * 1024) // 4MB

	logInfo("Listening", map[string]interface{}{"address": cfg.ListenAddr()})

	// Start HTTP server for metrics and status
	if cfg.HTTP.Enabled {
		go startHTTPServer()
	}

	// Start health checker
	go healthChecker()

	// Start stats reporter
	go statsReporter(cfg.Logging.StatsInterval)

	// Notify systemd that service is ready
	sdReady()
	startWatchdog()

	// Send startup notification
	startupDetails := fmt.Sprintf("Service started on `%s`\n📦 Version: `%s`\n📋 Rules:",
		cfg.ListenAddr(), version)
	for _, rule := range cfg.Enrichment.Rules {
		startupDetails += fmt.Sprintf("\n   • `%s` → AS%d", rule.Name, rule.SetAS)
	}
	startupDetails += "\n🎯 Destinations:"
	for _, dest := range destinations {
		startupDetails += fmt.Sprintf("\n   • `%s` (%s)", dest.Config.Name, dest.Stats.Address)
	}
	sendTelegramAlert("startup", startupDetails)

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Start packet processing
	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		processPackets(listener, stopChan)
	}()

	// Signal handling loop
	for sig := range sigChan {
		switch sig {
		case syscall.SIGHUP:
			logInfo("Received SIGHUP, reloading configuration", nil)
			if err := cfg.Reload(configPath); err != nil {
				logError("Failed to reload config", err, nil)
			} else {
				logInfo("Configuration reloaded", map[string]interface{}{
					"rules_count": len(cfg.Enrichment.Rules),
				})
			}
		case syscall.SIGINT, syscall.SIGTERM:
			sdStopping()
			logInfo("Received shutdown signal", map[string]interface{}{"signal": sig.String()})

			// Build detailed shutdown message
			destStats := ""
			for _, dest := range destinations {
				statusIcon := "✅"
				if !dest.Healthy.Load() {
					statusIcon = "❌"
				}
				destStats += fmt.Sprintf("\n   %s `%s`: %d pkts", statusIcon, dest.Config.Name,
					atomic.LoadUint64(&dest.Stats.PacketsSent))
			}

			shutdownDetails := fmt.Sprintf("Service shutting down\n"+
				"⏱️ Uptime: `%s`\n"+
				"📥 Received: `%d`\n"+
				"✅ Enriched: `%d`\n"+
				"❌ Dropped: `%d`\n"+
				"🎯 Destinations:%s",
				time.Since(stats.StartTime).Round(time.Second),
				atomic.LoadUint64(&stats.PacketsReceived),
				atomic.LoadUint64(&stats.PacketsEnriched),
				atomic.LoadUint64(&stats.PacketsDropped),
				destStats)

			// Blocking call to ensure Telegram notification is sent before shutdown
			sendTelegramAlertWithWait("shutdown", shutdownDetails, true)
			close(stopChan)
			listener.Close()
			for _, dest := range destinations {
				dest.Conn.Close()
			}
			wg.Wait()
			printFinalStats()
			return
		}
	}
}

func setupDestinations() error {
	destMap := make(map[string]*Destination)

	for _, destCfg := range cfg.Destinations {
		if !destCfg.Enabled {
			continue
		}

		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", destCfg.Address, destCfg.Port))
		if err != nil {
			return fmt.Errorf("failed to resolve destination %s: %w", destCfg.Name, err)
		}

		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			return fmt.Errorf("failed to create connection to %s: %w", destCfg.Name, err)
		}

		// Increase socket buffer
		conn.SetWriteBuffer(2 * 1024 * 1024) // 2MB

		dest := &Destination{
			Config: destCfg,
			Conn:   conn,
			Addr:   addr,
			Stats: DestinationStats{
				Name:    destCfg.Name,
				Address: fmt.Sprintf("%s:%d", destCfg.Address, destCfg.Port),
				Healthy: true,
			},
		}
		dest.Healthy.Store(true)

		destinations = append(destinations, dest)
		destMap[destCfg.Name] = dest

		logInfo("Destination configured", map[string]interface{}{
			"name":    destCfg.Name,
			"address": dest.Stats.Address,
			"primary": destCfg.Primary,
		})
	}

	// Setup failover links
	for _, dest := range destinations {
		if dest.Config.Failover != "" {
			if failover, ok := destMap[dest.Config.Failover]; ok {
				dest.FailoverDst = failover
				logInfo("Failover configured", map[string]interface{}{
					"primary":  dest.Config.Name,
					"failover": dest.Config.Failover,
				})
			}
		}
	}

	if len(destinations) == 0 {
		return fmt.Errorf("no enabled destinations configured")
	}

	return nil
}

func processPackets(listener *net.UDPConn, stopChan chan struct{}) {
	for {
		select {
		case <-stopChan:
			return
		default:
		}

		// Get buffer from pool
		bufPtr := bufferPool.Get().(*[]byte)
		buffer := *bufPtr

		listener.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := listener.ReadFromUDP(buffer)
		if err != nil {
			bufferPool.Put(bufPtr)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				return
			}
			logError("Read error", err, nil)
			continue
		}

		atomic.AddUint64(&stats.PacketsReceived, 1)
		atomic.AddUint64(&stats.BytesReceived, uint64(n))

		// Check whitelist
		if !cfg.IsWhitelisted(remoteAddr.IP) {
			atomic.AddUint64(&stats.PacketsFiltered, 1)
			bufferPool.Put(bufPtr)
			if debugMode {
				logDebug("Packet filtered (not whitelisted)", map[string]interface{}{
					"source": remoteAddr.IP.String(),
				})
			}
			continue
		}

		// Make a copy of the packet for modification
		packet := make([]byte, n)
		copy(packet, buffer[:n])
		bufferPool.Put(bufPtr)

		// Process and enrich the packet
		packet, enriched := enrichPacket(packet, remoteAddr)

		// Forward to all destinations (use potentially resized packet)
		for _, dest := range destinations {
			sendToDestination(dest, packet, len(packet), enriched)
		}
	}
}

func sendToDestination(dest *Destination, packet []byte, n int, enriched bool) {
	// Check if destination is healthy, use failover if not
	targetDest := dest
	if !dest.Healthy.Load() && dest.FailoverDst != nil && dest.FailoverDst.Healthy.Load() {
		targetDest = dest.FailoverDst
		if debugMode {
			logDebug("Using failover destination", map[string]interface{}{
				"primary":  dest.Config.Name,
				"failover": targetDest.Config.Name,
			})
		}
	}

	_, err := targetDest.Conn.Write(packet)
	if err != nil {
		atomic.AddUint64(&targetDest.Stats.PacketsDropped, 1)
		atomic.AddUint64(&stats.PacketsDropped, 1)
		targetDest.mu.Lock()
		targetDest.Stats.LastError = err.Error()
		targetDest.mu.Unlock()
		if debugMode {
			logError("Forward error", err, map[string]interface{}{
				"destination": targetDest.Config.Name,
			})
		}
	} else {
		atomic.AddUint64(&targetDest.Stats.PacketsSent, 1)
		atomic.AddUint64(&targetDest.Stats.BytesSent, uint64(n))
		atomic.AddUint64(&stats.PacketsForwarded, 1)
		atomic.AddUint64(&stats.BytesForwarded, uint64(n))
	}

	if enriched {
		atomic.AddUint64(&stats.PacketsEnriched, 1)
	}
}

func enrichPacket(packet []byte, remoteAddr *net.UDPAddr) ([]byte, bool) {
	datagram, err := sflow.Parse(packet)
	if err != nil {
		if debugMode {
			logError("Parse error", err, map[string]interface{}{
				"source": remoteAddr.String(),
			})
		}
		return packet, false
	}

	enriched := false
	rules := cfg.GetEnrichmentRules()

	// CRITICAL: Process samples in REVERSE ORDER to handle packet resizing correctly.
	// When ModifyDstAS inserts 12 bytes into a sample, it shifts all subsequent data.
	// By processing from last to first, we ensure earlier sample offsets remain valid.
	// This is the correct approach for XDR variable-length data modification.
	for i := len(datagram.Samples) - 1; i >= 0; i-- {
		sample := datagram.Samples[i]
		if sample.Enterprise != 0 {
			continue
		}

		var expanded bool
		switch sample.Format {
		case sflow.SampleTypeFlowSample:
			expanded = false
		case sflow.SampleTypeExpandedFlowSample:
			expanded = true
		default:
			continue
		}

		flowSample, err := sflow.ParseFlowSample(sample.Data, expanded)
		if err != nil {
			if debugMode {
				logError("Flow sample parse error", err, nil)
			}
			continue
		}

		// Find source and destination IP from raw packet header
		var srcIP, dstIP net.IP
		for _, record := range flowSample.Records {
			if record.Enterprise == 0 && record.Format == sflow.FlowRecordRawPacketHeader {
				srcIP, dstIP = sflow.GetSrcDstIPFromRawPacket(record.Data)
				break
			}
		}

		// Process extended gateway records
		for _, record := range flowSample.Records {
			if record.Enterprise != 0 || record.Format != sflow.FlowRecordExtendedGateway {
				continue
			}

			eg, err := sflow.ParseExtendedGateway(record.Data)
			if err != nil {
				if debugMode {
					logError("Extended gateway parse error", err, nil)
				}
				continue
			}

			// Check enrichment rules for SrcAS (outbound traffic)
			for _, rule := range rules {
				// Check if we should apply this rule for SrcAS
				shouldApply := false
				if rule.Overwrite {
					// Always apply if IP matches
					shouldApply = srcIP != nil && rule.IPNet.Contains(srcIP)
				} else {
					// Only apply if AS matches and IP matches
					shouldApply = eg.SrcAS == rule.MatchAS && srcIP != nil && rule.IPNet.Contains(srcIP)
				}

				if shouldApply {
					if debugMode {
						logDebug("Enriching SrcAS", map[string]interface{}{
							"src_ip": srcIP.String(),
							"old_as": eg.SrcAS,
							"new_as": rule.SetAS,
							"rule":   rule.Name,
						})
					}
					sflow.ModifySrcAS(packet, sample.Offset, record.Offset, rule.SetAS)
					enriched = true
				}
			}

			// Check enrichment rules for DstAS (inbound traffic)
			// Only enrich if DstASPath is empty (DstASPathLen == 0)
			if eg.DstASPathLen == 0 {
				for _, rule := range rules {
					// Check if destination IP matches the rule's network
					if dstIP != nil && rule.IPNet.Contains(dstIP) {
						if debugMode {
							logDebug("Enriching DstAS", map[string]interface{}{
								"dst_ip": dstIP.String(),
								"new_as": rule.SetAS,
								"rule":   rule.Name,
							})
						}
						// ModifyDstAS returns a new packet (resized) and success flag
						newPacket, ok := sflow.ModifyDstAS(packet, sample.Offset, record.Offset, rule.SetAS)
						if ok {
							packet = newPacket
							enriched = true
						}
						break // Only apply first matching rule for DstAS
					}
				}
			}
		}
	}

	return packet, enriched
}

func healthChecker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, dest := range destinations {
			checkDestinationHealth(dest)
		}
	}
}

func checkDestinationHealth(dest *Destination) {
	// Simple health check: try to resolve the address
	addr := fmt.Sprintf("%s:%d", dest.Config.Address, dest.Config.Port)
	conn, err := net.DialTimeout("udp", addr, 5*time.Second)

	wasHealthy := dest.Healthy.Load()

	if err != nil {
		dest.Healthy.Store(false)
		dest.mu.Lock()
		dest.Stats.Healthy = false
		dest.Stats.LastCheck = time.Now()
		dest.Stats.LastError = err.Error()
		dest.mu.Unlock()

		if wasHealthy {
			logError("Destination unhealthy", err, map[string]interface{}{
				"destination": dest.Config.Name,
			})
			sendTelegramAlert("destination_down", fmt.Sprintf("Destination %s is DOWN: %v", dest.Config.Name, err))
		}
	} else {
		conn.Close()
		dest.Healthy.Store(true)
		dest.mu.Lock()
		dest.Stats.Healthy = true
		dest.Stats.LastCheck = time.Now()
		dest.Stats.LastError = ""
		dest.mu.Unlock()

		if !wasHealthy {
			logInfo("Destination healthy", map[string]interface{}{
				"destination": dest.Config.Name,
			})
			sendTelegramAlert("destination_down", fmt.Sprintf("Destination %s is UP", dest.Config.Name))
		}
	}
}

// HTTP Server for metrics and status
func startHTTPServer() {
	http.HandleFunc("/metrics", prometheusMetricsHandler)
	http.HandleFunc("/status", statusHandler)
	http.HandleFunc("/health", healthHandler)

	logInfo("HTTP server starting", map[string]interface{}{"address": cfg.HTTPAddr()})

	if err := http.ListenAndServe(cfg.HTTPAddr(), nil); err != nil {
		logError("HTTP server error", err, nil)
	}
}

func prometheusMetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	fmt.Fprintf(w, "# HELP sflow_asn_enricher_packets_received_total Total packets received\n")
	fmt.Fprintf(w, "# TYPE sflow_asn_enricher_packets_received_total counter\n")
	fmt.Fprintf(w, "sflow_asn_enricher_packets_received_total %d\n", atomic.LoadUint64(&stats.PacketsReceived))

	fmt.Fprintf(w, "# HELP sflow_asn_enricher_packets_forwarded_total Total packets forwarded\n")
	fmt.Fprintf(w, "# TYPE sflow_asn_enricher_packets_forwarded_total counter\n")
	fmt.Fprintf(w, "sflow_asn_enricher_packets_forwarded_total %d\n", atomic.LoadUint64(&stats.PacketsForwarded))

	fmt.Fprintf(w, "# HELP sflow_asn_enricher_packets_enriched_total Total packets enriched\n")
	fmt.Fprintf(w, "# TYPE sflow_asn_enricher_packets_enriched_total counter\n")
	fmt.Fprintf(w, "sflow_asn_enricher_packets_enriched_total %d\n", atomic.LoadUint64(&stats.PacketsEnriched))

	fmt.Fprintf(w, "# HELP sflow_asn_enricher_packets_dropped_total Total packets dropped\n")
	fmt.Fprintf(w, "# TYPE sflow_asn_enricher_packets_dropped_total counter\n")
	fmt.Fprintf(w, "sflow_asn_enricher_packets_dropped_total %d\n", atomic.LoadUint64(&stats.PacketsDropped))

	fmt.Fprintf(w, "# HELP sflow_asn_enricher_packets_filtered_total Total packets filtered by whitelist\n")
	fmt.Fprintf(w, "# TYPE sflow_asn_enricher_packets_filtered_total counter\n")
	fmt.Fprintf(w, "sflow_asn_enricher_packets_filtered_total %d\n", atomic.LoadUint64(&stats.PacketsFiltered))

	fmt.Fprintf(w, "# HELP sflow_asn_enricher_bytes_received_total Total bytes received\n")
	fmt.Fprintf(w, "# TYPE sflow_asn_enricher_bytes_received_total counter\n")
	fmt.Fprintf(w, "sflow_asn_enricher_bytes_received_total %d\n", atomic.LoadUint64(&stats.BytesReceived))

	fmt.Fprintf(w, "# HELP sflow_asn_enricher_bytes_forwarded_total Total bytes forwarded\n")
	fmt.Fprintf(w, "# TYPE sflow_asn_enricher_bytes_forwarded_total counter\n")
	fmt.Fprintf(w, "sflow_asn_enricher_bytes_forwarded_total %d\n", atomic.LoadUint64(&stats.BytesForwarded))

	fmt.Fprintf(w, "# HELP sflow_asn_enricher_uptime_seconds Uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE sflow_asn_enricher_uptime_seconds gauge\n")
	fmt.Fprintf(w, "sflow_asn_enricher_uptime_seconds %.0f\n", time.Since(stats.StartTime).Seconds())

	// Per-destination metrics
	for _, dest := range destinations {
		labels := fmt.Sprintf(`destination="%s"`, dest.Config.Name)

		fmt.Fprintf(w, "# HELP sflow_asn_enricher_destination_packets_sent_total Packets sent to destination\n")
		fmt.Fprintf(w, "# TYPE sflow_asn_enricher_destination_packets_sent_total counter\n")
		fmt.Fprintf(w, "sflow_asn_enricher_destination_packets_sent_total{%s} %d\n", labels, atomic.LoadUint64(&dest.Stats.PacketsSent))

		fmt.Fprintf(w, "# HELP sflow_asn_enricher_destination_healthy Destination health status\n")
		fmt.Fprintf(w, "# TYPE sflow_asn_enricher_destination_healthy gauge\n")
		healthy := 0
		if dest.Healthy.Load() {
			healthy = 1
		}
		fmt.Fprintf(w, "sflow_asn_enricher_destination_healthy{%s} %d\n", labels, healthy)
	}
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := map[string]interface{}{
		"version": version,
		"uptime":  time.Since(stats.StartTime).String(),
		"stats": map[string]uint64{
			"packets_received":  atomic.LoadUint64(&stats.PacketsReceived),
			"packets_forwarded": atomic.LoadUint64(&stats.PacketsForwarded),
			"packets_enriched":  atomic.LoadUint64(&stats.PacketsEnriched),
			"packets_dropped":   atomic.LoadUint64(&stats.PacketsDropped),
			"packets_filtered":  atomic.LoadUint64(&stats.PacketsFiltered),
			"bytes_received":    atomic.LoadUint64(&stats.BytesReceived),
			"bytes_forwarded":   atomic.LoadUint64(&stats.BytesForwarded),
		},
		"destinations": []map[string]interface{}{},
	}

	destList := status["destinations"].([]map[string]interface{})
	for _, dest := range destinations {
		dest.mu.RLock()
		destStatus := map[string]interface{}{
			"name":           dest.Stats.Name,
			"address":        dest.Stats.Address,
			"healthy":        dest.Healthy.Load(),
			"packets_sent":   atomic.LoadUint64(&dest.Stats.PacketsSent),
			"packets_dropped": atomic.LoadUint64(&dest.Stats.PacketsDropped),
			"last_error":     dest.Stats.LastError,
		}
		dest.mu.RUnlock()
		destList = append(destList, destStatus)
	}
	status["destinations"] = destList

	json.NewEncoder(w).Encode(status)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	allHealthy := true
	for _, dest := range destinations {
		if !dest.Healthy.Load() {
			allHealthy = false
			break
		}
	}

	if allHealthy {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("DEGRADED"))
	}
}

// Telegram notifications
func sendTelegramAlert(alertType, message string) {
	sendTelegramAlertWithWait(alertType, message, false)
}

func sendTelegramAlertWithWait(alertType, message string, blocking bool) {
	if !cfg.Telegram.Enabled {
		return
	}

	// Check if this alert type is enabled
	enabled := false
	for _, t := range cfg.Telegram.AlertOn {
		if t == alertType {
			enabled = true
			break
		}
	}
	if !enabled {
		return
	}

	logInfo("Sending Telegram notification", map[string]interface{}{
		"type": alertType,
	})

	doSend := func() {
		hostname, _ := os.Hostname()

		// Icon based on alert type
		icon := "ℹ️"
		switch alertType {
		case "startup":
			icon = "🟢"
		case "shutdown":
			icon = "🔴"
		case "destination_down":
			icon = "🔻"
		case "destination_up":
			icon = "🔺"
		case "high_drop_rate":
			icon = "📉"
		}

		// Message format with European date DD/MM/YYYY
		fullMessage := fmt.Sprintf(
			"%s *sFlow ASN Enricher*\n"+
				"━━━━━━━━━━━━━━━━━━━━━\n"+
				"📍 *Host:* `%s`\n"+
				"🏷️ *Event:* `%s`\n"+
				"💬 *Details:* %s\n"+
				"🕐 *Time:* `%s`",
			icon, hostname, alertType, message,
			time.Now().Format("02/01/2006 15:04:05"))

		url := fmt.Sprintf(
			"https://api.telegram.org/bot%s/sendMessage",
			cfg.Telegram.BotToken)

		payload := map[string]interface{}{
			"chat_id":    cfg.Telegram.ChatID,
			"text":       fullMessage,
			"parse_mode": "Markdown",
		}

		jsonPayload, _ := json.Marshal(payload)

		resp, err := http.Post(url, "application/json",
			bytes.NewBuffer(jsonPayload))
		if err != nil {
			logError("Failed to send Telegram alert", err, nil)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			logError("Telegram API error",
				fmt.Errorf("status code: %d", resp.StatusCode), nil)
		}
	}

	// Blocking vs async
	if blocking {
		doSend()
	} else {
		go doSend()
	}
}

// Logging functions
func logInfo(msg string, fields map[string]interface{}) {
	logMessage("INFO", msg, fields)
}

func logError(msg string, err error, fields map[string]interface{}) {
	if fields == nil {
		fields = make(map[string]interface{})
	}
	if err != nil {
		fields["error"] = err.Error()
	}
	logMessage("ERROR", msg, fields)
}

func logDebug(msg string, fields map[string]interface{}) {
	if debugMode {
		logMessage("DEBUG", msg, fields)
	}
}

func logMessage(level, msg string, fields map[string]interface{}) {
	if logJSON {
		entry := map[string]interface{}{
			"timestamp": time.Now().Format(time.RFC3339),
			"level":     level,
			"message":   msg,
		}
		for k, v := range fields {
			entry[k] = v
		}
		jsonBytes, _ := json.Marshal(entry)
		fmt.Println(string(jsonBytes))
	} else {
		if fields != nil && len(fields) > 0 {
			log.Printf("[%s] %s %v", level, msg, fields)
		} else {
			log.Printf("[%s] %s", level, msg)
		}
	}
}

func statsReporter(intervalSeconds int) {
	if intervalSeconds <= 0 {
		intervalSeconds = 60
	}

	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		printStats()
	}
}

func printStats() {
	logInfo("Statistics", map[string]interface{}{
		"received":  atomic.LoadUint64(&stats.PacketsReceived),
		"forwarded": atomic.LoadUint64(&stats.PacketsForwarded),
		"enriched":  atomic.LoadUint64(&stats.PacketsEnriched),
		"dropped":   atomic.LoadUint64(&stats.PacketsDropped),
		"filtered":  atomic.LoadUint64(&stats.PacketsFiltered),
		"bytes_in":  atomic.LoadUint64(&stats.BytesReceived),
		"bytes_out": atomic.LoadUint64(&stats.BytesForwarded),
	})
}

func printFinalStats() {
	logInfo("Final statistics", map[string]interface{}{
		"uptime":    time.Since(stats.StartTime).String(),
		"received":  atomic.LoadUint64(&stats.PacketsReceived),
		"forwarded": atomic.LoadUint64(&stats.PacketsForwarded),
		"enriched":  atomic.LoadUint64(&stats.PacketsEnriched),
		"dropped":   atomic.LoadUint64(&stats.PacketsDropped),
		"filtered":  atomic.LoadUint64(&stats.PacketsFiltered),
		"bytes_in":  atomic.LoadUint64(&stats.BytesReceived),
		"bytes_out": atomic.LoadUint64(&stats.BytesForwarded),
	})
}
