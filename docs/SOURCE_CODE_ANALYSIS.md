# Source Code Analysis - Line by Line Documentation

## Document Information
- **Version**: 2.0.0
- **Date**: 23/01/2026
- **Binary SHA256**: See verification section

---

## File: `internal/sflow/sflow.go`

### ModifyDstAS Function (Lines 441-521)

This is the most critical function for DstAS enrichment. It inserts a new AS path segment into the Extended Gateway record.

```go
// Line 441-443
func ModifyDstAS(packet []byte, sampleOffset int, recordOffset int, newAS uint32) ([]byte, bool) {
    // Parameters:
    //   packet: Original sFlow datagram bytes
    //   sampleOffset: Absolute offset to sample header in packet
    //   recordOffset: Relative offset to record header within sample data
    //   newAS: AS number to insert (202032)
    // Returns:
    //   []byte: New packet (resized, +12 bytes)
    //   bool: Success flag
```

```go
// Lines 444-447: Validate sample offset
if sampleOffset+8 > len(packet) {
    return packet, false
}
// Sample header is 8 bytes: type(4) + length(4)
```

```go
// Lines 449-454: Read sample length and calculate data start
sampleLen := binary.BigEndian.Uint32(packet[sampleOffset+4:])
sampleDataStart := sampleOffset + 8

if sampleDataStart+int(sampleLen) > len(packet) {
    return packet, false
}
// sampleDataStart points to first byte of flow sample data
```

```go
// Lines 456-466: Calculate absolute record offset and validate
absRecordOffset := sampleDataStart + recordOffset
if absRecordOffset+8 > len(packet) {
    return packet, false
}

recordLen := binary.BigEndian.Uint32(packet[absRecordOffset+4:])
recordDataStart := absRecordOffset + 8

if recordDataStart+int(recordLen) > len(packet) {
    return packet, false
}
// recordDataStart points to first byte of Extended Gateway data
```

```go
// Lines 468-482: Read NextHop type and calculate DstASPathLen offset
if recordDataStart+4 > len(packet) {
    return packet, false
}
nextHopType := binary.BigEndian.Uint32(packet[recordDataStart:])

var dstASPathLenOffset int
switch nextHopType {
case AddressTypeIPv4:  // type=1
    dstASPathLenOffset = recordDataStart + 4 + 4 + 4 + 4 + 4
    // Breakdown: type(4) + ipv4(4) + AS(4) + SrcAS(4) + SrcPeerAS(4) = 20
case AddressTypeIPv6:  // type=2
    dstASPathLenOffset = recordDataStart + 4 + 16 + 4 + 4 + 4
    // Breakdown: type(4) + ipv6(16) + AS(4) + SrcAS(4) + SrcPeerAS(4) = 32
default:
    return packet, false
}
```

```go
// Lines 484-493: Verify DstASPath is empty
if dstASPathLenOffset+4 > len(packet) {
    return packet, false
}

currentPathLen := binary.BigEndian.Uint32(packet[dstASPathLenOffset:])
if currentPathLen != 0 {
    // Already has AS path, don't modify
    return packet, false
}
```

```go
// Lines 495-501: Prepare insertion data (12 bytes)
insertPoint := dstASPathLenOffset + 4  // Insert AFTER DstASPathLen field
insertData := make([]byte, 12)
binary.BigEndian.PutUint32(insertData[0:], 2)     // AS_SEQUENCE type = 2
binary.BigEndian.PutUint32(insertData[4:], 1)     // Segment contains 1 ASN
binary.BigEndian.PutUint32(insertData[8:], newAS) // The ASN value (202032)

// XDR structure being created:
// ┌─────────────────┬─────────────────┬─────────────────┐
// │ AS_SEQUENCE = 2 │ AS_COUNT = 1    │ ASN = 202032    │
// │ (4 bytes)       │ (4 bytes)       │ (4 bytes)       │
// └─────────────────┴─────────────────┴─────────────────┘
```

```go
// Lines 503-507: Create new packet with inserted data
newPacket := make([]byte, len(packet)+12)
copy(newPacket[:insertPoint], packet[:insertPoint])           // Before insertion point
copy(newPacket[insertPoint:insertPoint+12], insertData)       // New 12 bytes
copy(newPacket[insertPoint+12:], packet[insertPoint:])        // After insertion point

// Memory layout:
// Original: [....A][B....]  where A=before, B=after insertion point
// New:      [....A][12 bytes][B....]
```

```go
// Lines 509-518: Update all length fields
// Update DstASPathLen from 0 to 1
binary.BigEndian.PutUint32(newPacket[dstASPathLenOffset:], 1)

// Update record length (+12 bytes)
newRecordLen := recordLen + 12
binary.BigEndian.PutUint32(newPacket[absRecordOffset+4:], newRecordLen)

// Update sample length (+12 bytes)
newSampleLen := sampleLen + 12
binary.BigEndian.PutUint32(newPacket[sampleOffset+4:], newSampleLen)
```

```go
// Line 520: Return resized packet
return newPacket, true
```

---

### ModifySrcAS Function (Lines 523-577)

This function modifies SrcAS in-place without resizing the packet.

```go
// Lines 523-527
func ModifySrcAS(packet []byte, sampleOffset int, recordOffset int, newAS uint32) {
    // In-place modification - no return value needed
    // Packet size doesn't change
```

```go
// Lines 529-548: Calculate absolute offset to record data
// Same logic as ModifyDstAS for finding recordDataStart
```

```go
// Lines 555-569: Calculate SrcAS offset based on NextHop type
nextHopType := binary.BigEndian.Uint32(packet[recordDataStart:])

var srcASOffset int
switch nextHopType {
case AddressTypeIPv4:
    srcASOffset = recordDataStart + 4 + 4 + 4
    // Breakdown: type(4) + ipv4(4) + AS(4) = 12 bytes to SrcAS
case AddressTypeIPv6:
    srcASOffset = recordDataStart + 4 + 16 + 4
    // Breakdown: type(4) + ipv6(16) + AS(4) = 24 bytes to SrcAS
default:
    return
}
```

```go
// Lines 571-576: Write new SrcAS value
if srcASOffset+4 > len(packet) {
    return
}
binary.BigEndian.PutUint32(packet[srcASOffset:], newAS)
// Simple 4-byte overwrite at calculated offset
```

---

### GetSrcDstIPFromRawPacket Function (Lines 332-384)

Extracts source and destination IP addresses from the raw packet header record.

```go
// Line 332-336
func GetSrcDstIPFromRawPacket(data []byte) (srcIP, dstIP net.IP) {
    if len(data) < 20 {
        return nil, nil
    }
```

```go
// Lines 338-352: Parse raw packet header structure
offset := 0
protocol := binary.BigEndian.Uint32(data[offset:])  // Protocol (1=Ethernet)
offset += 4
_ = protocol

offset += 4 // frame_length
offset += 4 // stripped

headerLen := binary.BigEndian.Uint32(data[offset:])  // Actual header length
offset += 4

// Raw packet header structure:
// ┌──────────────┬──────────────┬──────────────┬──────────────┬──────────────┐
// │ Protocol (4) │ FrameLen (4) │ Stripped (4) │ HeaderLen(4) │ Header bytes │
// └──────────────┴──────────────┴──────────────┴──────────────┴──────────────┘
```

```go
// Lines 355-383: Parse Ethernet frame to extract IP addresses
header := data[offset : offset+int(headerLen)]

if len(header) >= 34 {
    etherType := binary.BigEndian.Uint16(header[12:14])

    switch etherType {
    case 0x0800: // IPv4
        // Ethernet: dst(6) + src(6) + type(2) = 14 bytes
        // IPv4: ... + srcIP at offset 12 (26 from ethernet start)
        srcIP = net.IP(header[26:30])  // IPv4 source (4 bytes)
        dstIP = net.IP(header[30:34])  // IPv4 dest (4 bytes)

    case 0x86DD: // IPv6
        // IPv6: srcIP at offset 8 (22 from ethernet start)
        srcIP = net.IP(header[22:38])  // IPv6 source (16 bytes)
        dstIP = net.IP(header[38:54])  // IPv6 dest (16 bytes)

    case 0x8100: // VLAN tagged (802.1Q)
        // VLAN tag adds 4 bytes: tag(2) + type(2)
        innerType := binary.BigEndian.Uint16(header[16:18])
        if innerType == 0x0800 {
            srcIP = net.IP(header[30:34])  // IPv4 with VLAN
            dstIP = net.IP(header[34:38])
        }
    }
}
```

---

## File: `cmd/sflow-enricher/main.go`

### Reverse Order Processing (Lines 370-374)

```go
// CRITICAL: Process samples in REVERSE ORDER to handle packet resizing correctly.
// When ModifyDstAS inserts 12 bytes into a sample, it shifts all subsequent data.
// By processing from last to first, we ensure earlier sample offsets remain valid.
// This is the correct approach for XDR variable-length data modification.
for i := len(datagram.Samples) - 1; i >= 0; i-- {
    sample := datagram.Samples[i]
```

### IP Extraction (Lines 398-405)

```go
// Find source and destination IP from raw packet header
var srcIP, dstIP net.IP
for _, record := range flowSample.Records {
    if record.Enterprise == 0 && record.Format == sflow.FlowRecordRawPacketHeader {
        srcIP, dstIP = sflow.GetSrcDstIPFromRawPacket(record.Data)
        break
    }
}
```

### SrcAS Enrichment (Lines 421-445)

```go
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
```

### DstAS Enrichment (Lines 447-469)

```go
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
                packet = newPacket  // IMPORTANT: Update packet reference
                enriched = true
            }
            break // Only apply first matching rule for DstAS
        }
    }
}
```

### Packet Reference Update (Lines 310-315)

```go
// Process and enrich the packet
packet, enriched := enrichPacket(packet, remoteAddr)

// Forward to all destinations (use potentially resized packet)
for _, dest := range destinations {
    sendToDestination(dest, packet, len(packet), enriched)
    //                       ↑           ↑
    //                       │           └── Use actual length of possibly resized packet
    //                       └── Use potentially resized packet from DstAS enrichment
}
```

---

## Systemd Integration (Lines 82-140)

### sdNotify (Lines 83-96)

```go
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
```

### startWatchdog (Lines 117-140)

```go
func startWatchdog() {
    watchdogUSec := os.Getenv("WATCHDOG_USEC")
    if watchdogUSec == "" {
        return
    }

    usec, err := strconv.ParseInt(watchdogUSec, 10, 64)
    if err != nil || usec <= 0 {
        return
    }

    // Notify at half the interval (best practice from systemd docs)
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

---

## Critical Invariants

1. **Packet reference must be updated after ModifyDstAS**
   - ModifyDstAS returns a NEW slice (resized)
   - The caller MUST use the returned slice

2. **Samples must be processed in reverse order**
   - Prevents offset invalidation during DstAS insertion

3. **sdReady() must be called after initialization**
   - Service won't be considered "started" by systemd otherwise
   - Will timeout and be killed

4. **Telegram shutdown must be blocking**
   - Use `sendTelegramAlertWithWait(..., true)` on shutdown
   - Ensures notification delivery before process exit

---

## Verification Checksums

```bash
# Generate checksums for source files
sha256sum cmd/sflow-enricher/main.go internal/sflow/sflow.go

# Verify binary contains correct function calls
go tool objdump /usr/local/bin/sflow-enricher 2>/dev/null | \
  grep -c "ModifyDstAS" # Should return > 0
```
