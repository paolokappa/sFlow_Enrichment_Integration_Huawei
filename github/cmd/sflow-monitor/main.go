package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"
)

const (
	monitorVersion = "1.0.0"
	sparkChars     = "▁▂▃▄▅▆▇█"
	sparkLen       = 20
)

// ANSI color codes
var (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cCyan   = "\033[36m"
	cWhite  = "\033[97m"
)

// StatusResponse mirrors the JSON from /status
type StatusResponse struct {
	Version          string            `json:"version"`
	Uptime           string            `json:"uptime"`
	ListenAddress    string            `json:"listen_address"`
	WhitelistSources []string          `json:"whitelist_sources"`
	EnrichmentRules  []RuleData        `json:"enrichment_rules"`
	Stats            StatsData         `json:"stats"`
	Destinations     []DestinationData `json:"destinations"`
}

type RuleData struct {
	Name      string `json:"name"`
	Network   string `json:"network"`
	MatchAS   uint32 `json:"match_as"`
	SetAS     uint32 `json:"set_as"`
	Overwrite bool   `json:"overwrite"`
}

type StatsData struct {
	PacketsReceived  uint64 `json:"packets_received"`
	PacketsForwarded uint64 `json:"packets_forwarded"`
	PacketsEnriched  uint64 `json:"packets_enriched"`
	PacketsDropped   uint64 `json:"packets_dropped"`
	PacketsFiltered  uint64 `json:"packets_filtered"`
	BytesReceived    uint64 `json:"bytes_received"`
	BytesForwarded   uint64 `json:"bytes_forwarded"`
}

type DestinationData struct {
	Name           string `json:"name"`
	Address        string `json:"address"`
	Healthy        bool   `json:"healthy"`
	PacketsSent    uint64 `json:"packets_sent"`
	PacketsDropped uint64 `json:"packets_dropped"`
	LastError      string `json:"last_error"`
}

// RateCalculator computes rates from counter deltas
type RateCalculator struct {
	prev        StatsData
	prevDests   []DestinationData
	prevTime    time.Time
	initialized bool

	PpsIn   float64
	PpsOut  float64
	PpsDrop float64
	PpsFilt float64
	BpsIn   float64
	BpsOut  float64
	DestPps []float64
}

func (rc *RateCalculator) Update(s StatsData, dests []DestinationData, now time.Time) {
	if !rc.initialized {
		rc.prev = s
		rc.prevDests = dests
		rc.prevTime = now
		rc.initialized = true
		rc.DestPps = make([]float64, len(dests))
		return
	}

	dt := now.Sub(rc.prevTime).Seconds()
	if dt <= 0 {
		return
	}

	rc.PpsIn = float64(s.PacketsReceived-rc.prev.PacketsReceived) / dt
	rc.PpsOut = float64(s.PacketsForwarded-rc.prev.PacketsForwarded) / dt
	rc.PpsDrop = float64(s.PacketsDropped-rc.prev.PacketsDropped) / dt
	rc.PpsFilt = float64(s.PacketsFiltered-rc.prev.PacketsFiltered) / dt
	rc.BpsIn = float64(s.BytesReceived-rc.prev.BytesReceived) / dt
	rc.BpsOut = float64(s.BytesForwarded-rc.prev.BytesForwarded) / dt

	rc.DestPps = make([]float64, len(dests))
	for i, d := range dests {
		if i < len(rc.prevDests) {
			rc.DestPps[i] = float64(d.PacketsSent-rc.prevDests[i].PacketsSent) / dt
		}
	}

	rc.prev = s
	rc.prevDests = dests
	rc.prevTime = now
}

// SparklineBuffer holds a ring buffer for sparkline rendering
type SparklineBuffer struct {
	data   [sparkLen]float64
	pos    int
	filled bool
}

func (sb *SparklineBuffer) Push(v float64) {
	sb.data[sb.pos] = v
	sb.pos++
	if sb.pos >= sparkLen {
		sb.pos = 0
		sb.filled = true
	}
}

// Render returns a 12-rune wide sparkline string (always exactly 12 columns)
func (sb *SparklineBuffer) Render() string {
	count := sb.pos
	if sb.filled {
		count = sparkLen
	}
	if count == 0 {
		return strings.Repeat(" ", 12)
	}

	vals := make([]float64, count)
	if sb.filled {
		for i := 0; i < sparkLen; i++ {
			vals[i] = sb.data[(sb.pos+i)%sparkLen]
		}
	} else {
		copy(vals, sb.data[:count])
	}

	minV, maxV := vals[0], vals[0]
	for _, v := range vals {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}

	runes := []rune(sparkChars)
	levels := len(runes)
	var b strings.Builder
	rng := maxV - minV
	start := 0
	if len(vals) > 12 {
		start = len(vals) - 12
	}
	n := 0
	for _, v := range vals[start:] {
		idx := 0
		if rng > 0 {
			idx = int((v - minV) / rng * float64(levels-1))
			if idx >= levels {
				idx = levels - 1
			}
		}
		b.WriteRune(runes[idx])
		n++
	}
	for i := n; i < 12; i++ {
		b.WriteByte(' ')
	}
	return b.String()
}

// rw returns the visible terminal column width of a string (rune count).
func rw(s string) int {
	return utf8.RuneCountInString(s)
}

// --- Formatting helpers ---

func formatPps(f float64) string {
	switch {
	case f >= 1_000_000:
		return fmt.Sprintf("%.1fMpps", f/1_000_000)
	case f >= 1_000:
		return fmt.Sprintf("%.1fKpps", f/1_000)
	default:
		return fmt.Sprintf("%.1f pps", f)
	}
}

func formatBps(f float64) string {
	switch {
	case f >= 1_000_000_000:
		return fmt.Sprintf("%.1fGB/s", f/1_000_000_000)
	case f >= 1_000_000:
		return fmt.Sprintf("%.1fMB/s", f/1_000_000)
	case f >= 1_000:
		return fmt.Sprintf("%.1fKB/s", f/1_000)
	default:
		return fmt.Sprintf("%.0f B/s", f)
	}
}

func formatBytes(b uint64) string {
	switch {
	case b >= 1_000_000_000_000:
		return fmt.Sprintf("%.2f TB", float64(b)/1_000_000_000_000)
	case b >= 1_000_000_000:
		return fmt.Sprintf("%.2f GB", float64(b)/1_000_000_000)
	case b >= 1_000_000:
		return fmt.Sprintf("%.2f MB", float64(b)/1_000_000)
	case b >= 1_000:
		return fmt.Sprintf("%.2f KB", float64(b)/1_000)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatCount(n uint64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func formatCountShort(n uint64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fG", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func progressBar(percent float64, width int, fillColor string) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(math.Round(percent / 100.0 * float64(width)))
	empty := width - filled
	return fillColor + strings.Repeat("█", filled) + cDim + strings.Repeat("░", empty) + cReset
}

func cc(text, color string) string {
	if color == "" {
		return text
	}
	return color + text + cReset
}

func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen > 3 {
		return s[:maxLen-3] + "..."
	}
	return s[:maxLen]
}

func padR(s string, width int) string {
	if len(s) > width {
		s = truncStr(s, width)
	}
	return fmt.Sprintf("%-*s", width, s)
}

func padL(s string, width int) string {
	if len(s) > width {
		s = truncStr(s, width)
	}
	return fmt.Sprintf("%*s", width, s)
}

// dline is a dashboard line: raw text (for width measurement) + colored text (for display)
type dline struct {
	raw   string // visible text (may have Unicode chars, no ANSI codes)
	color string // same text with ANSI color codes
}

// separator marker (raw="---")
const sepMarker = "---"

func sep() dline {
	return dline{raw: sepMarker}
}

// emit renders all lines into a framed box, auto-sizing to the widest line
func emit(lines []dline) string {
	// Find max visible width
	maxW := 0
	for _, l := range lines {
		if l.raw == sepMarker {
			continue
		}
		w := rw(l.raw)
		if w > maxW {
			maxW = w
		}
	}
	if maxW < 72 {
		maxW = 72 // minimum inner width
	}

	boxW := maxW + 4 // "│ " + content + " │"
	hline := strings.Repeat("─", boxW-2)

	var b strings.Builder
	b.Grow(4096)

	b.WriteString("\033[H\033[2J")
	b.WriteString("┌" + hline + "┐\n")

	for _, l := range lines {
		if l.raw == sepMarker {
			b.WriteString("├" + hline + "┤\n")
			continue
		}
		pad := maxW - rw(l.raw)
		if pad < 0 {
			pad = 0
		}
		content := l.color
		if content == "" {
			content = l.raw
		}
		b.WriteString("│ " + content + strings.Repeat(" ", pad) + " │\n")
	}

	b.WriteString("└" + hline + "┘\n")
	return b.String()
}

// --- API client ---

var httpClient = &http.Client{Timeout: 3 * time.Second}

func fetchStatus(baseURL string) (*StatusResponse, error) {
	resp, err := httpClient.Get(baseURL + "/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

func fetchHealth(baseURL string) string {
	resp, err := httpClient.Get(baseURL + "/health")
	if err != nil {
		return "DISCONNECTED"
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return "HEALTHY"
	}
	return "DEGRADED"
}

// --- Dashboard rendering ---

func renderDashboard(status *StatusResponse, health string, rates *RateCalculator,
	sparkPpsIn, sparkPpsOut, sparkBpsIn, sparkBpsOut *SparklineBuffer) string {

	var lines []dline

	// === HEADER ===
	healthColor := cGreen
	if health == "DEGRADED" {
		healthColor = cYellow
	} else if health == "DISCONNECTED" {
		healthColor = cRed
	}

	titleR := "sFlow Monitor v" + monitorVersion
	statusR := "Status: ● " + health
	quitR := "q=quit"
	gap := 6
	rawH1 := titleR + strings.Repeat(" ", gap) + statusR + strings.Repeat(" ", gap) + quitR
	colorH1 := cc("sFlow Monitor", cBold+cCyan) + " " + cc("v"+monitorVersion, cDim) +
		strings.Repeat(" ", gap) +
		"Status: " + cc("●", healthColor) + " " + cc(health, cBold+healthColor) +
		strings.Repeat(" ", gap) +
		cc(quitR, cDim)
	lines = append(lines, dline{rawH1, colorH1})

	// Enricher version + uptime + listen address
	uptimeParsed, err := time.ParseDuration(status.Uptime)
	uptimeStr := status.Uptime
	if err == nil {
		uptimeStr = uptimeParsed.Round(time.Second).String()
	}
	rawH2 := "Enricher v" + status.Version + "  │  Uptime: " + uptimeStr + "  │  Listen: " + status.ListenAddress
	colorH2 := cc("Enricher v"+status.Version, cWhite) + "  │  " + cc("Uptime: "+uptimeStr, cWhite) + "  │  " + cc("Listen: "+status.ListenAddress, cWhite)
	lines = append(lines, dline{rawH2, colorH2})

	// Source IPs
	if len(status.WhitelistSources) > 0 {
		srcIPs := strings.Join(status.WhitelistSources, ", ")
		rawSrc := "Sources: " + srcIPs
		colorSrc := cc("Sources: ", cDim) + cc(srcIPs, cCyan)
		lines = append(lines, dline{rawSrc, colorSrc})
	}

	lines = append(lines, sep())

	// === PACKET FLOW + BYTES FLOW ===
	{
		rawTitle := "PACKET FLOW                          BYTES FLOW"
		colorTitle := cc("PACKET FLOW", cBold+cCyan) + "                          " + cc("BYTES FLOW", cBold+cCyan)
		lines = append(lines, dline{rawTitle, colorTitle})

		inPps := padL(formatPps(rates.PpsIn), 9)
		outPps := padL(formatPps(rates.PpsOut), 9)
		dropPps := padL(formatPps(rates.PpsDrop), 9)
		inBps := padL(formatBps(rates.BpsIn), 9)
		outBps := padL(formatBps(rates.BpsOut), 9)
		filtPps := padL(formatPps(rates.PpsFilt), 9)

		sIn := sparkPpsIn.Render()   // 12 runes
		sOut := sparkPpsOut.Render()  // 12 runes
		sBIn := sparkBpsIn.Render()   // 12 runes
		sBOut := sparkBpsOut.Render() // 12 runes

		rawIn := "In:  " + inPps + "  " + sIn + "    In:  " + inBps + "  " + sBIn
		colorIn := "In:  " + cc(inPps, cWhite) + "  " + sIn + "    In:  " + cc(inBps, cWhite) + "  " + sBIn
		lines = append(lines, dline{rawIn, colorIn})

		rawOut := "Out: " + outPps + "  " + sOut + "    Out: " + outBps + "  " + sBOut
		colorOut := "Out: " + cc(outPps, cWhite) + "  " + sOut + "    Out: " + cc(outBps, cWhite) + "  " + sBOut
		lines = append(lines, dline{rawOut, colorOut})

		dropColor := cGreen
		if rates.PpsDrop > 0 {
			dropColor = cYellow
		}
		if rates.PpsDrop > 10 {
			dropColor = cRed
		}
		rawDrop := "Drop:" + dropPps + "                    Filt:" + filtPps
		colorDrop := cc("Drop:"+dropPps, dropColor) + "                    " + cc("Filt:"+filtPps, cDim)
		lines = append(lines, dline{rawDrop, colorDrop})
	}

	lines = append(lines, sep())

	// === ENRICHMENT ===
	{
		lines = append(lines, dline{"ENRICHMENT", cc("ENRICHMENT", cBold+cCyan)})

		totalRecv := maxU64(status.Stats.PacketsReceived, 1)
		enrichPct := float64(status.Stats.PacketsEnriched) / float64(totalRecv) * 100
		dropPct := float64(status.Stats.PacketsDropped) / float64(totalRecv) * 100
		filtPct := float64(status.Stats.PacketsFiltered) / float64(totalRecv) * 100

		// Packets without Extended Gateway record (counter samples, no-match flows)
		noGw := status.Stats.PacketsReceived - status.Stats.PacketsEnriched - status.Stats.PacketsDropped - status.Stats.PacketsFiltered
		noGwPct := float64(noGw) / float64(totalRecv) * 100

		barW := 40
		enrichLabel := fmt.Sprintf("Enriched: %5.1f%%  ", enrichPct)
		noGwLabel := fmt.Sprintf("No ExtGW: %5.1f%%  ", noGwPct)
		dropLabel := fmt.Sprintf("Dropped:  %5.1f%%  ", dropPct)
		filtLabel := fmt.Sprintf("Filtered: %5.1f%%  ", filtPct)

		// Raw uses # for bar chars (same column width)
		rawBar := strings.Repeat("#", barW)
		rawEnrich := enrichLabel + "[" + rawBar + "]"
		rawNoGw := noGwLabel + "[" + rawBar + "]"
		rawDrop := dropLabel + "[" + rawBar + "]"
		rawFilt := filtLabel + "[" + rawBar + "]"

		dropBarColor := cGreen
		if dropPct > 1 {
			dropBarColor = cYellow
		}
		if dropPct > 5 {
			dropBarColor = cRed
		}

		colorEnrich := cc(enrichLabel, cWhite) + "[" + progressBar(enrichPct, barW, cGreen) + "]"
		colorNoGw := cc(noGwLabel, cWhite) + "[" + progressBar(noGwPct, barW, cYellow) + "]"
		colorDrop := cc(dropLabel, cWhite) + "[" + progressBar(dropPct, barW, dropBarColor) + "]"
		colorFilt := cc(filtLabel, cWhite) + "[" + progressBar(filtPct, barW, cCyan) + "]"

		lines = append(lines, dline{rawEnrich, colorEnrich})
		lines = append(lines, dline{rawNoGw, colorNoGw})
		lines = append(lines, dline{rawFilt, colorFilt})
		lines = append(lines, dline{rawDrop, colorDrop})
	}

	lines = append(lines, sep())

	// === ENRICHMENT RULES ===
	if len(status.EnrichmentRules) > 0 {
		lines = append(lines, dline{"ENRICHMENT RULES", cc("ENRICHMENT RULES", cBold+cCyan)})

		hdrR := fmt.Sprintf("%-16s %-20s %8s  %-s", "Name", "Network", "SetAS", "Modifies")
		lines = append(lines, dline{hdrR, cc(hdrR, cBold+cWhite)})

		tblSepR := strings.Repeat("─", rw(hdrR))
		lines = append(lines, dline{tblSepR, cc(tblSepR, cDim)})

		for _, r := range status.EnrichmentRules {
			name := padR(truncStr(r.Name, 16), 16)
			network := padR(r.Network, 20)
			setAS := padL(fmt.Sprintf("%d", r.SetAS), 8)

			// Build human-readable modifies description
			var cond string
			if r.Overwrite {
				cond = "always"
			} else if r.MatchAS == 0 {
				cond = "if unset"
			} else {
				cond = fmt.Sprintf("if AS=%d", r.MatchAS)
			}
			modifies := "SrcAS + SrcPeerAS + DstAS + RouterAS (" + cond + ")"

			rawRow := name + " " + network + " " + setAS + "  " + modifies
			colorRow := cc(name, cWhite) + " " + cc(network, cCyan) + " " +
				cc(setAS, cYellow) + "  " +
				cc("SrcAS", cGreen) + " + " + cc("SrcPeerAS", cGreen) + " + " +
				cc("DstAS", cGreen) + " + " + cc("RouterAS", cGreen) +
				" (" + cc(cond, cDim) + ")"
			lines = append(lines, dline{rawRow, colorRow})
		}

		lines = append(lines, sep())
	}

	// === FLOW DIAGRAM ===
	{
		lines = append(lines, dline{"FLOW DIAGRAM", cc("FLOW DIAGRAM", cBold+cCyan)})

		totalRecv := maxU64(status.Stats.PacketsReceived, 1)
		enrichPct := float64(status.Stats.PacketsEnriched) / float64(totalRecv) * 100
		inRate := padL(formatPps(rates.PpsIn), 9)

		srcLabel := "SOURCE"
		if len(status.WhitelistSources) > 0 {
			srcLabel = status.WhitelistSources[0]
		}

		if len(status.Destinations) > 0 {
			for i, d := range status.Destinations {
				dPps := "0 pps"
				if i < len(rates.DestPps) {
					dPps = formatPps(rates.DestPps[i])
				}
				dPpsFmt := padL(dPps, 9)
				dName := d.Name
				dAddr := d.Address

				healthR := "● UP"
				healthC := cc("● UP", cGreen)
				if !d.Healthy {
					healthR = "● DN"
					healthC = cc("● DN", cRed)
				}

				if i == 0 {
					rawLine := "[" + srcLabel + "]─" + inRate + "─▶[ENRICHER]─" + dPpsFmt + "─▶[" + dName + "] " + healthR + "  " + dAddr
					colorLine := "[" + cc(srcLabel, cCyan) + "]─" + cc(inRate, cWhite) + "─▶[" +
						cc("ENRICHER", cBold+cGreen) + "]─" + cc(dPpsFmt, cWhite) + "─▶[" +
						cc(dName, cWhite) + "] " + healthC + "  " + cc(dAddr, cDim)
					lines = append(lines, dline{rawLine, colorLine})
				} else {
					enrichFmt := fmt.Sprintf("%5.1f%%", enrichPct)
					// Indent to align with the ENRICHER box output
					indent := strings.Repeat(" ", rw("["+srcLabel+"]─"+inRate+"─▶[ENRICHER]─"))
					rawLine := indent + enrichFmt + "─" + dPpsFmt + "─▶[" + dName + "] " + healthR + "  " + dAddr
					colorLine := indent + cc(enrichFmt, cYellow) + "─" + cc(dPpsFmt, cWhite) + "─▶[" +
						cc(dName, cWhite) + "] " + healthC + "  " + cc(dAddr, cDim)
					lines = append(lines, dline{rawLine, colorLine})
					enrichPct = 0
				}
			}
		} else {
			rawLine := "[" + srcLabel + "]─" + inRate + "─▶[ENRICHER]─▶(no destinations)"
			colorLine := "[" + cc(srcLabel, cCyan) + "]─" + cc(inRate, cWhite) + "─▶[" +
				cc("ENRICHER", cBold+cGreen) + "]─▶" + cc("(no destinations)", cYellow)
			lines = append(lines, dline{rawLine, colorLine})
		}
	}

	lines = append(lines, sep())

	// === DESTINATIONS TABLE ===
	{
		lines = append(lines, dline{"DESTINATIONS", cc("DESTINATIONS", cBold+cCyan)})

		hdr := fmt.Sprintf("%-12s %-22s %-6s %10s %7s  %-8s", "Name", "Address", "Health", "Pkts Sent", "Drop", "Error")
		lines = append(lines, dline{hdr, cc(hdr, cBold+cWhite)})

		tblSep := strings.Repeat("─", rw(hdr))
		lines = append(lines, dline{tblSep, cc(tblSep, cDim)})

		for _, d := range status.Destinations {
			name := padR(truncStr(d.Name, 12), 12)
			addr := padR(truncStr(d.Address, 22), 22)
			sent := padL(formatCountShort(d.PacketsSent), 10)
			drop := padL(formatCountShort(d.PacketsDropped), 7)
			errStr := padR(truncStr(d.LastError, 8), 8)

			healthR := "● UP  "
			healthC := cc("● UP  ", cGreen)
			if !d.Healthy {
				healthR = "● DOWN"
				healthC = cc("● DOWN", cRed)
			}

			rawRow := name + " " + addr + " " + healthR + " " + sent + " " + drop + "  " + errStr
			colorRow := cc(name, cWhite) + " " + cc(addr, cWhite) + " " + healthC + " " +
				cc(sent, cWhite) + " " + cc(drop, cWhite) + "  " + cc(errStr, cDim)
			lines = append(lines, dline{rawRow, colorRow})
		}
	}

	lines = append(lines, sep())

	// === TOTALS ===
	{
		lines = append(lines, dline{"TOTALS", cc("TOTALS", cBold+cCyan)})

		s := status.Stats
		r := formatCount(s.PacketsReceived)
		f := formatCount(s.PacketsForwarded)
		e := formatCount(s.PacketsEnriched)
		bi := formatBytes(s.BytesReceived)
		bo := formatBytes(s.BytesForwarded)
		dr := formatCount(s.PacketsDropped)

		rawTot1 := fmt.Sprintf("Received: %12s   Forwarded: %12s   Enriched: %12s", r, f, e)
		rawTot2 := fmt.Sprintf("Bytes In: %12s   Bytes Out: %12s   Dropped:  %12s", bi, bo, dr)

		lines = append(lines, dline{rawTot1, cc(rawTot1, cWhite)})

		dropColor := cWhite
		if s.PacketsDropped > 0 {
			dropColor = cYellow
		}
		lines = append(lines, dline{rawTot2, cc(rawTot2, dropColor)})
	}

	return emit(lines)
}

func renderDisconnected(baseURL string, lastErr error) string {
	var lines []dline

	rawH := "sFlow Monitor v" + monitorVersion + "      Status: ● DISCONNECTED      q=quit"
	colorH := cc("sFlow Monitor", cBold+cCyan) + " " + cc("v"+monitorVersion, cDim) +
		"      Status: " + cc("●", cRed) + " " + cc("DISCONNECTED", cBold+cRed) +
		"      " + cc("q=quit", cDim)
	lines = append(lines, dline{rawH, colorH})
	lines = append(lines, sep())
	lines = append(lines, dline{"", ""})

	msg := "Cannot connect to sflow-enricher API"
	lines = append(lines, dline{msg, cc(msg, cBold+cRed)})

	urlLine := "URL: " + baseURL
	lines = append(lines, dline{urlLine, cc(urlLine, cYellow)})

	if lastErr != nil {
		errLine := "Error: " + lastErr.Error()
		lines = append(lines, dline{errLine, cc(errLine, cDim)})
	}

	lines = append(lines, dline{"Retrying...", cc("Retrying...", cDim)})
	lines = append(lines, dline{"", ""})

	return emit(lines)
}

func maxU64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func disableColors() {
	cReset = ""
	cBold = ""
	cDim = ""
	cRed = ""
	cGreen = ""
	cYellow = ""
	cCyan = ""
	cWhite = ""
}

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:8080", "sflow-enricher API base URL")
	interval := flag.Float64("interval", 2.0, "Refresh interval in seconds")
	noColor := flag.Bool("no-color", false, "Disable ANSI colors")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("sflow-monitor version %s\n", monitorVersion)
		os.Exit(0)
	}

	if *noColor {
		disableColors()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	quitChan := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if buf[0] == 'q' || buf[0] == 'Q' {
				close(quitChan)
				return
			}
		}
	}()

	fd := int(os.Stdin.Fd())
	oldState := setRawTerminal(fd)
	defer restoreTerminal(fd, oldState)

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	rates := &RateCalculator{}
	sparkPpsIn := &SparklineBuffer{}
	sparkPpsOut := &SparklineBuffer{}
	sparkBpsIn := &SparklineBuffer{}
	sparkBpsOut := &SparklineBuffer{}

	ticker := time.NewTicker(time.Duration(*interval*1000) * time.Millisecond)
	defer ticker.Stop()

	doRender := func() {
		status, err := fetchStatus(*baseURL)
		if err != nil {
			os.Stdout.WriteString(renderDisconnected(*baseURL, err))
			return
		}

		health := fetchHealth(*baseURL)
		now := time.Now()
		rates.Update(status.Stats, status.Destinations, now)

		sparkPpsIn.Push(rates.PpsIn)
		sparkPpsOut.Push(rates.PpsOut)
		sparkBpsIn.Push(rates.BpsIn)
		sparkBpsOut.Push(rates.BpsOut)

		os.Stdout.WriteString(renderDashboard(status, health, rates, sparkPpsIn, sparkPpsOut, sparkBpsIn, sparkBpsOut))
	}

	doRender()

	for {
		select {
		case <-ticker.C:
			doRender()
		case <-sigChan:
			return
		case <-quitChan:
			return
		}
	}
}

// --- Raw terminal mode via syscall ---

type termios struct {
	Iflag  uint32
	Oflag  uint32
	Cflag  uint32
	Lflag  uint32
	Cc     [20]byte
	Ispeed uint32
	Ospeed uint32
}

func getTermios(fd int) *termios {
	t := &termios{}
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(0x5401), // TCGETS
		uintptr(unsafe.Pointer(t)))
	return t
}

func setTermios(fd int, t *termios) {
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(0x5402), // TCSETS
		uintptr(unsafe.Pointer(t)))
}

func setRawTerminal(fd int) *termios {
	old := getTermios(fd)
	raw := *old
	raw.Lflag &^= syscall.ICANON | syscall.ECHO
	raw.Cc[6] = 0 // VMIN
	raw.Cc[5] = 1 // VTIME (1/10 second)
	setTermios(fd, &raw)
	return old
}

func restoreTerminal(fd int, old *termios) {
	setTermios(fd, old)
	fmt.Print("\033[?25h\033[H\033[2J")
}
