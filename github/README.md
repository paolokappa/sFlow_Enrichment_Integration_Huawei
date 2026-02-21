<p align="center">
  <img src="assets/huawei-logo.png" alt="Huawei" height="60"/>
  &nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;
  <img src="assets/goline-logo.png" alt="GOLINE SA" height="60"/>
</p>

<h1 align="center">sFlow ASN Enricher</h1>

<p align="center">
  <strong>High-performance sFlow v5 proxy for ASN enrichment</strong>
</p>

<p align="center">
  <a href="#"><img src="https://img.shields.io/badge/version-2.2.1-blue.svg" alt="Version"></a>
  <a href="#"><img src="https://img.shields.io/badge/Go-1.21+-00ADD8.svg" alt="Go"></a>
  <a href="#"><img src="https://img.shields.io/badge/sFlow-v5-orange.svg" alt="sFlow v5"></a>
  <a href="#"><img src="https://img.shields.io/badge/license-MIT-green.svg" alt="License"></a>
</p>

<p align="center">
  <a href="#overview">Overview</a> •
  <a href="#features">Features</a> •
  <a href="#installation">Installation</a> •
  <a href="#configuration">Configuration</a> •
  <a href="#sflow-monitor">Monitor</a> •
  <a href="#documentation">Documentation</a>
</p>

---

## Overview

**sFlow ASN Enricher** is a transparent sFlow v5 proxy written in Go that enriches flow samples with Autonomous System Number (ASN) information before forwarding to collectors.

Designed specifically for environments where network devices (such as Huawei NetEngine routers) export sFlow data with missing or incomplete ASN information, this proxy intercepts sFlow datagrams, enriches them based on configurable rules, and forwards the modified packets to multiple destinations.

### The Problem

Huawei NetEngine 8000 routers (and similar devices) present two issues when exporting sFlow:

| Direction | Issue | Impact |
|-----------|-------|--------|
| **Outbound** | `SrcAS = 0` for locally-originated traffic | Collectors cannot attribute traffic to correct origin AS |
| **Outbound** | `SrcPeerAS = 0` for locally-originated traffic | Collectors see no source peer AS |
| **Outbound** | `RouterAS = peer AS` (wrong value) | Router's own AS field contains destination peer AS |
| **Inbound** | `DstASPath = []` (empty) for local destinations | Collectors cannot identify destination AS |
| **Inbound** | `RouterAS = 0` (missing) | Router's own AS field is empty |

### The Solution

```
┌─────────────────┐         ┌─────────────────────┐         ┌─────────────────┐
│  NetEngine 8000 │  sFlow  │   sFlow Enricher    │  sFlow  │    Collector    │
│                 │────────►│                     │────────►│                 │
│  SrcAS=0        │  UDP    │  SrcAS=64512        │  UDP    │  Collector A    │
│  SrcPeerAS=0    │  :6343  │  SrcPeerAS=64512    │  :6343  │  Collector B    │
│  RouterAS=0     │         │  RouterAS=64512     │         │                 │
│  DstASPath=[]   │         │  DstASPath=[64512]  │         │                 │
└─────────────────┘         └─────────────────────┘         └─────────────────┘
```

---

## Features

### Core Capabilities

| Feature | Description |
|---------|-------------|
| **SrcAS Enrichment** | In-place modification of Source AS field for outbound traffic |
| **SrcPeerAS Enrichment** | Sets SrcPeerAS to router's own AS when SrcPeerAS=0 (locally-originated traffic) |
| **RouterAS Enrichment** | Sets router's own AS field when RouterAS=0 (inbound traffic) |
| **DstAS Enrichment** | XDR-compliant insertion of Destination AS path segment for inbound traffic |
| **Multi-Sample Support** | Correct handling of datagrams with multiple flow samples |
| **Dual-Stack** | Full IPv4 and IPv6 support |

### Operational Features

| Feature | Description |
|---------|-------------|
| **Multi-Destination** | Forward to multiple collectors simultaneously |
| **Hot-Reload** | Configuration reload without service restart (SIGHUP) |
| **Health Checks** | Automatic destination monitoring with configurable failover |
| **Source Whitelist** | Accept sFlow only from authorized sources |

### Observability

| Feature | Description |
|---------|-------------|
| **Prometheus Metrics** | `/metrics` endpoint for monitoring |
| **HTTP Status API** | `/status` endpoint with JSON statistics |
| **Health Endpoint** | `/health` for load balancer integration |
| **Telegram Alerts** | Real-time notifications with IPv6/IPv4 fallback and rate limiting |
| **Structured Logging** | JSON or text format for ELK/Loki integration |
| **Live Dashboard** | `sflow-monitor` ASCII dashboard with sparklines and flow diagram |

### Reliability

| Feature | Description |
|---------|-------------|
| **Systemd Integration** | Type=notify with watchdog support |
| **Auto-Restart** | Automatic recovery from crashes |
| **Mission-Critical Config** | Nice=-10, CPUWeight=200 for priority scheduling |
| **Graceful Shutdown** | Clean termination with notification delivery |

---

## Architecture

```
                              ┌──────────────────────────────────────────┐
                              │           sFlow ASN Enricher             │
                              │                                          │
┌─────────────┐   UDP:6343    │  ┌────────────────────────────────────┐  │
│ NetEngine   │──────────────►│  │         Packet Pipeline            │  │
│ 8000 M14    │               │  │                                    │  │
│             │               │  │  ┌──────┐  ┌──────┐  ┌──────────┐  │  │
│ sFlow Agent │               │  │  │Parse │─►│Match │─►│ Enrich   │  │  │
│ 10.0.0.1    │               │  │  │sFlow │  │Rules │  │SrcAS/Dst │  │  │
└─────────────┘               │  │  │  v5  │  │      │  │PeerAS/   │  │  │
                              │  │  │      │  │      │  │RouterAS  │  │  │
                              │  │  └──────┘  └──────┘  └────┬─────┘  │  │
                              │  │                           │        │  │
                              │  └───────────────────────────┼────────┘  │
                              │                              │           │
                              │  ┌─────────────────┐         │           │
                              │  │   HTTP API      │         │           │
                              │  │   :8080         │         │           │
                              │  │  /metrics       │         │           │
                              │  │  /status        │         │           │
                              │  │  /health        │         │           │
                              │  └─────────────────┘         │           │
                              └──────────────────────────────┼───────────┘
                                                             │
                               ┌─────────────────────────────┼────────────────────┐
                               │                             │                    │
                               ▼                             ▼                    ▼
                    ┌─────────────────────┐     ┌─────────────────────┐     ┌─────────┐
                    │  Primary Collector  │     │  Secondary          │     │  Other  │
                    │                     │     │  Collector          │     │         │
                    │  198.51.100.1:6343  │     │  198.51.100.2:6343  │     │         │
                    └─────────────────────┘     └─────────────────────┘     └─────────┘
```

---

## Technical Specifications

### Protocol Compliance

This implementation is based on extensive research of the following specifications:

| Specification | Description | Reference |
|---------------|-------------|-----------|
| **sFlow v5** | sFlow Datagram Version 5 | [sflow.org/SFLOW-DATAGRAM5.txt](https://sflow.org/SFLOW-DATAGRAM5.txt) |
| **RFC 4506** | XDR: External Data Representation | [RFC 4506](https://www.rfc-editor.org/rfc/rfc4506) |
| **RFC 3176** | InMon Corporation's sFlow | [RFC 3176](https://datatracker.ietf.org/doc/rfc3176/) |

### sFlow v5 Datagram Structure

Based on the official sFlow specification:

```c
struct sample_datagram_v5 {
   address agent_address;        // IP address of sampling agent
   unsigned int sub_agent_id;    // Distinguishes datagram streams
   unsigned int sequence_number; // Incremented with each datagram
   unsigned int uptime;          // Milliseconds since boot
   sample_record samples<>;      // Variable-length array of samples
};

struct sample_record {
   data_format sample_type;      // 4 bytes: enterprise << 12 | format
   opaque sample_data<>;         // Variable-length opaque data (XDR)
};
```

### Extended Gateway Record (Type 1003)

The core structure modified by this enricher:

```c
struct extended_gateway {
   address nexthop;              // Next hop router address
   unsigned int as;              // Router's own AS          ◄── RouterAS enrichment
   unsigned int src_as;          // Source AS from routing   ◄── SrcAS enrichment
   unsigned int src_peer_as;     // Source peer AS           ◄── SrcPeerAS enrichment
   as_path_type dst_as_path<>;   // AS path to destination   ◄── DstAS enrichment
   unsigned int communities<>;   // BGP communities
   unsigned int localpref;       // Local preference
};

struct as_path_type {
   as_path_segment_type type;    // AS_SET=1, AS_SEQUENCE=2
   unsigned int as_number<>;     // Array of AS numbers
};
```

### XDR Encoding (RFC 4506)

The enricher strictly follows XDR encoding rules:

| Principle | Description |
|-----------|-------------|
| **4-byte alignment** | All data elements aligned to 32-bit boundaries |
| **Big-endian** | Network byte order for all multi-byte values |
| **Variable-length opaque** | Length prefix (4 bytes) + data + padding (0-3 bytes) |

**Variable-Length Opaque Data Encoding:**

```
+--------+--------+--------+--------+
|    length n (4 bytes, unsigned)   |
+--------+--------+--------+--------+
|     byte 0     |     byte 1       |
+--------+--------+--------+--------+
|       ...      |     byte n-1     |
+--------+--------+--------+--------+
|    padding (0-3 bytes of zeros)   |
+--------+--------+--------+--------+
```

### Enrichment Implementation

**SrcAS enrichment** (outbound traffic):
- Modification is **in-place** (no packet resize)
- SrcAS field at fixed offset within Extended Gateway record
- Simply overwrites 4 bytes when `SrcAS=0` and source IP matches rule

**SrcPeerAS enrichment** (outbound traffic):
- Modification is **in-place** (no packet resize)
- SrcPeerAS field at offset: NextHopType(4) + NextHop(4|16) + AS(4) + SrcAS(4)
- Sets SrcPeerAS to router's own AS when `SrcPeerAS=0` (locally-originated traffic)

**RouterAS enrichment** (inbound/outbound traffic):
- Modification is **in-place** (no packet resize)
- AS field at offset: NextHopType(4) + NextHop(4|16)
- Sets router's own AS when `RouterAS=0` (only enriches when the field is empty)

**DstAS enrichment** (inbound traffic):
- Requires **packet resize** (+12 bytes per enriched sample)
- Inserts AS_SEQUENCE segment with the following structure:

```
DstAS Insertion: 12 bytes total (XDR-compliant)
┌────────────────┬────────────────┬────────────────┐
│  Segment Type  │ Segment Length │   ASN Value    │
│   (4 bytes)    │   (4 bytes)    │   (4 bytes)    │
│  AS_SEQUENCE=2 │      1         │    64512       │
└────────────────┴────────────────┴────────────────┘
```

- Updates `DstASPathLen`: 0 → 1
- Updates `record_length`: +12 bytes
- Updates `sample_length`: +12 bytes
- **Critical**: Samples processed in reverse order to maintain offset integrity

### Multi-Sample Handling

sFlow datagrams can contain multiple samples. When inserting bytes for DstAS:

```
Problem: Forward processing corrupts subsequent offsets

┌─────────┐     ┌─────────┐     ┌─────────┐
│Sample[0]│     │Sample[1]│     │Sample[2]│
│@off 100 │     │@off 300 │     │@off 500 │
└─────────┘     └─────────┘     └─────────┘

After inserting 12 bytes at Sample[0]:
- Sample[1] stored offset (300) → INVALID! (now at 312)
- Sample[2] stored offset (500) → INVALID! (now at 512)

Solution: Reverse-order processing

Process Sample[2] first (offset 500):
  → Insert 12 bytes at 500
  → Sample[0] offset 100 ✓ VALID (100 < 500)
  → Sample[1] offset 300 ✓ VALID (300 < 500)

Process Sample[1] next (offset 300):
  → Insert 12 bytes at 300
  → Sample[0] offset 100 ✓ VALID (100 < 300)

Process Sample[0] last (offset 100):
  → Insert 12 bytes at 100
  → COMPLETE ✓
```

This approach guarantees mathematical correctness with zero additional overhead

---

## Installation

### From Source

```bash
# Clone repository
git clone https://github.com/paolokappa/sFlow_Enrichment_Integration_Huawei.git
cd sFlow_Enrichment_Integration_Huawei/sflow-enricher

# Build (enricher + monitor)
make all

# Install (binaries + config + systemd)
sudo make install

# Enable and start
sudo systemctl enable --now sflow-enricher
```

### Verify Installation

```bash
# Check service status
systemctl status sflow-enricher

# Check version
sflow-enricher -version

# Health check
curl http://127.0.0.1:8080/health
```

### Installed Files

| Path | Description |
|------|-------------|
| `/usr/local/bin/sflow-enricher` | Enricher binary |
| `/usr/local/bin/sflow-monitor` | Monitor dashboard binary |
| `/etc/sflow-enricher/config.yaml` | Configuration file |
| `/etc/systemd/system/sflow-enricher.service` | Systemd unit |

---

## Configuration

Configuration file: `/etc/sflow-enricher/config.yaml`

### Minimal Example

```yaml
listen:
  address: "0.0.0.0"
  port: 6343

destinations:
  - name: "collector"
    address: "192.168.1.100"
    port: 6343
    enabled: true

enrichment:
  rules:
    - name: "my-network"
      network: "10.0.0.0/8"
      match_as: 0
      set_as: 64512
```

### Production Example

```yaml
listen:
  address: "0.0.0.0"
  port: 6343

http:
  enabled: true
  address: "127.0.0.1"
  port: 8080

destinations:
  - name: "primary-collector"
    address: "198.51.100.1"
    port: 6343
    enabled: true
    primary: true

  - name: "secondary-collector"
    address: "198.51.100.2"
    port: 6343
    enabled: true
    primary: true

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

security:
  whitelist_enabled: true
  whitelist_sources:
    - "10.0.0.1"

logging:
  level: "info"
  format: "text"
  stats_interval: 60
```

For complete configuration reference, see [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

---

## Usage

### Service Management

```bash
# Start/Stop/Restart
sudo systemctl start sflow-enricher
sudo systemctl stop sflow-enricher
sudo systemctl restart sflow-enricher

# Hot-reload configuration
sudo systemctl reload sflow-enricher

# View logs
journalctl -u sflow-enricher -f
```

### Debug Mode

```bash
# Stop service and run manually with debug
sudo systemctl stop sflow-enricher
/usr/local/bin/sflow-enricher -config /etc/sflow-enricher/config.yaml -debug
```

### API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Returns `OK` or `DEGRADED` |
| `/status` | GET | JSON statistics |
| `/metrics` | GET | Prometheus format |

```bash
# Health check
curl http://127.0.0.1:8080/health

# Statistics
curl -s http://127.0.0.1:8080/status | jq .

# Prometheus metrics
curl http://127.0.0.1:8080/metrics
```

---

## sflow-monitor

Real-time ASCII dashboard for monitoring the sFlow Enricher.

### Quick Start

```bash
# Build
make build-monitor

# Run (connects to local enricher API)
sflow-monitor

# Custom URL and interval
sflow-monitor -url http://192.168.1.100:8080 -interval 5

# No colors (for logging/piping)
sflow-monitor -no-color
```

### Dashboard Features

| Feature | Description |
|---------|-------------|
| **Packet/Byte rates** | Real-time pps and KB/s with sparkline graphs |
| **Enrichment stats** | Percentage bars for enriched/dropped/filtered |
| **Enrichment rules** | Table with Name, Network, SrcAS, DstAS, Condition |
| **Flow diagram** | Visual source -> enricher -> destinations with addresses |
| **Destination table** | Health status, packets sent, drops, errors |
| **Totals** | Cumulative counters with human-readable formatting |
| **Dynamic sizing** | Frame auto-sizes to widest content at every refresh |
| **Auto-reconnect** | Shows DISCONNECTED state and retries automatically |

### Installation

```bash
# Install alongside sflow-enricher
sudo make install

# Or install manually
sudo install -m 755 build/sflow-monitor /usr/local/bin/sflow-monitor

# Static build (no dependencies)
make build-static
```

---

## Documentation

| Document | Description |
|----------|-------------|
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md) | Complete configuration reference |
| [docs/API.md](docs/API.md) | HTTP API documentation |
| [docs/OPERATIONS.md](docs/OPERATIONS.md) | Operational guide and troubleshooting |
| [docs/MULTI_SAMPLE_FIX_RESEARCH.md](docs/MULTI_SAMPLE_FIX_RESEARCH.md) | Technical research on multi-sample handling |
| [docs/SYSTEMD_INTEGRATION.md](docs/SYSTEMD_INTEGRATION.md) | Systemd notify and watchdog integration |
| [docs/TELEGRAM_NOTIFICATIONS.md](docs/TELEGRAM_NOTIFICATIONS.md) | Telegram alerting configuration |
| [CHANGELOG.md](CHANGELOG.md) | Version history and changes |

---

## Performance

Tested on Ubuntu 24.04 LTS:

| Metric | Value |
|--------|-------|
| **Throughput** | >100,000 packets/second |
| **Latency** | <1ms processing time |
| **Memory** | ~10-20 MB stable |
| **CPU** | <5% single core |

---

## References

### Official Specifications
- [sFlow Version 5 Specification](https://sflow.org/sflow_version_5.txt)
- [sFlow Datagram Structure](https://sflow.org/SFLOW-DATAGRAM5.txt)
- [RFC 4506 - XDR Encoding](https://www.rfc-editor.org/rfc/rfc4506)
- [RFC 3176 - sFlow Original](https://datatracker.ietf.org/doc/rfc3176/)

### Implementation References
- [Google gopacket sflow.go](https://github.com/google/gopacket/blob/master/layers/sflow.go)
- [Cistern/sflow - Go encoder/decoder](https://github.com/Cistern/sflow)
- [Cloudflare goflow2](https://github.com/netsampler/goflow2)

### Systemd Documentation
- [systemd.service(5)](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
- [sd_notify(3)](https://www.freedesktop.org/software/systemd/man/sd_notify.html)

---

## Project Structure

```
sflow-enricher/
├── cmd/
│   ├── sflow-enricher/
│   │   └── main.go              # Enricher entry point
│   └── sflow-monitor/
│       └── main.go              # Monitor dashboard entry point
├── internal/
│   ├── config/
│   │   └── config.go            # Configuration management
│   └── sflow/
│       └── sflow.go             # sFlow v5 parser/modifier
├── docs/
│   ├── CONFIGURATION.md         # Configuration reference
│   ├── API.md                   # API documentation
│   ├── OPERATIONS.md            # Operational guide
│   ├── MULTI_SAMPLE_FIX_RESEARCH.md
│   ├── SYSTEMD_INTEGRATION.md
│   └── TELEGRAM_NOTIFICATIONS.md
├── systemd/
│   └── sflow-enricher.service
├── config.yaml                  # Example configuration
├── Makefile                     # Build automation
├── CHANGELOG.md                 # Version history
└── README.md                    # This file
```

---

<br/>

<p align="center">
  <img src="assets/goline-logo.png" alt="GOLINE SA" height="60"/>
</p>

<p align="center">
  <strong>Developed by GOLINE SA</strong><br/>
  Swiss Network & Security Solutions
</p>

<p align="center">
  <a href="mailto:soc@goline.ch">soc@goline.ch</a>
</p>

<p align="center">
  <strong>Author:</strong> Paolo Caparrelli
</p>

<p align="center">
  <sub>MIT License - GOLINE SA - 2026</sub>
</p>

<p align="center">
  <strong>Version 2.2.1</strong> • February 2026
</p>
