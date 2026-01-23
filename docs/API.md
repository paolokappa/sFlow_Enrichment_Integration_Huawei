# HTTP API Reference

The HTTP API provides endpoints for health checks, status monitoring, and Prometheus metrics.

## Configuration

Enable the HTTP API in `config.yaml`:

```yaml
http:
  enabled: true
  address: "127.0.0.1"  # localhost only for security
  port: 8080
```

**Security Note:** By default, the API binds to `127.0.0.1` (localhost only). To expose externally, change to `0.0.0.0` but ensure proper firewall rules are in place.

---

## Endpoints

### GET /health

Simple health check endpoint for load balancers and monitoring systems.

**Response:**
- `200 OK` with body `OK` - All destinations are healthy
- `503 Service Unavailable` with body `DEGRADED` - One or more destinations are unhealthy

**Example:**
```bash
$ curl http://127.0.0.1:8080/health
OK
```

**Use cases:**
- Kubernetes liveness/readiness probes
- Load balancer health checks
- Monitoring systems (Nagios, Zabbix)

---

### GET /status

Detailed JSON status including statistics and destination information.

**Response:** `200 OK` with JSON body

**Example:**
```bash
$ curl -s http://127.0.0.1:8080/status | jq .
```

```json
{
  "version": "2.0.0",
  "uptime": "2h30m15s",
  "stats": {
    "packets_received": 125000,
    "packets_forwarded": 250000,
    "packets_enriched": 85000,
    "packets_dropped": 0,
    "packets_filtered": 150,
    "bytes_received": 45000000,
    "bytes_forwarded": 90000000
  },
  "destinations": [
    {
      "name": "cloudflare",
      "address": "162.159.65.1:6343",
      "healthy": true,
      "packets_sent": 125000,
      "packets_dropped": 0,
      "last_error": ""
    },
    {
      "name": "noction",
      "address": "208.122.196.72:6343",
      "healthy": true,
      "packets_sent": 125000,
      "packets_dropped": 0,
      "last_error": ""
    }
  ]
}
```

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | sflow-enricher version |
| `uptime` | string | Time since service started |
| `stats.packets_received` | uint64 | Total packets received |
| `stats.packets_forwarded` | uint64 | Total packets forwarded (sum of all destinations) |
| `stats.packets_enriched` | uint64 | Packets where SrcAS was modified |
| `stats.packets_dropped` | uint64 | Packets that failed to forward |
| `stats.packets_filtered` | uint64 | Packets dropped by whitelist |
| `stats.bytes_received` | uint64 | Total bytes received |
| `stats.bytes_forwarded` | uint64 | Total bytes forwarded |
| `destinations[].name` | string | Destination name from config |
| `destinations[].address` | string | Destination address:port |
| `destinations[].healthy` | bool | Health check status |
| `destinations[].packets_sent` | uint64 | Packets sent to this destination |
| `destinations[].packets_dropped` | uint64 | Failed sends to this destination |
| `destinations[].last_error` | string | Last error message (empty if healthy) |

---

### GET /metrics

Prometheus-compatible metrics endpoint.

**Response:** `200 OK` with `text/plain` body in Prometheus exposition format

**Example:**
```bash
$ curl -s http://127.0.0.1:8080/metrics
```

```
# HELP sflow_enricher_packets_received_total Total packets received
# TYPE sflow_enricher_packets_received_total counter
sflow_enricher_packets_received_total 125000

# HELP sflow_enricher_packets_forwarded_total Total packets forwarded
# TYPE sflow_enricher_packets_forwarded_total counter
sflow_enricher_packets_forwarded_total 250000

# HELP sflow_enricher_packets_enriched_total Total packets enriched
# TYPE sflow_enricher_packets_enriched_total counter
sflow_enricher_packets_enriched_total 85000

# HELP sflow_enricher_packets_dropped_total Total packets dropped
# TYPE sflow_enricher_packets_dropped_total counter
sflow_enricher_packets_dropped_total 0

# HELP sflow_enricher_packets_filtered_total Total packets filtered by whitelist
# TYPE sflow_enricher_packets_filtered_total counter
sflow_enricher_packets_filtered_total 150

# HELP sflow_enricher_bytes_received_total Total bytes received
# TYPE sflow_enricher_bytes_received_total counter
sflow_enricher_bytes_received_total 45000000

# HELP sflow_enricher_bytes_forwarded_total Total bytes forwarded
# TYPE sflow_enricher_bytes_forwarded_total counter
sflow_enricher_bytes_forwarded_total 90000000

# HELP sflow_enricher_uptime_seconds Uptime in seconds
# TYPE sflow_enricher_uptime_seconds gauge
sflow_enricher_uptime_seconds 9015

# HELP sflow_enricher_destination_packets_sent_total Packets sent to destination
# TYPE sflow_enricher_destination_packets_sent_total counter
sflow_enricher_destination_packets_sent_total{destination="cloudflare"} 125000
sflow_enricher_destination_packets_sent_total{destination="noction"} 125000

# HELP sflow_enricher_destination_healthy Destination health status
# TYPE sflow_enricher_destination_healthy gauge
sflow_enricher_destination_healthy{destination="cloudflare"} 1
sflow_enricher_destination_healthy{destination="noction"} 1
```

**Metrics:**

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sflow_enricher_packets_received_total` | counter | - | Total packets received |
| `sflow_enricher_packets_forwarded_total` | counter | - | Total packets forwarded |
| `sflow_enricher_packets_enriched_total` | counter | - | Packets with modified SrcAS |
| `sflow_enricher_packets_dropped_total` | counter | - | Failed forwards |
| `sflow_enricher_packets_filtered_total` | counter | - | Whitelist drops |
| `sflow_enricher_bytes_received_total` | counter | - | Bytes received |
| `sflow_enricher_bytes_forwarded_total` | counter | - | Bytes forwarded |
| `sflow_enricher_uptime_seconds` | gauge | - | Uptime in seconds |
| `sflow_enricher_destination_packets_sent_total` | counter | `destination` | Per-destination packets |
| `sflow_enricher_destination_healthy` | gauge | `destination` | 1=healthy, 0=unhealthy |

---

## Prometheus Configuration

Add to `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'sflow-enricher'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: /metrics
    scrape_interval: 15s
```

---

## Grafana Dashboard

Example queries for Grafana:

**Packets per second:**
```promql
rate(sflow_enricher_packets_received_total[1m])
```

**Enrichment rate:**
```promql
rate(sflow_enricher_packets_enriched_total[1m]) / rate(sflow_enricher_packets_received_total[1m]) * 100
```

**Drop rate:**
```promql
rate(sflow_enricher_packets_dropped_total[1m])
```

**Destination health:**
```promql
sflow_enricher_destination_healthy
```

**Per-destination throughput:**
```promql
rate(sflow_enricher_destination_packets_sent_total[1m])
```
