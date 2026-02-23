# sFlow ASN Enricher - Complete Technical Specification

## Document Information
- **Version**: 2.3.0
- **Date**: 23/02/2026
- **Author**: Paolo Caparrelli - GOLINE SA
- **Contact**: soc@goline.ch

---

## Table of Contents
1. [Executive Summary](#executive-summary)
2. [Architecture Overview](#architecture-overview)
3. [sFlow v5 Protocol Deep Dive](#sflow-v5-protocol-deep-dive)
4. [XDR Encoding Specification](#xdr-encoding-specification)
5. [Extended Gateway Record Structure](#extended-gateway-record-structure)
6. [SrcAS Enrichment Logic](#srcas-enrichment-logic)
7. [DstAS Enrichment Logic](#dstas-enrichment-logic)
8. [Multi-Sample Handling](#multi-sample-handling)
9. [Systemd Integration](#systemd-integration)
10. [Telegram Notifications](#telegram-notifications)
11. [Configuration Reference](#configuration-reference)
12. [Source Code Map](#source-code-map)
13. [Binary Verification](#binary-verification)
14. [Testing and Validation](#testing-and-validation)

---

## Executive Summary

The sFlow ASN Enricher is a mission-critical service that intercepts sFlow v5 datagrams from Huawei NetEngine routers and enriches them with AS (Autonomous System) information before forwarding to collectors (Cloudflare, Noction).

### Business Requirements

| Requirement | Condition | Action |
|-------------|-----------|--------|
| **SrcAS Enrichment** | `srcIP ∈ Goline` AND `SrcAS=0` | Set `SrcAS=202032` (in-place) |
| **SrcPeerAS Enrichment** | `srcIP ∈ Goline` AND `SrcPeerAS=0` | Set `SrcPeerAS=202032` (in-place) |
| **RouterAS Enrichment** | `srcIP/dstIP ∈ Goline` AND `RouterAS=0` | Set `RouterAS=202032` (in-place) |
| **DstAS Enrichment** | `dstIP ∈ Goline` AND `DstASPathLen=0` | Insert `AS202032` in DstASPath (+12 bytes) |

### Goline Networks
- **IPv4**: `185.54.80.0/22`
- **IPv6**: `2a02:4460::/32`
- **ASN**: `AS202032`

---

## Architecture Overview

```
┌─────────────────┐     ┌──────────────────────┐     ┌─────────────────┐
│  Huawei Router  │────▶│  sFlow ASN Enricher  │────▶│   Cloudflare    │
│  NetEngine 8000 │     │    (Port 6343)       │     │  162.159.65.1   │
└─────────────────┘     │                      │────▶│                 │
                        │  - Parse sFlow v5    │     └─────────────────┘
                        │  - Enrich SrcAS      │     ┌─────────────────┐
                        │  - Enrich DstAS      │────▶│     Noction     │
                        │  - Forward packets   │     │ 208.122.196.72  │
                        └──────────────────────┘     └─────────────────┘
```

### Data Flow

1. **Receive**: UDP packet on port 6343
2. **Parse**: sFlow v5 datagram structure
3. **Extract**: Source and Destination IP from raw packet header
4. **Check**: Extended Gateway record for AS information
5. **Enrich** (Extended Gateway record, type 1003):
   - Outbound (srcIP ∈ Goline): SrcAS, SrcPeerAS, RouterAS (in-place, when field=0)
   - Inbound (dstIP ∈ Goline): DstAS (XDR insert +12 bytes, when DstASPathLen=0), RouterAS (in-place, when field=0)
6. **Forward**: Modified packet to all destinations

---

## sFlow v5 Protocol Deep Dive

### Reference Documents
- [sFlow v5 Specification](https://sflow.org/SFLOW-DATAGRAM5.txt)
- [RFC 3176 - InMon sFlow](https://datatracker.ietf.org/doc/rfc3176/)

### Datagram Structure

```
┌────────────────────────────────────────────────────────────────┐
│                    sFlow v5 Datagram Header                    │
├────────────────────────────────────────────────────────────────┤
│ Version (4 bytes)           = 5                                │
│ Agent Address Type (4 bytes) = 1 (IPv4) or 2 (IPv6)           │
│ Agent Address (4 or 16 bytes)                                  │
│ Sub-Agent ID (4 bytes)                                         │
│ Sequence Number (4 bytes)                                      │
│ Uptime (4 bytes)            = milliseconds since boot          │
│ Number of Samples (4 bytes)                                    │
├────────────────────────────────────────────────────────────────┤
│                         Sample[0]                              │
│ ┌────────────────────────────────────────────────────────────┐ │
│ │ Sample Type (4 bytes)    = enterprise << 12 | format       │ │
│ │ Sample Length (4 bytes)  = length of sample data           │ │
│ │ Sample Data (variable)                                     │ │
│ └────────────────────────────────────────────────────────────┘ │
│                         Sample[1]                              │
│                           ...                                  │
│                         Sample[N-1]                            │
└────────────────────────────────────────────────────────────────┘
```

### Sample Types (Enterprise 0)

| Format | Name | Description |
|--------|------|-------------|
| 1 | Flow Sample | Standard flow sample |
| 2 | Counter Sample | Interface counters |
| 3 | Expanded Flow Sample | Extended format with 32-bit interface IDs |

### Flow Sample Structure

```
┌────────────────────────────────────────────────────────────────┐
│                      Flow Sample Data                          │
├────────────────────────────────────────────────────────────────┤
│ Sequence Number (4 bytes)                                      │
│ Source ID (4 bytes)         = type << 24 | index               │
│ Sampling Rate (4 bytes)                                        │
│ Sample Pool (4 bytes)                                          │
│ Drops (4 bytes)                                                │
│ Input Interface (4 bytes)                                      │
│ Output Interface (4 bytes)                                     │
│ Number of Records (4 bytes)                                    │
├────────────────────────────────────────────────────────────────┤
│                        Record[0]                               │
│ ┌────────────────────────────────────────────────────────────┐ │
│ │ Record Type (4 bytes)    = enterprise << 12 | format       │ │
│ │ Record Length (4 bytes)                                    │ │
│ │ Record Data (variable)                                     │ │
│ └────────────────────────────────────────────────────────────┘ │
│                        Record[1]                               │
│                           ...                                  │
└────────────────────────────────────────────────────────────────┘
```

### Flow Record Types (Enterprise 0)

| Format | Name | Description |
|--------|------|-------------|
| 1 | Raw Packet Header | Sampled packet header bytes |
| 2 | Ethernet Frame | Ethernet frame data |
| 3 | IPv4 | IPv4 header data |
| 4 | IPv6 | IPv6 header data |
| 1001 | Extended Switch | VLAN information |
| 1002 | Extended Router | Routing information |
| 1003 | Extended Gateway | BGP/AS information |

---

## XDR Encoding Specification

### Reference
- [RFC 4506 - XDR: External Data Representation Standard](https://www.rfc-editor.org/rfc/rfc4506)

### Fundamental Rules

1. **Base Unit**: 4 bytes (32 bits)
2. **Byte Order**: Big-endian (network byte order)
3. **Alignment**: All data aligned to 4-byte boundaries
4. **Padding**: Zeros added to reach 4-byte alignment

### Data Types Used in sFlow

| Type | Encoding | Size |
|------|----------|------|
| `unsigned int` | 4 bytes, big-endian | 4 bytes |
| `opaque<>` | length (4) + data + padding | variable |
| `array<>` | count (4) + elements | variable |

### Variable-Length Array Encoding

```
┌──────────────────────────────────────────┐
│ Count (4 bytes, unsigned int)            │
├──────────────────────────────────────────┤
│ Element[0]                               │
│ Element[1]                               │
│ ...                                      │
│ Element[Count-1]                         │
└──────────────────────────────────────────┘
```

---

## Extended Gateway Record Structure

### Type: 1003 (Enterprise 0, Format 1003)

```c
struct extended_gateway {
   address nexthop;              // Next hop router address
   unsigned int as;              // Router's own AS
   unsigned int src_as;          // Source AS from routing
   unsigned int src_peer_as;     // Source peer AS
   as_path_type dst_as_path<>;   // AS path to destination (variable-length)
   unsigned int communities<>;   // BGP communities (variable-length)
   unsigned int localpref;       // Local preference
};

struct as_path_type {
   as_path_segment_type type;    // AS_SET=1, AS_SEQUENCE=2
   unsigned int as_number<>;     // Array of AS numbers
};
```

### Binary Layout — All 3 Address Types

**UNKNOWN NextHop (type=0, void = 0 address bytes):**

```
Offset  Size  Field
──────  ────  ─────────────────────────────
0       4     NextHopType (0=UNKNOWN)
4       4     AS (Router's own AS)          ◄── RouterAS enrichment
8       4     SrcAS                         ◄── SrcAS enrichment
12      4     SrcPeerAS                     ◄── SrcPeerAS enrichment
16      4     DstASPathLen                  ◄── DstAS enrichment (checked)
20      ...   DstASPath segments (if > 0)
```

**IPv4 NextHop (type=1, 4 address bytes):**

```
Offset  Size  Field
──────  ────  ─────────────────────────────
0       4     NextHopType (1=IPv4)
4       4     NextHop IPv4 Address
8       4     AS (Router's own AS)          ◄── RouterAS enrichment
12      4     SrcAS                         ◄── SrcAS enrichment
16      4     SrcPeerAS                     ◄── SrcPeerAS enrichment
20      4     DstASPathLen                  ◄── DstAS enrichment (checked)
24      ...   DstASPath segments (if > 0)
```

**IPv6 NextHop (type=2, 16 address bytes):**

```
Offset  Size  Field
──────  ────  ─────────────────────────────
0       4     NextHopType (2=IPv6)
4       16    NextHop IPv6 Address
20      4     AS (Router's own AS)          ◄── RouterAS enrichment
24      4     SrcAS                         ◄── SrcAS enrichment
28      4     SrcPeerAS                     ◄── SrcPeerAS enrichment
32      4     DstASPathLen                  ◄── DstAS enrichment (checked)
36      ...   DstASPath segments (if > 0)
```

**Offset formula (v2.3.0):** `type(4) + addr(nextHopAddrSize) + field_offset`

---

## SrcAS Enrichment Logic

### Purpose
Enrich outbound traffic from Goline networks with the correct source AS.

### Condition
```
srcIP ∈ {185.54.80.0/22, 2a02:4460::/32} AND SrcAS == 0
```

### Implementation

**File**: `cmd/sflow-enricher/main.go` (lines 421-445)

```go
// Check enrichment rules for SrcAS (outbound traffic)
for _, rule := range rules {
    shouldApply := false
    if rule.Overwrite {
        shouldApply = srcIP != nil && rule.IPNet.Contains(srcIP)
    } else {
        shouldApply = eg.SrcAS == rule.MatchAS && srcIP != nil && rule.IPNet.Contains(srcIP)
    }

    if shouldApply {
        sflow.ModifySrcAS(packet, sample.Offset, record.Offset, rule.SetAS)
        enriched = true
    }
}
```

**File**: `internal/sflow/sflow.go` (lines 523-577)

```go
func ModifySrcAS(packet []byte, sampleOffset int, recordOffset int, newAS uint32) {
    // Calculate absolute offset to SrcAS field
    // For IPv4: recordDataStart + 4 + 4 + 4 = type + ipv4 + AS
    // For IPv6: recordDataStart + 4 + 16 + 4 = type + ipv6 + AS

    // Write new SrcAS (in-place modification, no packet resize)
    binary.BigEndian.PutUint32(packet[srcASOffset:], newAS)
}
```

### Offset Calculation

| Next Hop Type | SrcAS Offset from Record Data Start |
|---------------|-------------------------------------|
| UNKNOWN (type=0) | 4 + 0 + 4 = 8 bytes |
| IPv4 (type=1) | 4 + 4 + 4 = 12 bytes |
| IPv6 (type=2) | 4 + 16 + 4 = 24 bytes |

---

## DstAS Enrichment Logic

### Purpose
Enrich inbound traffic to Goline networks with the destination AS path.

### Condition
```
dstIP ∈ {185.54.80.0/22, 2a02:4460::/32} AND DstASPathLen == 0
```

### Implementation

**File**: `cmd/sflow-enricher/main.go` (lines 447-469)

```go
// Check enrichment rules for DstAS (inbound traffic)
// Only enrich if DstASPath is empty (DstASPathLen == 0)
if eg.DstASPathLen == 0 {
    for _, rule := range rules {
        if dstIP != nil && rule.IPNet.Contains(dstIP) {
            // ModifyDstAS returns a new packet (resized) and success flag
            newPacket, ok := sflow.ModifyDstAS(packet, sample.Offset, record.Offset, rule.SetAS)
            if ok {
                packet = newPacket
                enriched = true
            }
            break
        }
    }
}
```

**File**: `internal/sflow/sflow.go` (lines 441-521)

```go
func ModifyDstAS(packet []byte, sampleOffset int, recordOffset int, newAS uint32) ([]byte, bool) {
    // 1. Calculate DstASPathLen offset
    // 2. Verify DstASPathLen == 0
    // 3. Insert 12 bytes: AS_SEQUENCE(4) + count(4) + ASN(4)
    // 4. Update DstASPathLen = 1
    // 5. Update record_length += 12
    // 6. Update sample_length += 12
    // 7. Return new packet (resized)
}
```

### Offset Calculation

| Next Hop Type | DstASPathLen Offset from Record Data Start |
|---------------|-------------------------------------------|
| UNKNOWN (type=0) | 4 + 0 + 4 + 4 + 4 = 16 bytes |
| IPv4 (type=1) | 4 + 4 + 4 + 4 + 4 = 20 bytes |
| IPv6 (type=2) | 4 + 16 + 4 + 4 + 4 = 32 bytes |

### Data Insertion (12 bytes)

```
┌────────────────────────────────────────────────────────────────┐
│ Byte 0-3:  AS_SEQUENCE (value = 2)                             │
│ Byte 4-7:  AS count in segment (value = 1)                     │
│ Byte 8-11: ASN value (value = 202032 = 0x000314A0)            │
└────────────────────────────────────────────────────────────────┘

Hex representation: 00 00 00 02 | 00 00 00 01 | 00 03 14 A0
```

### Length Updates After Insertion

| Field | Before | After | Change |
|-------|--------|-------|--------|
| DstASPathLen | 0 | 1 | +1 segment |
| record_length | N | N+12 | +12 bytes |
| sample_length | M | M+12 | +12 bytes |

---

## Multi-Sample Handling

### The Problem

When a datagram contains multiple samples and we modify one sample by inserting bytes, all subsequent sample offsets become invalid.

```
BEFORE modification:
[Header][Sample0 @ offset 100][Sample1 @ offset 300][Sample2 @ offset 500]

AFTER modifying Sample0 (+12 bytes):
[Header][Sample0+12 @ offset 100][Sample1 @ offset 312][Sample2 @ offset 512]
                                   ↑                      ↑
                                   Stored offset (300)    Stored offset (500)
                                   is now INVALID!        is now INVALID!
```

### The Solution: Reverse Order Processing

**File**: `cmd/sflow-enricher/main.go` (lines 370-374)

```go
// CRITICAL: Process samples in REVERSE ORDER to handle packet resizing correctly.
// When ModifyDstAS inserts 12 bytes into a sample, it shifts all subsequent data.
// By processing from last to first, we ensure earlier sample offsets remain valid.
for i := len(datagram.Samples) - 1; i >= 0; i-- {
    sample := datagram.Samples[i]
    // ... process sample ...
}
```

### Mathematical Proof

```
Given: Sample offsets [100, 300, 500]

Forward processing (WRONG):
  Process Sample[0] at 100: insert 12 bytes
  → Sample[1] now at 312 (but stored as 300) ✗
  → Sample[2] now at 512 (but stored as 500) ✗

Reverse processing (CORRECT):
  Process Sample[2] at 500: insert 12 bytes
  → Sample[0] at 100 still valid (100 < 500) ✓
  → Sample[1] at 300 still valid (300 < 500) ✓

  Process Sample[1] at 300: insert 12 bytes
  → Sample[0] at 100 still valid (100 < 300) ✓

  Process Sample[0] at 100: insert 12 bytes
  → Done ✓
```

---

## Systemd Integration

### Service Type: notify

The service uses `Type=notify` which requires explicit notification to systemd when ready.

### Notification Protocol

**File**: `cmd/sflow-enricher/main.go` (lines 82-140)

```go
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

func sdReady() {
    sdNotify("READY=1")
    logInfo("Systemd notified: READY", nil)
}

func sdStopping() {
    sdNotify("STOPPING=1")
}

func sdWatchdog() {
    sdNotify("WATCHDOG=1")
}
```

### Watchdog Configuration

- **WatchdogSec**: 30 seconds
- **Heartbeat interval**: 15 seconds (half of WatchdogSec)
- **Environment variable**: `WATCHDOG_USEC=30000000`

### Service Unit File

**File**: `/etc/systemd/system/sflow-enricher.service`

```ini
[Unit]
Description=sFlow ASN Enricher - Mission Critical
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=notify
ExecStart=/usr/local/bin/sflow-enricher -config /etc/sflow-enricher/config.yaml
Restart=always
RestartSec=3
WatchdogSec=30
TimeoutStartSec=30
TimeoutStopSec=30

# Security
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

# Resources
Nice=-10
LimitNOFILE=65535
MemoryMax=256M
CPUWeight=200

[Install]
WantedBy=multi-user.target
```

---

## Telegram Notifications

### Alert Types

| Type | Icon | Trigger |
|------|------|---------|
| startup | 🟢 | Service started and ready |
| shutdown | 🔴 | Service shutting down (SIGTERM/SIGINT) |
| destination_down | 🔻 | Destination health check failed |
| destination_up | 🔺 | Destination recovered after being down |
| high_drop_rate | 📉 | Drop rate exceeded threshold (default 5%) |
| ipv6_degraded | ⚠️ | IPv6 fallback to IPv4 (max 1/hour) |

### Message Template (v2.3.0)

All messages share a common header/footer with version in the title and uniform section spacing:

```
{ICON} *sFlow ASN Enricher* `v2.3.0`
━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `hostname`
🏷️ *Event:* `event_type`
{type-specific structured body with sections separated by empty lines}

🕐 *Time:* `DD/MM/YYYY HH:MM:SS`
```

### Startup Message

```
🟢 *sFlow ASN Enricher* `v2.3.0`
━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `matomo.goline.ch`
🏷️ *Event:* `startup`
📡 *Listen:* `0.0.0.0:6343`

📋 *Enrichment Rules — Extended Gateway (1003):*
   • `GOLINE_IPv4` → AS202032 (185.54.80.0/22)
   • `GOLINE_IPv6` → AS202032 (2a02:4460::/32)
   _Out(srcIP): SrcAS, SrcPeerAS, RouterAS_
   _In(dstIP): DstAS, RouterAS_

🎯 *Destinations:*
   • `cloudflare` (162.159.65.1:6343)
   • `ntopng` (127.0.0.1:4739)

🖧 *sFlow Sources:*
   • `185.54.80.2`

🕐 *Time:* `23/02/2026 23:30:48`
```

**Sections**: Listen address, Enrichment Rules with Extended Gateway (1003) details showing which fields are modified for outbound (SrcAS, SrcPeerAS, RouterAS when field=0) and inbound (DstAS XDR insert, RouterAS when field=0), Destinations with addresses, sFlow Sources (authorized router IPs).

### Shutdown Message

```
🔴 *sFlow ASN Enricher* `v2.3.0`
━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `matomo.goline.ch`
🏷️ *Event:* `shutdown`
⏱️ *Uptime:* `2h15m30s`

📊 *Stats:*
   📥 Received: `15234`
   ✅ Enriched: `14890` (97.7%)
   📤 Forwarded: `30468`
   ❌ Dropped: `0`

🎯 *Destinations:*
   ✅ `cloudflare`: 15234 pkts, 5.2 MB
   ✅ `ntopng`: 15234 pkts, 5.2 MB

🕐 *Time:* `23/02/2026 23:35:39`
```

**Sections**: Uptime (human-readable), Stats with enrichment percentage, Destinations with per-dest packets and bytes (`formatBytesCompact()` helper). Shutdown message is **blocking** to ensure delivery before process exit.

### Destination Down Message

```
🔻 *sFlow ASN Enricher* `v2.3.0`
━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `matomo.goline.ch`
🏷️ *Event:* `destination_down`
🎯 *Destination:* `cloudflare` (`162.159.65.1:6343`)
❌ *Status:* DOWN

💥 *Error:* `dial udp 162.159.65.1:6343: connect: connection refused`

📊 *Sent before failure:* 15234 pkts

🕐 *Time:* `23/02/2026 20:15:00`
```

### Destination Up Message

```
🔺 *sFlow ASN Enricher* `v2.3.0`
━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `matomo.goline.ch`
🏷️ *Event:* `destination_up`
🎯 *Destination:* `cloudflare` (`162.159.65.1:6343`)
✅ *Status:* UP

🔄 Recovered

🕐 *Time:* `23/02/2026 20:20:00`
```

### High Drop Rate Message

```
📉 *sFlow ASN Enricher* `v2.3.0`
━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `matomo.goline.ch`
🏷️ *Event:* `high_drop_rate`
⚠️ *Drop rate:* `7.2%` (threshold: `5.0%`)

📊 *Interval:* `1000` received, `72` dropped

📈 *Totals:* `50000` received, `150` dropped

🕐 *Time:* `23/02/2026 20:30:00`
```

### IPv6 Degraded Message

```
⚠️ *sFlow ASN Enricher* `v2.3.0`
━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `matomo.goline.ch`
🏷️ *Event:* `ipv6_degraded`

💬 IPv6 connectivity to Telegram API failed, using IPv4 fallback

🕐 *Time:* `23/02/2026 20:35:00`
```

Sent via a separate IPv4-only HTTP client to avoid recursion through the fallback dialer. Max 1 alert per hour (sentinel-based dedup).

### Rate Limiting

Destination state change alerts use per-destination cooldown (`flap_cooldown`, default 300s):

- Key format: `alertType:destinationName`
- Same key within cooldown → alert suppressed
- Different destinations have independent cooldowns

### Blocking vs Async

| Alert Type | Mode | Reason |
|------------|------|--------|
| startup | Async | Non-blocking, service continues initialization |
| shutdown | **Blocking** | Ensures delivery before process exit |
| destination_down | Async + rate-limited | Prevents notification storms during flapping |
| destination_up | Async + rate-limited | Same cooldown as destination_down |
| high_drop_rate | Async + rate-limited | Checked every stats interval |
| ipv6_degraded | Async (separate client) | Max 1/hour, IPv4-only client |

---

## Configuration Reference

### File: `/etc/sflow-enricher/config.yaml`

```yaml
# Listen address
listen:
  address: "0.0.0.0"
  port: 6343

# Forwarding destinations
destinations:
  - name: "cloudflare"
    address: "162.159.65.1"
    port: 6343
    enabled: true
    primary: true

  - name: "noction"
    address: "208.122.196.72"
    port: 6343
    enabled: true
    primary: true

# Enrichment rules
enrichment:
  rules:
    - name: "GOLINE_IPv4"
      network: "185.54.80.0/22"
      match_as: 0
      set_as: 202032

    - name: "GOLINE_IPv6"
      network: "2a02:4460::/32"
      match_as: 0
      set_as: 202032

# HTTP server for metrics
http:
  enabled: true
  address: "127.0.0.1"
  port: 8080

# Logging
logging:
  format: "text"  # or "json"
  stats_interval: 60

# Telegram notifications
telegram:
  enabled: true
  bot_token: "YOUR_BOT_TOKEN"
  chat_id: "YOUR_CHAT_ID"
  alert_on:
    - "startup"
    - "shutdown"
    - "destination_down"
    - "destination_up"

# Source whitelist
whitelist:
  enabled: true
  sources:
    - "10.0.0.0/8"      # Internal network
    - "172.16.0.0/12"   # Internal network
```

---

## Source Code Map

### Directory Structure

```
sflow-enricher/
├── cmd/
│   ├── sflow-enricher/
│   │   └── main.go              # Main enricher application
│   └── sflow-monitor/
│       └── main.go              # Monitor dashboard
├── internal/
│   ├── config/
│   │   └── config.go            # Configuration loading
│   └── sflow/
│       └── sflow.go             # sFlow v5 parser/modifier (RFC compliant)
├── docs/
│   ├── API.md                   # HTTP API documentation
│   ├── COMPLETE_TECHNICAL_SPECIFICATION.md  # This document
│   ├── CONFIGURATION.md         # Configuration reference
│   ├── MULTI_SAMPLE_FIX_RESEARCH.md
│   ├── OPERATIONS.md            # Operational guide
│   ├── RFC_COMPLIANCE.md        # RFC compliance certification
│   ├── SOURCE_CODE_ANALYSIS.md  # Line-by-line code analysis
│   ├── SYSTEMD_INTEGRATION.md
│   └── TELEGRAM_NOTIFICATIONS.md
├── config.yaml                  # Example configuration
├── Makefile                     # Build automation
├── go.mod                       # Go module definition
├── go.sum                       # Go dependencies checksum
├── CHANGELOG.md                 # Version history
└── README.md                    # Project overview
```

### Key Functions

| File | Function | Line | Purpose |
|------|----------|------|---------|
| main.go | `main()` | 145 | Entry point, initialization |
| main.go | `enrichPacket()` | 452 | Packet enrichment orchestration |
| main.go | `processPackets()` | 359 | UDP packet receive loop |
| main.go | `sdNotify()` | 83 | Systemd notification |
| main.go | `sdReady()` | 98 | READY notification |
| main.go | `startWatchdog()` | 117 | Watchdog heartbeat goroutine |
| main.go | `sendTelegramAlertWithWait()` | 752 | Telegram notification |
| sflow.go | `Parse()` | 92 | Parse sFlow datagram |
| sflow.go | `ParseFlowSample()` | 172 | Parse flow sample |
| sflow.go | `ParseExtendedGateway()` | 255 | Parse extended gateway record |
| sflow.go | `GetSrcDstIPFromRawPacket()` | 332 | Extract IPs from raw header |
| sflow.go | `ModifySrcAS()` | 523 | Modify SrcAS (in-place) |
| sflow.go | `ModifyDstAS()` | 441 | Insert DstAS path (resize) |

---

## Binary Verification

### Verify Function Calls in Binary

```bash
go tool objdump /usr/local/bin/sflow-enricher 2>/dev/null | \
  grep -E "(ModifySrcAS|ModifyDstAS|GetSrcDstIPFromRawPacket|sdReady)"
```

### Expected Output

```
main.go:467  CALL sflow-enricher/internal/sflow.GetSrcDstIPFromRawPacket(SB)
main.go:507  CALL sflow-enricher/internal/sflow.ModifySrcAS(SB)
main.go:526  CALL sflow-enricher/internal/sflow.ModifyDstAS(SB)
main.go:217  CALL main.sdReady(SB)
```

### Verify Binary Checksum

```bash
sha256sum /usr/local/bin/sflow-enricher
```

---

## Testing and Validation

### Debug Mode

```bash
/usr/local/bin/sflow-enricher -config /etc/sflow-enricher/config.yaml -debug
```

### Expected Debug Output

```
[DEBUG] Enriching SrcAS map[src_ip:185.54.80.30 old_as:0 new_as:202032 rule:GOLINE_IPv4]
[DEBUG] Enriching DstAS map[dst_ip:185.54.81.10 new_as:202032 rule:GOLINE_IPv4]
[DEBUG] Enriching DstAS map[dst_ip:2a02:4460:1:1::20 new_as:202032 rule:GOLINE_IPv6]
```

### Status Endpoint

```bash
curl -s http://127.0.0.1:8080/status | jq .
```

### Health Check

```bash
curl -s http://127.0.0.1:8080/health
```

### Prometheus Metrics

```bash
curl -s http://127.0.0.1:8080/metrics
```

---

## References

1. [sFlow v5 Datagram Specification](https://sflow.org/SFLOW-DATAGRAM5.txt)
2. [sFlow Version 5 Full Spec](https://sflow.org/sflow_version_5.txt)
3. [RFC 4506 - XDR Encoding Standard](https://www.rfc-editor.org/rfc/rfc4506)
4. [RFC 3176 - Original sFlow Specification](https://datatracker.ietf.org/doc/rfc3176/)
5. [systemd.service(5)](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
6. [sd_notify(3)](https://www.freedesktop.org/software/systemd/man/sd_notify.html)
7. [Telegram Bot API](https://core.telegram.org/bots/api)

---

## Document History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0.0 | 22/01/2026 | Paolo Caparrelli | Initial version |
| 2.0.0 | 23/01/2026 | Paolo Caparrelli | Added DstAS enrichment, systemd notify, Telegram |
| 2.3.0 | 23/02/2026 | Paolo Caparrelli | UNKNOWN nexthop, nextHopAddrSize(), all 4 enrichment fields, Telegram redesign, Prometheus per-dest metrics, sflow-monitor improvements |
