# Multi-Sample Bug Fix - Complete Technical Documentation

## Table of Contents
1. [Problem Identified](#problem-identified)
2. [sFlow v5 Specification Research](#sflow-v5-specification-research)
3. [RFC 4506 - XDR Encoding Research](#rfc-4506---xdr-encoding-research)
4. [RFC 3176 - Original sFlow Research](#rfc-3176---original-sflow-research)
5. [Existing Implementations Analysis](#existing-implementations-analysis)
6. [Fix Strategies Analyzed](#fix-strategies-analyzed)
7. [Implemented Solution](#implemented-solution)
8. [Verification and Testing](#verification-and-testing)

---

## Problem Identified

### Context
The sFlow ASN Enricher for Huawei NetEngine must modify sFlow v5 packets to:
1. **SrcAS**: Set AS202032 when `SrcAS=0` and `src_ip` belongs to `185.54.80.0/22` or `2a02:4460::/32`
2. **DstAS**: Insert AS202032 in `DstASPath` when empty and `dst_ip` belongs to Goline networks

### The Multi-Sample Bug
When `ModifyDstAS()` inserts 12 bytes for the AS path segment:

```
BEFORE modification:
[Datagram Header][Sample0 @ offset 100][Sample1 @ offset 300][Sample2 @ offset 500]

AFTER modifying Sample0 (+12 bytes):
[Datagram Header][Sample0+12 @ offset 100][Sample1 @ offset 312][Sample2 @ offset 512]
                                           ↑                      ↑
                                           Stored offsets (300, 500) are now INVALID!
```

The problem: `datagram.Samples[1].Offset` still contains `300` (calculated from the original packet), but in the new packet Sample1 is located at `312`.

---

## sFlow v5 Specification Research

### Source: [sflow.org/SFLOW-DATAGRAM5.txt](https://sflow.org/SFLOW-DATAGRAM5.txt)

### Datagram v5 Structure

```c
struct sample_datagram_v5 {
   address agent_address;        // IP address of sampling agent
   unsigned int sub_agent_id;    // Distinguishes datagram streams
   unsigned int sequence_number; // Incremented with each datagram
   unsigned int uptime;          // Milliseconds since boot
   sample_record samples<>;      // Variable-length array of samples
};
```

### Sample Record Structure

```c
struct sample_record {
   data_format sample_type;      // 4 bytes: enterprise << 12 | format
   opaque sample_data<>;         // Variable-length opaque data
};
```

**Key**: The use of `opaque<>` indicates variable-length data with length prefix (XDR encoding).

### Flow Sample Structure

```c
struct flow_sample {
   unsigned int sequence_number;
   sflow_data_source source_id;
   unsigned int sampling_rate;
   unsigned int sample_pool;
   unsigned int drops;
   interface input;
   interface output;
   flow_record flow_records<>;   // Variable-length array of records
};
```

### Extended Gateway Record (Type 1003)

```c
struct extended_gateway {
   address nexthop;              // Next hop router address
   unsigned int as;              // Router's own AS
   unsigned int src_as;          // Source AS from routing
   unsigned int src_peer_as;     // Source peer AS
   as_path_type dst_as_path<>;   // AS path to destination
   unsigned int communities<>;   // BGP communities
   unsigned int localpref;       // Local preference
};

struct as_path_type {
   as_path_segment_type type;    // AS_SET=1, AS_SEQUENCE=2
   unsigned int as_number<>;     // Array of AS numbers
};
```

### Critical Note from Specification

> "Applications receiving sFlow data must always use the opaque length information when decoding opaque<> structures so that encountering extended structures will not cause decoding errors."

> "Adding length fields to structures provides for two different types of extensibility. The second type involves being able to extend the length of an already-existing structure in a way that need not break compatibility with collectors which understand an older version."

---

## RFC 4506 - XDR Encoding Research

### Source: [RFC 4506 - XDR: External Data Representation Standard](https://www.rfc-editor.org/rfc/rfc4506)

### Fundamental XDR Principles

1. **Base Unit**: 4 bytes, 32 bits, serialized in big-endian
2. **Alignment**: All data must be aligned to 4 bytes
3. **Smaller Types**: Still occupy 4 bytes after encoding

### Variable-Length Opaque Data

From RFC 4506, Section 4.10:

```
opaque identifier<m>;    // with maximum limit m
opaque identifier<>;     // no limit (max 2^32 - 1)
```

**Encoding**:
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

> "Variable-length opaque data is defined as a sequence of n (numbered 0 through n-1) arbitrary bytes to be the number n encoded as an unsigned integer, and followed by the n bytes of the sequence."

> "If n is not a multiple of four, then the n bytes are followed by enough (0 to 3) residual zero bytes, r, to make the total byte count a multiple of four."

### Implications for Modification

When we insert 12 bytes for DstAS:
- 4 bytes: segment type (AS_SEQUENCE = 2)
- 4 bytes: segment length (1 ASN)
- 4 bytes: ASN value (202032)

Total: 12 bytes, already aligned to 4 bytes (12 % 4 = 0), no padding needed.

**IMPORTANT**: After insertion, we must update:
1. `DstASPathLen`: from 0 to 1 (number of segments)
2. `record_length`: +12 bytes
3. `sample_length`: +12 bytes

---

## RFC 3176 - Original sFlow Research

### Source: [RFC 3176 - InMon Corporation's sFlow](https://datatracker.ietf.org/doc/rfc3176/)

### RFC Status
> "This RFC is labeled as 'Legacy' and was published before a formal source was recorded. It is not endorsed by the IETF and has no formal standing in the IETF standards process."

### AS Path Segment Types

```c
enum as_path_segment_type {
   AS_SET      = 1,  // Unordered set of ASs
   AS_SEQUENCE = 2   // Ordered set of ASs (most common)
};
```

### Extended Gateway Structure (RFC 3176)

```c
struct extended_gateway {
   unsigned int as;              // Autonomous system number of router
   unsigned int src_as;          // Autonomous system number of source
   unsigned int src_peer_as;     // Autonomous system number of source peer
   unsigned int dst_as_path_length;
   unsigned int dst_as_path<>;   // AS path to destination
   // ... communities, localpref (added in v5)
};
```

### Differences Between RFC 3176 and sFlow v5
- sFlow v5 adds the `nexthop` field before the AS fields
- sFlow v5 uses more explicit encoding for path segments
- sFlow v5 adds fields for communities and localpref

---

## Existing Implementations Analysis

### 1. Google gopacket/layers/sflow.go

**Source**: [github.com/google/gopacket/blob/master/layers/sflow.go](https://github.com/google/gopacket/blob/master/layers/sflow.go)

**Parsing approach**:
```go
// DecodeFromBytes iterates through samples
for i := uint32(0); i < s.SampleCount; i++ {
    // Switch on sample type
    switch sampleType {
    case SFlowTypeFlowSample:
        // Decode and advance pointer
    case SFlowTypeCounterSample:
        // Decode and advance pointer
    }
}
```

**AS Path handling**:
```go
type SFlowExtendedGatewayFlowRecord struct {
    // ...
    ASPathCount  uint32
    ASPath       []SFlowASDestination
}

type SFlowASDestination struct {
    Type    SFlowASPathType  // AS_SET or AS_SEQUENCE
    Count   uint32
    Members []uint32         // Array of ASNs
}
```

**Note**: gopacket is designed for parsing only, not for modification/re-encoding.

### 2. Cistern/sflow

**Source**: [github.com/Cistern/sflow](https://github.com/Cistern/sflow)

**Characteristics**:
- Supports both decoding and encoding
- Has a `NewEncoder()` that writes to `io.Writer`
- Round-trip structure: Decode → Modify → Encode

**Encoder file**:
- `encoder.go` - Core encoding logic
- `*_encode_test.go` - Encoding tests

**Limitation**: API not stable ("API stability is not guaranteed")

### 3. Cloudflare goflow/goflow2

**Source**: [github.com/netsampler/goflow2](https://github.com/netsampler/goflow2)

**Architecture**:
```
Datagram → Decoder → Go Structs → Producer → Protobuf/Kafka
```

**Important note**:
> "sFlow is a stateless protocol which sends the full header of a packet with router information (interfaces, destination AS)"

goflow2 doesn't modify sFlow packets, it converts them to an internal format.

### 4. pmacct

**Source**: pmacct source code analysis

**Approach**: pmacct does NOT modify sFlow packets. Instead:
1. Receives sFlow packets
2. Uses an internal BGP daemon to get AS information
3. Enriches data at collector level (after parsing)

```c
// From pmacct's sflow.h
struct sflow_extended_gateway {
    uint32_t nexthop_type;
    // ... fields
    uint32_t dst_as_path_len;
    uint32_t *dst_as_path;  // Pointer, doesn't modify original packet
};
```

### 5. VerizonDigital/vflow

**Source**: [github.com/VerizonDigital/vflow](https://pkg.go.dev/github.com/VerizonDigital/vflow/sflow)

**FlowSample structure**:
```go
type FlowSample struct {
    SequenceNo   uint32
    SourceID     uint32
    SamplingRate uint32
    SamplePool   uint32
    Drops        uint32
    Input        uint32
    Output       uint32
    RecordsNo    uint32
    Records      map[string]Record
}
```

---

## Fix Strategies Analyzed

### Strategy A: Remove DstAS Enrichment
**Pros**: Zero risk, no packet size modification
**Cons**: Doesn't solve the DstAS=0 problem for inbound traffic

### Strategy B: Decode-Modify-Reencode
**Approach**: Use Cistern/sflow to decode, modify Go structs, re-encode.

**Pros**:
- More robust
- Doesn't require manual offset tracking

**Cons**:
- Adds external dependency
- Unstable API
- Performance overhead (double parsing)
- Requires complete implementation of all record types

### Strategy C: Cumulative Offset Tracking
**Approach**: Track inserted bytes and adjust all subsequent offsets.

```go
cumulativeOffset := 0
for i, sample := range datagram.Samples {
    adjustedOffset := sample.Offset + cumulativeOffset
    // ... modify ...
    if bytesInserted > 0 {
        cumulativeOffset += bytesInserted
    }
}
```

**Pros**: Flexible
**Cons**:
- Complex code
- Requires passing state through functions
- Easy to introduce bugs

### Strategy D: Reverse Order Processing ✓ CHOSEN
**Approach**: Process samples from last to first.

**Mathematical logic**:
```
Offset Sample[0] = 100
Offset Sample[1] = 300
Offset Sample[2] = 500

Process Sample[2] first (offset 500):
- Insert 12 bytes at offset 500
- Sample[0] offset 100 → VALID (100 < 500)
- Sample[1] offset 300 → VALID (300 < 500)

Process Sample[1] (offset 300):
- Insert 12 bytes at offset 300
- Sample[0] offset 100 → VALID (100 < 300)

Process Sample[0] (offset 100):
- Insert 12 bytes at offset 100
- COMPLETED
```

**Pros**:
- No external dependencies
- Minimal code change (1 line changed)
- Mathematically correct
- Zero performance overhead

**Cons**: None identified

---

## Implemented Solution

### Modified Code

**File**: `cmd/sflow-enricher/main.go`, function `enrichPacket()`

**Before** (forward iteration):
```go
for _, sample := range datagram.Samples {
    // ... process sample ...
}
```

**After** (reverse iteration):
```go
// CRITICAL: Process samples in REVERSE ORDER to handle packet resizing correctly.
// When ModifyDstAS inserts 12 bytes into a sample, it shifts all subsequent data.
// By processing from last to first, we ensure earlier sample offsets remain valid.
// This is the correct approach for XDR variable-length data modification.
for i := len(datagram.Samples) - 1; i >= 0; i-- {
    sample := datagram.Samples[i]
    // ... process sample ...
}
```

### Why It Works

1. **Invariant preserved**: Offsets of sample[0..N-1] are always valid when processing sample[N]

2. **No dependencies between samples**: Each sample contains its own flow records, there are no cross-sample references

3. **XDR compliance**: Inserting 12 bytes (already 4-byte aligned) doesn't violate XDR rules

4. **Correct length updates**: `ModifyDstAS()` already updates:
   - `DstASPathLen`: 0 → 1
   - `record_length`: +12
   - `sample_length`: +12

---

## Verification and Testing

### Test in Debug Mode

```bash
/usr/local/bin/sflow-enricher -config /etc/sflow-enricher/config.yaml -debug
```

### Verified Output

**SrcAS Enrichment** (outbound traffic):
```
[DEBUG] Gateway AS values map[src_as:0 src_ip:185.54.80.30 ...]
[DEBUG] Enriching SrcAS map[new_as:202032 old_as:0 rule:goline-ipv4 src_ip:185.54.80.30]
```

**DstAS Enrichment** (inbound traffic):
```
[DEBUG] Gateway AS values map[dst_as:0 dst_as_path:[] dst_ip:185.54.80.24 ...]
[DEBUG] Enriching DstAS map[dst_ip:185.54.80.24 new_as:202032 rule:goline-ipv4]
```

**IPv6 Support**:
```
[DEBUG] Enriching SrcAS map[new_as:202032 old_as:0 rule:goline-ipv6 src_ip:2a02:4460:1:1::15]
[DEBUG] Enriching DstAS map[dst_ip:2a02:4460:1:1::22 new_as:202032 rule:goline-ipv6]
```

### Success Metrics

```json
{
  "stats": {
    "packets_received": 34,
    "packets_enriched": 68,
    "packets_dropped": 0,
    "packets_forwarded": 68
  },
  "destinations": [
    {"name": "cloudflare", "healthy": true, "packets_sent": 34},
    {"name": "noction", "healthy": true, "packets_sent": 34}
  ]
}
```

- **Zero packet drop**
- **100% enrichment rate**
- **Both destinations healthy**

---

## Sources and References

### Official Specifications
1. [sFlow v5 Datagram Specification](https://sflow.org/SFLOW-DATAGRAM5.txt)
2. [sFlow Version 5 Full Spec](https://sflow.org/sflow_version_5.txt)
3. [RFC 4506 - XDR Encoding Standard](https://www.rfc-editor.org/rfc/rfc4506)
4. [RFC 3176 - Original sFlow Specification](https://datatracker.ietf.org/doc/rfc3176/)

### Reference Implementations
5. [Google gopacket sflow.go](https://github.com/google/gopacket/blob/master/layers/sflow.go)
6. [Cistern/sflow - Go encoder/decoder](https://github.com/Cistern/sflow)
7. [Cloudflare goflow2](https://github.com/netsampler/goflow2)
8. [VerizonDigital vflow](https://github.com/VerizonDigital/vflow)

### Vendor Documentation
9. [InMon sFlow Agent v5](https://inmon.com/technology/InMon_Agentv5.pdf)
10. [sFlow Wikipedia](https://en.wikipedia.org/wiki/SFlow)

---

## Author

**Paolo Caparrelli** - GOLINE SA
**Email**: soc@goline.ch
**Date**: 23/01/2026

**Co-Authored-By**: Claude Opus 4.5 (Anthropic)
