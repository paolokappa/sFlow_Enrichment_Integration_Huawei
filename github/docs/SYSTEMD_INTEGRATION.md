# Systemd Integration - Complete Documentation

## Table of Contents
1. [Overview](#overview)
2. [Systemd Notify Protocol](#systemd-notify-protocol)
3. [Watchdog Mechanism](#watchdog-mechanism)
4. [Auto-Restart Configuration](#auto-restart-configuration)
5. [Service Unit File](#service-unit-file)
6. [Go Implementation](#go-implementation)
7. [Security Hardening](#security-hardening)
8. [Testing and Verification](#testing-and-verification)

---

## Overview

The sFlow ASN Enricher service is configured as **mission-critical** with:
- **Type=notify**: The service notifies systemd when it's ready
- **WatchdogSec=30**: Systemd verifies the service is active every 30 seconds
- **Restart=always**: Automatic restart on crash
- **Nice=-10, CPUWeight=200**: High priority for CPU scheduling

---

## Systemd Notify Protocol

### Official Documentation
- [systemd.service(5)](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
- [sd_notify(3)](https://www.freedesktop.org/software/systemd/man/sd_notify.html)

### How It Works

The service communicates with systemd via a Unix socket (AF_UNIX, SOCK_DGRAM) whose path is in `$NOTIFY_SOCKET`.

**Supported messages**:

| Message | Meaning |
|---------|---------|
| `READY=1` | Service ready to accept requests |
| `STOPPING=1` | Service shutting down |
| `WATCHDOG=1` | Heartbeat - service is still active |
| `STATUS=...` | Text status for `systemctl status` |
| `ERRNO=...` | Error code errno |
| `MAINPID=...` | Main process PID |

### Service Lifecycle Sequence

```
1. systemd starts the process
2. Process initializes (config, socket, destinations)
3. Process sends READY=1
4. systemd considers service "active (running)"
5. [Loop] Process sends WATCHDOG=1 every WatchdogSec/2
6. Upon receiving SIGTERM:
   6a. Process sends STOPPING=1
   6b. Process completes cleanup
   6c. Process terminates
7. systemd detects exit, decides whether to restart
```

---

## Watchdog Mechanism

### Purpose

The watchdog ensures the service is not in deadlock or hung. If the service doesn't send `WATCHDOG=1` within `WatchdogSec`, systemd considers it "failed" and restarts it.

### Systemd Configuration

```ini
[Service]
Type=notify
WatchdogSec=30
```

### Interval Calculation

From systemd documentation:
> "It is recommended that this setting is used together with Type=notify. If WatchdogSec= is used without Type=notify, the service manager will not be notified of service readiness."

**Best Practice**: Send the watchdog at half the configured interval.

```
WatchdogSec=30 → Send WATCHDOG=1 every 15 seconds
```

### Environment Variable

When systemd starts the service with watchdog enabled, it sets:
```
WATCHDOG_USEC=30000000   # 30 seconds in microseconds
```

The service reads this variable to determine the interval.

---

## Auto-Restart Configuration

### Restart Options

| Option | Behavior |
|--------|----------|
| `no` | Never restart |
| `on-success` | Only if exit code = 0 |
| `on-failure` | Only if exit code ≠ 0 or killed |
| `on-abnormal` | Only if killed by signal or timeout |
| `on-abort` | Only if killed by signal |
| `on-watchdog` | Only if watchdog timeout |
| `always` | Always restart |

### Rate Limiting

```ini
[Unit]
StartLimitIntervalSec=300
StartLimitBurst=5
```

**Meaning**: Maximum 5 starts in 300 seconds (5 minutes). If exceeded, the service enters "failed" state and won't be automatically restarted.

### RestartSec

```ini
[Service]
RestartSec=3
```

Waits 3 seconds before restarting after a crash. This prevents:
- CPU thrashing on immediate crash
- Resource exhaustion issues
- Log flooding

---

## Service Unit File

### Complete File: `/etc/systemd/system/sflow-enricher.service`

```ini
[Unit]
Description=sFlow ASN Enricher - Mission Critical
Documentation=https://github.com/paolokappa/sFlow_Enrichment_Integration_Huawei
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=notify
ExecStart=/usr/local/bin/sflow-enricher -config /etc/sflow-enricher/config.yaml
ExecReload=/bin/kill -HUP $MAINPID

# Auto-restart configuration
Restart=always
RestartSec=3
WatchdogSec=30

# Shutdown handling
TimeoutStartSec=30
TimeoutStopSec=30
KillMode=mixed
KillSignal=SIGTERM

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadOnlyPaths=/etc/sflow-enricher

# Resource limits & priority
Nice=-10
LimitNOFILE=65535
MemoryMax=256M
CPUWeight=200

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=sflow-enricher

[Install]
WantedBy=multi-user.target
```

### Detailed Explanation

#### [Unit] Section

| Directive | Value | Explanation |
|-----------|-------|-------------|
| `Description` | sFlow ASN Enricher - Mission Critical | Descriptive name |
| `Documentation` | GitHub URL | Link to documentation |
| `After` | network-online.target | Start after network is active |
| `Wants` | network-online.target | Soft dependency on network |
| `StartLimitIntervalSec` | 300 | Time window for rate limiting (5 min) |
| `StartLimitBurst` | 5 | Max starts within the window |

#### [Service] Section - Lifecycle

| Directive | Value | Explanation |
|-----------|-------|-------------|
| `Type` | notify | Uses sd_notify protocol |
| `ExecStart` | /usr/local/bin/sflow-enricher ... | Startup command |
| `ExecReload` | /bin/kill -HUP $MAINPID | Config reload command |
| `Restart` | always | Always restart after exit |
| `RestartSec` | 3 | Wait time before restart |
| `WatchdogSec` | 30 | Watchdog timeout |

#### [Service] Section - Shutdown

| Directive | Value | Explanation |
|-----------|-------|-------------|
| `TimeoutStartSec` | 30 | Max time for startup |
| `TimeoutStopSec` | 30 | Max time for shutdown |
| `KillMode` | mixed | SIGTERM to main, SIGKILL to children |
| `KillSignal` | SIGTERM | Graceful termination signal |

#### [Service] Section - Priority

| Directive | Value | Explanation |
|-----------|-------|-------------|
| `Nice` | -10 | High CPU priority (-20 = max, 19 = min) |
| `LimitNOFILE` | 65535 | Max open file descriptors |
| `MemoryMax` | 256M | Hard memory limit |
| `CPUWeight` | 200 | Relative CPU weight (100 = default, 200 = double) |

---

## Go Implementation

### sdNotify Function

```go
// sdNotify sends a notification to systemd via the notify socket
func sdNotify(state string) {
    socketPath := os.Getenv("NOTIFY_SOCKET")
    if socketPath == "" {
        return  // Not running under systemd
    }

    conn, err := net.Dial("unixgram", socketPath)
    if err != nil {
        return
    }
    defer conn.Close()

    conn.Write([]byte(state))
}
```

**Notes**:
- `NOTIFY_SOCKET` is set only if `Type=notify`
- The socket is AF_UNIX of type SOCK_DGRAM (datagram)
- Typical path: `/run/systemd/notify` or abstract socket

### Wrapper Functions

```go
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
```

### Watchdog Goroutine

```go
func startWatchdog() {
    // Read interval from environment
    watchdogUSec := os.Getenv("WATCHDOG_USEC")
    if watchdogUSec == "" {
        return  // Watchdog not configured
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
```

### Complete Sequence in main()

```go
func main() {
    // 1. Load config
    cfg, err = config.Load(configPath)

    // 2. Setup destinations
    setupDestinations()

    // 3. Setup listener
    listener, err := net.ListenUDP("udp", listenAddr)

    // 4. Start background services
    go startHTTPServer()
    go healthChecker()
    go statsReporter()

    // 5. NOTIFY READY - service operational
    sdReady()
    startWatchdog()

    // 6. Send Telegram startup notification
    sendTelegramAlert("startup", ...)

    // 7. Signal handling
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

    // 8. Start packet processing
    go processPackets(listener, stopChan)

    // 9. Wait for signals
    for sig := range sigChan {
        switch sig {
        case syscall.SIGHUP:
            cfg.Reload(configPath)  // Hot reload
        case syscall.SIGINT, syscall.SIGTERM:
            sdStopping()  // NOTIFY STOPPING
            sendTelegramAlertWithWait("shutdown", ..., true)  // Blocking
            close(stopChan)
            listener.Close()
            wg.Wait()
            return  // Exit gracefully
        }
    }
}
```

---

## Security Hardening

### Security Directives

| Directive | Effect |
|-----------|--------|
| `NoNewPrivileges=yes` | Process cannot acquire new privileges |
| `ProtectSystem=strict` | Filesystem mounted read-only except /dev, /proc, /sys |
| `ProtectHome=yes` | /home, /root, /run/user inaccessible |
| `PrivateTmp=yes` | Private isolated /tmp |
| `ReadOnlyPaths=/etc/sflow-enricher` | Config directory read-only |

### Why These Choices

1. **NoNewPrivileges**: Prevents privilege escalation if the binary is compromised

2. **ProtectSystem=strict**: The service cannot modify the system. If it needs to write logs, it uses journald.

3. **ProtectHome**: The service doesn't need access to home directories

4. **PrivateTmp**: Isolates temporary files, prevents symlink attacks

5. **ReadOnlyPaths**: Configuration should not be modified at runtime

---

## Testing and Verification

### 1. Verify Startup

```bash
systemctl start sflow-enricher
systemctl status sflow-enricher
```

Expected output:
```
● sflow-enricher.service - sFlow ASN Enricher - Mission Critical
     Loaded: loaded (/etc/systemd/system/sflow-enricher.service; enabled)
     Active: active (running) since ...
```

### 2. Test Watchdog

```bash
# Verify watchdog is active
journalctl -u sflow-enricher | grep -i watchdog
```

Expected output:
```
[INFO] Watchdog started map[interval:15s]
```

### 3. Test Auto-Restart

```bash
# Simulate crash with kill -9 (SIGKILL, not interceptable)
kill -9 $(pgrep sflow-enricher)

# Verify automatic restart
sleep 5
systemctl status sflow-enricher
```

Expected output:
```
Active: active (running)
```

With log:
```
Main process exited, code=killed, status=9/KILL
sflow-enricher.service: Scheduled restart job
Started sFlow ASN Enricher - Mission Critical
```

### 4. Test Graceful Shutdown

```bash
# Send SIGTERM (graceful shutdown)
systemctl stop sflow-enricher

# Verify Telegram receives the notification
```

### 5. Test Rate Limiting

```bash
# Multiple rapid crashes
for i in {1..6}; do
    kill -9 $(pgrep sflow-enricher) 2>/dev/null
    sleep 1
done

# After 5 crashes in < 5 minutes, service won't restart
systemctl status sflow-enricher
```

Expected output after 6th crash:
```
Active: failed (Result: start-limit-hit)
```

### 6. Reset After Rate Limit

```bash
# Reset the counter
systemctl reset-failed sflow-enricher

# Now it can start again
systemctl start sflow-enricher
```

---

## Useful Commands

```bash
# Detailed status
systemctl status sflow-enricher -l

# Real-time logs
journalctl -u sflow-enricher -f

# Last 100 logs
journalctl -u sflow-enricher -n 100

# Reload configuration (SIGHUP)
systemctl reload sflow-enricher

# Full restart
systemctl restart sflow-enricher

# Verify unit file
systemd-analyze verify /etc/systemd/system/sflow-enricher.service

# Show runtime properties
systemctl show sflow-enricher

# Show cgroup and resources
systemctl status sflow-enricher --no-pager -l
```

---

## References

1. [systemd.service(5) - Service unit configuration](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
2. [systemd.exec(5) - Execution environment](https://www.freedesktop.org/software/systemd/man/systemd.exec.html)
3. [sd_notify(3) - Notify service manager](https://www.freedesktop.org/software/systemd/man/sd_notify.html)
4. [systemd for Developers I](https://0pointer.de/blog/projects/socket-activation.html)
5. [systemd for Developers II](https://0pointer.de/blog/projects/socket-activation2.html)

---

## Author

**Paolo Caparrelli** - GOLINE SA
**Email**: soc@goline.ch
**Date**: 21/02/2026

**Co-Authored-By**: Claude Opus 4.6 (Anthropic)
