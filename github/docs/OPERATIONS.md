# Operations Guide

## Installation

### Build from Source

```bash
cd sflow-asn-enricher

# Install dependencies
go mod download

# Build
make build

# Install (binary + config + systemd)
make install
```

### File Locations

| File | Path | Description |
|------|------|-------------|
| Binary | `/usr/local/bin/sflow-asn-enricher` | Main executable |
| Config | `/etc/sflow-asn-enricher/config.yaml` | Configuration file |
| Systemd | `/etc/systemd/system/sflow-asn-enricher.service` | Service unit |

---

## Service Management

### Basic Commands

```bash
# Start service
systemctl start sflow-asn-enricher

# Stop service
systemctl stop sflow-asn-enricher

# Restart service
systemctl restart sflow-asn-enricher

# Reload configuration (hot-reload)
systemctl reload sflow-asn-enricher

# Enable at boot
systemctl enable sflow-asn-enricher

# Disable at boot
systemctl disable sflow-asn-enricher

# Check status
systemctl status sflow-asn-enricher
```

### Hot-Reload

Reload configuration without dropping packets:

```bash
systemctl reload sflow-asn-enricher
# or
kill -HUP $(pgrep sflow-asn-enricher)
```

**Reloadable settings:**
- Enrichment rules
- Whitelist configuration
- Telegram settings
- Log level

**Requires restart:**
- Listen address/port
- HTTP address/port
- Destinations

---

## Logging

### View Logs

```bash
# Real-time logs
journalctl -u sflow-asn-enricher -f

# Last 100 lines
journalctl -u sflow-asn-enricher -n 100

# Logs since boot
journalctl -u sflow-asn-enricher -b

# Logs from specific time
journalctl -u sflow-asn-enricher --since "2026-01-23 18:00:00"

# Logs with priority
journalctl -u sflow-asn-enricher -p err  # errors only
```

### Log Format

**Text format (default):**
```
2026/01/23 18:50:00 [INFO] Statistics map[received:100 forwarded:200 enriched:50]
```

**JSON format:**
```json
{"timestamp":"2026-01-23T18:50:00Z","level":"INFO","message":"Statistics","received":100,"forwarded":200,"enriched":50}
```

Enable JSON in config:
```yaml
logging:
  format: "json"
```

---

## Monitoring

### Quick Health Check

```bash
# Service status
systemctl is-active sflow-asn-enricher

# HTTP health check
curl -s http://127.0.0.1:8080/health

# Detailed status
curl -s http://127.0.0.1:8080/status | jq .

# Prometheus metrics
curl -s http://127.0.0.1:8080/metrics
```

### Traffic Verification

```bash
# See sFlow traffic (incoming + outgoing)
tcpdump -i any udp port 6343 -c 20 -n

# Count packets per second
timeout 10 tcpdump -i any udp port 6343 -q 2>/dev/null | wc -l

# Verify enrichment (debug mode)
systemctl stop sflow-asn-enricher
/usr/local/bin/sflow-asn-enricher -config /etc/sflow-asn-enricher/config.yaml -debug
```

### Key Metrics to Monitor

| Metric | Alert Threshold | Description |
|--------|-----------------|-------------|
| `packets_dropped` | > 0 | Forwarding failures |
| `packets_filtered` | unexpected increase | Whitelist rejections |
| `destination_healthy` | = 0 | Destination down |
| `uptime_seconds` | unexpected reset | Service crashed |

---

## Troubleshooting

### Service Won't Start

**Check logs:**
```bash
journalctl -u sflow-asn-enricher -n 50 --no-pager
```

**Common issues:**

1. **Port already in use:**
   ```
   Failed to listen on 0.0.0.0:6343: bind: address already in use
   ```
   Solution:
   ```bash
   ss -ulnp | grep 6343
   # Kill the process using the port or change config
   ```

2. **Config syntax error:**
   ```
   Failed to parse config file: yaml: ...
   ```
   Solution: Validate YAML syntax
   ```bash
   python3 -c "import yaml; yaml.safe_load(open('/etc/sflow-asn-enricher/config.yaml'))"
   ```

3. **Invalid network CIDR:**
   ```
   invalid network 192.168.1.0: invalid CIDR address
   ```
   Solution: Use correct CIDR notation (e.g., `192.168.1.0/24`)

### No Packets Received

1. **Check firewall:**
   ```bash
   ufw status | grep 6343
   # Add rule if missing:
   ufw allow from 10.0.0.1 to any port 6343 proto udp
   ```

2. **Verify source is sending:**
   ```bash
   tcpdump -i any udp port 6343 -c 5 -n
   ```

3. **Check whitelist:**
   ```yaml
   security:
     whitelist_enabled: false  # Temporarily disable
   ```

### Packets Not Enriched

1. **Verify rule matches:**
   ```bash
   # Run in debug mode
   /usr/local/bin/sflow-asn-enricher -config /etc/sflow-asn-enricher/config.yaml -debug
   ```

2. **Check source IP extraction:**
   - sFlow must contain raw packet header record
   - Source IP must match rule network

3. **Check match_as:**
   - If `overwrite: false`, current SrcAS must equal `match_as`
   - Use `overwrite: true` to force

### Destination Unhealthy

1. **Check connectivity:**
   ```bash
   nc -vzu 198.51.100.1 6343
   ```

2. **Check DNS resolution:**
   ```bash
   dig +short collector.example.com
   ```

3. **Check firewall outbound:**
   ```bash
   iptables -L OUTPUT -n | grep 6343
   ```

### High Drop Rate

1. **Check socket buffers:**
   ```bash
   cat /proc/sys/net/core/rmem_max
   cat /proc/sys/net/core/wmem_max
   ```

   Increase if needed:
   ```bash
   sysctl -w net.core.rmem_max=8388608
   sysctl -w net.core.wmem_max=8388608
   ```

2. **Check system resources:**
   ```bash
   top -p $(pgrep sflow-asn-enricher)
   ```

---

## Backup and Recovery

### Backup Configuration

```bash
cp /etc/sflow-asn-enricher/config.yaml /etc/sflow-asn-enricher/config.yaml.bak
```

### Restore Configuration

```bash
cp /etc/sflow-asn-enricher/config.yaml.bak /etc/sflow-asn-enricher/config.yaml
systemctl reload sflow-asn-enricher
```

### Full Backup

```bash
tar -czvf sflow-asn-enricher-backup.tar.gz \
    /etc/sflow-asn-enricher/ \
    /usr/local/bin/sflow-asn-enricher \
    /etc/systemd/system/sflow-asn-enricher.service
```

---

## Upgrade Procedure

```bash
cd sflow-asn-enricher

# Pull latest code (if using git)
git pull

# Build new version
make build

# Check version
./build/sflow-asn-enricher -version

# Stop service
systemctl stop sflow-asn-enricher

# Install new binary
cp build/sflow-asn-enricher /usr/local/bin/

# Start service
systemctl start sflow-asn-enricher

# Verify
curl -s http://127.0.0.1:8080/status | jq .version
```

---

## Uninstall

```bash
cd sflow-asn-enricher
make uninstall

# Or manually:
systemctl stop sflow-asn-enricher
systemctl disable sflow-asn-enricher
rm -f /etc/systemd/system/sflow-asn-enricher.service
rm -f /usr/local/bin/sflow-asn-enricher
rm -rf /etc/sflow-asn-enricher
systemctl daemon-reload
```

---

## Performance Tuning

### System Settings

Add to `/etc/sysctl.conf`:

```
# Increase socket buffers
net.core.rmem_max = 8388608
net.core.wmem_max = 8388608
net.core.rmem_default = 1048576
net.core.wmem_default = 1048576

# Increase UDP buffer
net.ipv4.udp_mem = 65536 131072 262144
```

Apply:
```bash
sysctl -p
```

### Service Limits

Edit `/etc/systemd/system/sflow-asn-enricher.service`:

```ini
[Service]
LimitNOFILE=65535
LimitMEMLOCK=infinity
```

Reload:
```bash
systemctl daemon-reload
systemctl restart sflow-asn-enricher
```
