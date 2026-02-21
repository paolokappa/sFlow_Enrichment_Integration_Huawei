# Configuration Reference

Configuration file location: `/etc/sflow-enricher/config.yaml`

## Full Example

```yaml
# sFlow Enricher Configuration v2.1

# Listen address for incoming sFlow
listen:
  address: "0.0.0.0"
  port: 6343

# HTTP API for metrics and status
http:
  enabled: true
  address: "127.0.0.1"
  port: 8080

# Destinations to forward enriched sFlow
destinations:
  - name: "primary-collector"
    address: "198.51.100.1"
    port: 6343
    enabled: true
    primary: true
    failover: "primary-collector-backup"

  - name: "primary-collector-backup"
    address: "198.51.100.10"
    port: 6343
    enabled: true
    primary: false

  - name: "secondary-collector"
    address: "198.51.100.2"
    port: 6343
    enabled: true
    primary: true

# ASN enrichment rules
enrichment:
  rules:
    - name: "my-network-ipv4"
      network: "203.0.113.0/24"
      match_as: 0
      set_as: 64512
      overwrite: false

    - name: "my-network-ipv6"
      network: "2001:db8::/32"
      match_as: 0
      set_as: 64512
      overwrite: false

# Security settings
security:
  whitelist_enabled: true
  whitelist_sources:
    - "10.0.0.1"
    - "10.0.0.0/8"

# Logging configuration
logging:
  level: "info"
  format: "text"
  stats_interval: 60

# Telegram notifications
telegram:
  enabled: true
  bot_token: "123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
  chat_id: "-1001234567890"
  alert_on:
    - "startup"
    - "shutdown"
    - "destination_down"
    - "destination_up"
    - "high_drop_rate"
  drop_rate_threshold: 5.0
  http_timeout: 15
  flap_cooldown: 300
  ipv6_fallback: false
```

---

## Section Reference

### listen

Controls where the proxy listens for incoming sFlow packets.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `address` | string | `"0.0.0.0"` | IP address to bind to |
| `port` | int | `6343` | UDP port to listen on |

```yaml
listen:
  address: "0.0.0.0"
  port: 6343
```

---

### http

Controls the HTTP API server for metrics and status.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `enabled` | bool | `false` | Enable HTTP API |
| `address` | string | `"127.0.0.1"` | IP address to bind to |
| `port` | int | `8080` | HTTP port |

```yaml
http:
  enabled: true
  address: "127.0.0.1"  # localhost only for security
  port: 8080
```

**Endpoints:**
- `GET /health` - Returns `OK` or `DEGRADED`
- `GET /status` - JSON status with statistics
- `GET /metrics` - Prometheus metrics

---

### destinations

List of collectors to forward enriched sFlow packets to.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Unique identifier for the destination |
| `address` | string | required | IP address or hostname |
| `port` | int | required | UDP port |
| `enabled` | bool | `false` | Enable this destination |
| `primary` | bool | `false` | Mark as primary (for failover) |
| `failover` | string | `""` | Name of failover destination |

```yaml
destinations:
  - name: "primary-collector"
    address: "198.51.100.1"
    port: 6343
    enabled: true
    primary: true
    failover: "primary-collector-backup"

  - name: "primary-collector-backup"
    address: "198.51.100.10"
    port: 6343
    enabled: true
    primary: false
```

**Failover Behavior:**
- Health checks run every 30 seconds
- If primary destination is unhealthy and failover is configured, traffic is sent to failover
- When primary recovers, traffic switches back automatically

---

### enrichment

Rules for modifying ASN fields in sFlow packets. Rules are applied to **both SrcAS and DstAS**:

- **SrcAS enrichment**: Applied when source IP matches the network (outbound traffic)
- **DstAS enrichment**: Applied when destination IP matches the network AND DstASPath is empty (inbound traffic)

#### enrichment.rules

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | required | Rule name (for logging) |
| `network` | string | required | CIDR notation (e.g., `"192.168.0.0/16"`) |
| `match_as` | uint32 | required | Only apply SrcAS if current value equals this |
| `set_as` | uint32 | required | New AS value to set |
| `overwrite` | bool | `false` | If true, ignore `match_as` and always overwrite SrcAS |

```yaml
enrichment:
  rules:
    - name: "my-network-ipv4"
      network: "192.168.0.0/16"
      match_as: 0           # Only if SrcAS is 0
      set_as: 64512         # Set to AS64512
      overwrite: false      # Don't overwrite if already set

    - name: "force-as"
      network: "10.0.0.0/8"
      match_as: 0           # Ignored when overwrite=true
      set_as: 64513
      overwrite: true       # Always set, regardless of current value
```

**How SrcAS enrichment works (outbound traffic):**
1. Extract source IP from raw packet header
2. Check if source IP matches any rule's network
3. If `overwrite: false`, only modify if current SrcAS equals `match_as`
4. If `overwrite: true`, always modify regardless of current SrcAS
5. Modify the SrcAS field in-place (no packet resize)

**How DstAS enrichment works (inbound traffic):**
1. Extract destination IP from raw packet header
2. Check if destination IP matches any rule's network
3. Check if DstASPath is empty (length = 0)
4. Insert AS path segment with `set_as` value (packet resize +12 bytes)
5. Update record and sample length fields (XDR-compliant)

**Multi-sample handling:**
- Samples are processed in **reverse order** (last to first)
- This ensures packet resizing doesn't corrupt subsequent sample offsets

---

### security

Controls access to the proxy.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `whitelist_enabled` | bool | `false` | Enable source IP whitelist |
| `whitelist_sources` | []string | `[]` | List of allowed IPs/CIDRs |

```yaml
security:
  whitelist_enabled: true
  whitelist_sources:
    - "10.0.0.1"          # Single IP
    - "10.0.0.0/8"        # CIDR notation
    - "192.168.1.0/24"
```

**Behavior:**
- If `whitelist_enabled: false`, all sources are accepted
- If `whitelist_enabled: true`, only listed IPs/networks can send sFlow
- Packets from non-whitelisted sources are silently dropped
- Filtered packets are counted in `packets_filtered` metric

---

### logging

Controls log output format and verbosity.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `level` | string | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `format` | string | `"text"` | Output format: `text` or `json` |
| `stats_interval` | int | `60` | Seconds between stats log output |

```yaml
logging:
  level: "info"
  format: "text"        # Human-readable
  stats_interval: 60    # Log stats every 60 seconds
```

**JSON format example:**
```yaml
logging:
  format: "json"
```

Output:
```json
{"timestamp":"2026-01-23T18:50:00Z","level":"INFO","message":"Statistics","received":100,"forwarded":200,"enriched":50}
```

---

### telegram

Telegram bot notifications for alerts.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `enabled` | bool | `false` | Enable Telegram notifications |
| `bot_token` | string | `""` | Bot token from @BotFather |
| `chat_id` | string | `""` | Chat or group ID |
| `alert_on` | []string | `[]` | List of alert types to send |
| `drop_rate_threshold` | float64 | `5.0` | Drop rate percentage to trigger `high_drop_rate` alert |
| `http_timeout` | int | `15` | HTTP request timeout in seconds for Telegram API calls |
| `flap_cooldown` | int | `300` | Seconds between alerts for the same destination (prevents flapping) |
| `ipv6_fallback` | bool | `false` | Try IPv6 first, fallback to IPv4 if it fails |

**Alert types:**
- `startup` - Service started
- `shutdown` - Service stopping
- `destination_down` - Destination became unhealthy
- `destination_up` - Destination recovered after being down
- `high_drop_rate` - Drop rate exceeded `drop_rate_threshold`

```yaml
telegram:
  enabled: true
  bot_token: "123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
  chat_id: "-1001234567890"
  alert_on:
    - "startup"
    - "shutdown"
    - "destination_down"
    - "destination_up"
    - "high_drop_rate"
  drop_rate_threshold: 5.0   # Alert when drops > 5%
  http_timeout: 15            # 15 second timeout
  flap_cooldown: 300          # 5 minutes between same-destination alerts
  ipv6_fallback: false        # Enable IPv6-first with IPv4 fallback
```

**Getting credentials:**
1. Create bot: Message @BotFather, send `/newbot`
2. Get token: BotFather will provide it
3. Get chat ID: Add bot to group, send message, check `https://api.telegram.org/bot<TOKEN>/getUpdates`

---

## Hot-Reload

The following settings can be reloaded without restart:
- `enrichment.rules`
- `security.whitelist_enabled`
- `security.whitelist_sources`
- `telegram.*`
- `logging.level`

**To reload:**
```bash
systemctl reload sflow-enricher
# or
kill -HUP $(pgrep sflow-enricher)
```

Settings that require restart:
- `listen.*`
- `http.*`
- `destinations.*`
- `logging.format`
