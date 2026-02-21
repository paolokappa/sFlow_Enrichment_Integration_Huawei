# RFC Compliance Certification

## sFlow ASN Enricher v2.2.2

**Date:** 2026-02-21
**Auditor:** Paolo Caparrelli (GOLINE SA) with Claude Opus 4.6 (Anthropic)
**Status:** COMPLIANT

---

## Applicable Standards

| Standard | Title | Relevance |
|----------|-------|-----------|
| [sFlow v5](https://sflow.org/SFLOW-DATAGRAM5.txt) | sFlow Datagram Version 5 | Primary datagram format specification |
| [RFC 4506](https://www.rfc-editor.org/rfc/rfc4506) | XDR: External Data Representation Standard | Binary encoding rules |
| [RFC 3176](https://datatracker.ietf.org/doc/rfc3176/) | InMon Corporation's sFlow | Original sFlow specification |

---

## 1. sFlow v5 Datagram Compliance

### 1.1 Datagram Header Parsing

**Specification (sflow.org/SFLOW-DATAGRAM5.txt):**
```c
struct sample_datagram_v5 {
   unsigned int version;            // Must be 5
   address agent_address;           // Variable-length: type(4) + addr(4|16)
   unsigned int sub_agent_id;
   unsigned int sequence_number;
   unsigned int uptime;
   sample_record samples<>;         // XDR variable-length array
};
```

**Implementation (`sflow.go:Parse()`):**

| Field | Spec Offset (IPv4) | Spec Offset (IPv6) | Code Offset | Status |
|-------|-------------------|-------------------|-------------|--------|
| Version | 0 | 0 | `data[0:4]` | COMPLIANT |
| AgentAddrType | 4 | 4 | `data[4:8]` | COMPLIANT |
| AgentAddr (IPv4) | 8-11 | — | `data[8:12]` | COMPLIANT |
| AgentAddr (IPv6) | — | 8-23 | `data[8:24]` | COMPLIANT |
| SubAgentID | 12 | 24 | `data[offset:offset+4]` | COMPLIANT |
| SequenceNum | 16 | 28 | `data[offset:offset+4]` | COMPLIANT |
| Uptime | 20 | 32 | `data[offset:offset+4]` | COMPLIANT |
| NumSamples | 24 | 36 | `data[offset:offset+4]` | COMPLIANT |

**Bounds checking:**
- IPv4 agent: minimum 28 bytes verified before parsing
- IPv6 agent: minimum 40 bytes verified before parsing
- Each sample's `opaque<>` length is validated against remaining packet data

**Verdict:** COMPLIANT

### 1.2 Sample Record Parsing

**Specification:**
```c
struct sample_record {
   data_format sample_type;      // enterprise(20 bits) << 12 | format(12 bits)
   opaque sample_data<>;         // Length-prefixed variable-length data
};
```

**Implementation (`sflow.go:Parse()`):**

| Element | Spec | Implementation | Status |
|---------|------|----------------|--------|
| Enterprise extraction | `sampleType >> 12` | `sampleType >> 12` | COMPLIANT |
| Format extraction | `sampleType & 0xFFF` | `sampleType & 0xFFF` | COMPLIANT |
| Data length prefix | 4-byte unsigned int | `binary.BigEndian.Uint32()` | COMPLIANT |
| Enterprise 0 filter | Process standard records | `enterprise == 0` check | COMPLIANT |
| Unknown sample skip | Use opaque length to skip | `offset += 8 + int(sampleLen)` | COMPLIANT |

**Supported sample types (enterprise 0):**

| Format | Type | Support |
|--------|------|---------|
| 1 | Flow Sample | FULL — parsed, enriched |
| 2 | Counter Sample | SKIPPED — no enrichment applicable |
| 3 | Expanded Flow Sample | FULL — parsed, enriched |
| 4 | Expanded Counter Sample | SKIPPED — no enrichment applicable |

**From specification:**
> "Applications receiving sFlow data must always use the opaque length information when decoding opaque<> structures so that encountering extended structures will not cause decoding errors."

**Compliance:** The parser uses opaque length to skip unknown records/samples, ensuring forward compatibility with future extensions.

**Verdict:** COMPLIANT

### 1.3 Flow Sample Parsing

**Specification:**
```c
struct flow_sample {                    // format = 1
   unsigned int sequence_number;        // +0
   sflow_data_source source_id;         // +4
   unsigned int sampling_rate;          // +8
   unsigned int sample_pool;            // +12
   unsigned int drops;                  // +16
   interface input;                     // +20 (4 bytes)
   interface output;                    // +24 (4 bytes)
   flow_record flow_records<>;          // +28
};

struct expanded_flow_sample {           // format = 3
   unsigned int sequence_number;        // +0
   sflow_data_source_expanded source_id; // +4 (8 bytes: type + index)
   unsigned int sampling_rate;          // +12
   unsigned int sample_pool;            // +16
   unsigned int drops;                  // +20
   interface_expanded input;            // +24 (8 bytes: format + value)
   interface_expanded output;           // +32 (8 bytes: format + value)
   flow_record flow_records<>;          // +40
};
```

**Implementation (`sflow.go:ParseFlowSample()`):**

| Variant | Min Size (Spec) | Min Size (Code) | Records Start | Status |
|---------|----------------|-----------------|---------------|--------|
| Standard (format=1) | 32 bytes | 32 bytes | offset 28 | COMPLIANT |
| Expanded (format=3) | 44 bytes | 44 bytes | offset 40 | COMPLIANT |

**Expanded format differences handled:**
- `source_id`: 8 bytes (type + index) vs 4 bytes — offset adjusted
- `input`/`output`: `interface_expanded` = {format(4), value(4)} — format field skipped, value read
- Flow records start at offset 40 (expanded) vs 28 (standard)

**Verdict:** COMPLIANT

### 1.4 Extended Gateway Record (Type 1003)

**Specification:**
```c
struct extended_gateway {
   address nexthop;              // Variable: type(4) + addr(4|16)
   unsigned int as;              // Router's own AS
   unsigned int src_as;          // Source AS
   unsigned int src_peer_as;     // Source peer AS
   unsigned int dst_as_path_length;
   as_path_type dst_as_path<>;
   unsigned int communities<>;
   unsigned int localpref;
};
```

**Implementation (`sflow.go:ParseExtendedGateway()`):**

**IPv4 NextHop Offsets (NextHopType=1):**

| Field | Spec Calculation | Code Offset | Status |
|-------|-----------------|-------------|--------|
| NextHopType | +0 | `recordData[0:4]` | COMPLIANT |
| NextHop (IPv4) | +4 to +7 | `recordData[4:8]` | COMPLIANT |
| AS (RouterAS) | +8 to +11 | `recordData[8:12]` | COMPLIANT |
| SrcAS | +12 to +15 | `recordData[12:16]` | COMPLIANT |
| SrcPeerAS | +16 to +19 | `recordData[16:20]` | COMPLIANT |
| DstASPathLen | +20 to +23 | `recordData[20:24]` | COMPLIANT |

**IPv6 NextHop Offsets (NextHopType=2):**

| Field | Spec Calculation | Code Offset | Status |
|-------|-----------------|-------------|--------|
| NextHopType | +0 | `recordData[0:4]` | COMPLIANT |
| NextHop (IPv6) | +4 to +19 | `recordData[4:20]` | COMPLIANT |
| AS (RouterAS) | +20 to +23 | `recordData[20:24]` | COMPLIANT |
| SrcAS | +24 to +27 | `recordData[24:28]` | COMPLIANT |
| SrcPeerAS | +28 to +31 | `recordData[28:32]` | COMPLIANT |
| DstASPathLen | +32 to +35 | `recordData[32:36]` | COMPLIANT |

**AS Path Segment Parsing:**

| Element | Spec | Implementation | Status |
|---------|------|----------------|--------|
| Segment type | `as_path_segment_type` (4 bytes) | `binary.BigEndian.Uint32()` | COMPLIANT |
| ASN count | 4-byte unsigned int | `binary.BigEndian.Uint32()` | COMPLIANT |
| ASN array | `unsigned int as_number<>` | Sequential 4-byte reads | COMPLIANT |
| Bounds check | Validate `segLen` range | `segLen > 1000` sanity check | COMPLIANT |

**Verdict:** COMPLIANT

---

## 2. RFC 4506 — XDR Encoding Compliance

### 2.1 Fundamental Principles

| Principle | RFC 4506 Requirement | Implementation | Status |
|-----------|---------------------|----------------|--------|
| Base unit | 4 bytes (32 bits) | All fields read as `uint32` | COMPLIANT |
| Byte order | Big-endian (MSB first) | `encoding/binary.BigEndian` | COMPLIANT |
| Alignment | All data aligned to 4-byte boundaries | All offsets are multiples of 4 | COMPLIANT |
| Integer encoding | Unsigned 32-bit big-endian | `binary.BigEndian.Uint32()` | COMPLIANT |

### 2.2 Variable-Length Opaque Data

**RFC 4506, Section 4.10:**
> "Variable-length opaque data is defined as a sequence of n arbitrary bytes to be the number n encoded as an unsigned integer, and followed by the n bytes of the sequence."

**Sample records:** Each sample uses `opaque sample_data<>`:
- Length prefix: 4 bytes, read with `binary.BigEndian.Uint32()`
- Data: `length` bytes following the prefix
- Parser advances by `8 + length` (type(4) + length(4) + data)

**Flow records:** Each record uses `opaque record_data<>`:
- Same pattern: enterprise/format(4) + length(4) + data
- Unknown records skipped using length field

**Verdict:** COMPLIANT

### 2.3 Modification Operations — XDR Alignment Audit

#### ModifySrcAS (in-place, 0 bytes inserted)

| Step | Operation | Alignment Check |
|------|-----------|-----------------|
| Read NextHopType | 4-byte read at record start | 4-byte aligned |
| Calculate SrcAS offset | type(4) + addr(4\|16) + AS(4) | Sum of 4-byte multiples |
| Write SrcAS | 4-byte write at computed offset | 4-byte aligned |
| Packet size | Unchanged | No alignment change |

**Verdict:** COMPLIANT — In-place 4-byte overwrite, no structural modification

#### ModifySrcPeerAS (in-place, 0 bytes inserted)

| Step | Operation | Alignment Check |
|------|-----------|-----------------|
| Calculate offset | type(4) + addr(4\|16) + AS(4) + SrcAS(4) | Sum of 4-byte multiples |
| Write SrcPeerAS | 4-byte write at computed offset | 4-byte aligned |
| Packet size | Unchanged | No alignment change |

**Verdict:** COMPLIANT — In-place 4-byte overwrite

#### ModifyRouterAS (in-place, 0 bytes inserted)

| Step | Operation | Alignment Check |
|------|-----------|-----------------|
| Calculate offset | type(4) + addr(4\|16) | Sum of 4-byte multiples |
| Write RouterAS | 4-byte write at computed offset | 4-byte aligned |
| Packet size | Unchanged | No alignment change |

**Verdict:** COMPLIANT — In-place 4-byte overwrite

#### ModifyDstAS (resize, +12 bytes inserted)

| Step | Operation | Alignment Check |
|------|-----------|-----------------|
| Calculate DstASPathLen offset | type(4) + addr(4\|16) + AS(4) + SrcAS(4) + SrcPeerAS(4) | Sum of 4-byte multiples |
| Verify DstASPathLen == 0 | Read 4 bytes | 4-byte aligned |
| Update DstASPathLen | Write `1` (4 bytes) | 4-byte aligned |
| Insert point | DstASPathLen + 4 | 4-byte aligned |
| Insert segment type | AS_SEQUENCE = 2 (4 bytes) | 4-byte aligned |
| Insert segment length | 1 (4 bytes) | 4-byte aligned |
| Insert ASN value | 4 bytes | 4-byte aligned |
| Total insertion | 12 bytes (3 x 4) | 4-byte aligned |
| Update record_length | +12 bytes | 4-byte aligned |
| Update sample_length | +12 bytes | 4-byte aligned |
| Padding required | 12 mod 4 = 0 | No padding needed |

**RFC 4506 padding rule:**
> "If n is not a multiple of four, then the n bytes are followed by enough (0 to 3) residual zero bytes to make the total byte count a multiple of four."

12 bytes is already a multiple of 4. No padding bytes required.

**Verdict:** COMPLIANT — All sizes and offsets maintain 4-byte XDR alignment

---

## 3. Multi-Sample Integrity

### 3.1 The Problem

sFlow v5 datagrams may contain multiple samples. When `ModifyDstAS` inserts 12 bytes into one sample, all subsequent sample offsets shift.

**Specification context:**
The `samples<>` array is a contiguous sequence of `sample_record` entries. Each record's offset depends on the cumulative size of all preceding records.

### 3.2 The Solution — Reverse-Order Processing

**Implementation:** Samples are processed from last to first (reverse index order).

**Mathematical proof:**

Given samples at offsets `O[0] < O[1] < ... < O[n-1]`:

1. Process `Sample[n-1]` at `O[n-1]`:
   - Insert 12 bytes at position `O[n-1]`
   - All offsets `O[0]..O[n-2]` remain valid (they are < `O[n-1]`)

2. Process `Sample[n-2]` at `O[n-2]`:
   - Insert 12 bytes at position `O[n-2]`
   - All offsets `O[0]..O[n-3]` remain valid (they are < `O[n-2]`)

3. Continue until `Sample[0]` is processed

**Invariant:** When processing `Sample[i]`, all offsets `O[0]..O[i-1]` are valid because insertions only occur at positions >= `O[i]` > `O[j]` for all `j < i`.

**Verdict:** COMPLIANT — Mathematically proven offset integrity

---

## 4. Raw Packet Header Parsing (Type 1)

**Specification:**
```c
struct sampled_header {
   header_protocol protocol;     // 4 bytes (1=Ethernet, 11=IPv4, 12=IPv6)
   unsigned int frame_length;    // 4 bytes
   unsigned int stripped;        // 4 bytes
   opaque header<>;              // Variable: length(4) + header bytes
};
```

**Implementation (`sflow.go:GetSrcDstIPFromRawPacket()`):**

| Layer | Parse Step | Bounds Check | Status |
|-------|-----------|--------------|--------|
| sFlow header | protocol(4) + frame_length(4) + stripped(4) + header_length(4) | `len(data) >= 20` | COMPLIANT |
| Ethernet | dst(6) + src(6) + etherType(2) | `len(header) >= 14` | COMPLIANT |
| IPv4 | Header at Ethernet+14, SrcIP at +12, DstIP at +16 | `len(header) >= 34` | COMPLIANT |
| IPv6 | Header at Ethernet+14, SrcIP at +8, DstIP at +24 | `len(header) >= 54` | COMPLIANT |
| 802.1Q VLAN | Tag at +14, inner EtherType at +16 | `len(header) >= 38` (IPv4) | COMPLIANT |

**Verdict:** COMPLIANT

---

## 5. AS Path Segment Types

**Specification (RFC 3176 / sFlow v5):**
```c
enum as_path_segment_type {
   AS_SET      = 1,   // Unordered set of ASs
   AS_SEQUENCE = 2    // Ordered set of ASs (BGP standard)
};
```

**Implementation:** DstAS insertion uses `AS_SEQUENCE = 2`, which is the correct type for a standard BGP AS path. Single-AS paths in BGP always use AS_SEQUENCE.

**Verdict:** COMPLIANT

---

## 6. Safety and Robustness

### 6.1 Bounds Checking

Every byte read/write operation is preceded by a bounds check:

| Function | Checks Performed |
|----------|-----------------|
| `Parse()` | Packet minimum length (28/40), address type validation, per-sample bounds |
| `ParseFlowSample()` | Minimum size (32/44), per-record bounds, expanded format detection |
| `ParseExtendedGateway()` | Record data bounds, next-hop type validation, segment count sanity (<=1000) |
| `GetSrcDstIPFromRawPacket()` | Header length validation, Ethernet frame bounds, IP header bounds |
| `ModifySrcAS()` | Sample bounds, record bounds, next-hop type, write offset bounds |
| `ModifySrcPeerAS()` | Same as ModifySrcAS with adjusted offset |
| `ModifyRouterAS()` | Same as ModifySrcAS with adjusted offset |
| `ModifyDstAS()` | All of the above + insert point bounds + new packet allocation |

No operation reads or writes beyond validated boundaries.

### 6.2 Unknown Record Handling

From sFlow v5 specification:
> "Applications receiving sFlow data must always use the opaque length information when decoding opaque<> structures."

The parser:
- Skips unknown enterprise values (enterprise != 0)
- Skips unknown sample formats (format != 1, 2, 3, 4)
- Skips unknown flow record types (format != 1, 1003)
- Uses `opaque<>` length prefix to skip over any unknown structure

### 6.3 Non-Destructive Enrichment

| Condition | Behavior |
|-----------|----------|
| SrcAS != 0 and overwrite=false | No modification |
| SrcPeerAS != 0 | No modification |
| RouterAS != 0 | No modification |
| DstASPathLen > 0 | No modification |
| Source IP doesn't match any rule | Packet forwarded unmodified |
| No Extended Gateway record | Packet forwarded unmodified |
| No Raw Packet Header record | Packet forwarded unmodified |

The enricher never corrupts existing valid data. Modifications are strictly additive (fill empty fields) or insert-only (DstAS path).

---

## 7. Verification Evidence

### 7.1 Synthetic XDR Offset Tests

30 synthetic packet tests executed covering:
- ModifySrcAS for IPv4 and IPv6
- ModifyRouterAS for IPv4 and IPv6
- ModifySrcPeerAS for IPv4 and IPv6
- ModifyDstAS for IPv4 and IPv6 (packet resize)
- DstAS + RouterAS combined (resize then in-place)
- All tests: **30/30 PASS**

### 7.2 Live Production Verification

```
Service:     sflow-enricher v2.2.2
Uptime:      continuous operation since 2026-01-23
Source:      Huawei NetEngine 8000 M14 (sFlow agent)

packets_received:   338,695
packets_enriched:   338,218
packets_dropped:           0
packets_filtered:          0
enrichment_ratio:     99.86%

No ExtGW:    0.14% (counter samples without Extended Gateway)
Dropped:     0.00%
```

### 7.3 Cross-Reference with Existing Implementations

The parser design was cross-referenced with:

| Implementation | Author | Conformance Check |
|---------------|--------|-------------------|
| [gopacket/layers/sflow.go](https://github.com/google/gopacket/blob/master/layers/sflow.go) | Google | Offset calculations match |
| [Cistern/sflow](https://github.com/Cistern/sflow) | Cistern | Structure definitions match |
| [goflow2](https://github.com/netsampler/goflow2) | Cloudflare | Parsing approach aligned |
| [vflow](https://github.com/VerizonDigital/vflow) | Verizon Digital | Field mappings consistent |

---

## 8. Compliance Summary

| Area | Standard | Status | Notes |
|------|----------|--------|-------|
| Datagram header | sFlow v5 | COMPLIANT | IPv4/IPv6 agent addressing |
| Sample records | sFlow v5 | COMPLIANT | Opaque length-based parsing |
| Flow samples | sFlow v5 | COMPLIANT | Standard + Expanded formats |
| Extended Gateway | sFlow v5 | COMPLIANT | All field offsets verified |
| XDR encoding | RFC 4506 | COMPLIANT | 4-byte alignment, big-endian |
| XDR opaque data | RFC 4506 | COMPLIANT | Length-prefixed variable data |
| XDR modification | RFC 4506 | COMPLIANT | 12-byte insert maintains alignment |
| AS path types | RFC 3176 | COMPLIANT | AS_SEQUENCE=2 for single-AS path |
| Multi-sample | sFlow v5 | COMPLIANT | Reverse-order offset integrity |
| Bounds safety | Best practice | COMPLIANT | Every read/write bounds-checked |
| Forward compat | sFlow v5 | COMPLIANT | Unknown records skipped via length |

---

## Certification Statement

This document certifies that **sFlow ASN Enricher v2.2.2** has been audited against the sFlow Version 5 specification (sflow.org/SFLOW-DATAGRAM5.txt), RFC 4506 (XDR External Data Representation Standard), and RFC 3176 (InMon Corporation's sFlow). All parsing, modification, and encoding operations conform to the requirements of these standards.

The enrichment operations (SrcAS, SrcPeerAS, RouterAS, DstAS) modify only the intended fields within the Extended Gateway record (type 1003), preserve all other data, maintain XDR 4-byte alignment, and correctly update associated length fields when packet resize is required.

---

**Paolo Caparrelli** — GOLINE SA
**Email:** soc@goline.ch
**Date:** 2026-02-21

**Co-Authored-By:** Claude Opus 4.6 (Anthropic)
