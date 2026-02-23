# Changelog

All notable changes to the sFlow ASN Enricher project.

## [2.3.0] - 2026-02-23

### Fixed
- **RFC compliance: NextHop address_type UNKNOWN (0)**: The sFlow v5 specification defines `enum address_type { UNKNOWN=0, IP_V4=1, IP_V6=2 }` where UNKNOWN uses `void` (0 address bytes). The enricher only handled IPv4 and IPv6, returning error for type=0 and skipping the entire extended_gateway record. Now correctly handles all three RFC-defined address types
- **ParseExtendedGateway minimum size**: Reduced from 20 to 16 bytes to accommodate UNKNOWN nexthop (type(4) + void(0) + AS(4) + SrcAS(4) + SrcPeerAS(4) = 16)
- **Per-type bounds checks**: Added explicit bounds validation for IPv4/IPv6 nexthop address reading instead of relying on a single upfront size check
- **Prometheus /metrics duplicate HELP/TYPE**: Per-destination HELP/TYPE comments were emitted inside the loop, producing duplicates with 2+ destinations. Now emitted once per metric family (Prometheus exposition format compliant)
- **sflow-monitor uint64 underflow**: Rate calculations and `noGw` counter could wrap on uint64 underflow if enricher restarted between polls. Added `safeDelta()` helper and sum-first guard

### Added
- **AddressTypeUnknown constant**: `AddressTypeUnknown = 0` added to sFlow v5 address type constants
- **nextHopAddrSize() helper**: Centralized DRY function returning address size per type (UNKNOWN=0, IPv4=4, IPv6=16), replacing duplicated switch statements in all 4 Modify* functions
- **Prometheus per-destination packets_dropped and bytes_sent**: New counter metrics `sflow_asn_enricher_destination_packets_dropped_total` and `sflow_asn_enricher_destination_bytes_sent_total` per destination label
- **API /status bytes_sent per destination**: `bytes_sent` field added to each destination in the JSON response
- **sflow-monitor Filtered total**: TOTALS section now shows Filtered count when > 0
- **Telegram message redesign**: All 6 alert types completely redesigned with rich formatting:
  - **startup**: Version in title, Listen address, Enrichment Rules with Extended Gateway (1003) details (Out/In fields), Destinations list, sFlow Sources list
  - **shutdown**: Uptime, Stats section (received/enriched %/forwarded/dropped), Destinations with per-dest packets and bytes
  - **destination_down**: Destination name + address, DOWN status, error detail, packets sent before failure
  - **destination_up**: Destination name + address, UP status, recovered indicator
  - **high_drop_rate**: Drop rate vs threshold, interval stats, cumulative totals
  - **ipv6_degraded**: Version in title (aligned with main template), IPv6 fallback detail
- **Telegram uniform spacing**: All message types have consistent empty line separators between sections
- **formatBytesCompact() helper**: Human-readable byte formatting (B/KB/MB/GB) for Telegram shutdown stats

### Changed
- **ModifySrcAS, ModifyRouterAS, ModifySrcPeerAS, ModifyDstAS**: All use `nextHopAddrSize()` with formula-based offset calculation (`type(4) + addr(addrSize) + field_offset`) instead of hardcoded per-type offsets
- **sflow-monitor version**: Updated from hardcoded `1.0.0` to `2.3.0`
- **sflow-monitor ENRICHMENT RULES**: Redesigned with Extended Gateway (1003) in title, per-rule ExtGW field details (Out/In), clean single-line format
- **sflow-monitor FLOW DIAGRAM**: Redesigned with tree layout (├─/└─ branches) for clean multi-destination display
- **Telegram template**: Version displayed in title (`*sFlow ASN Enricher* v2.3.0`), removed generic "Details:" prefix, each alert type has its own structured body
- **Telegram "Whitelist"**: Renamed to "sFlow Sources" with individual IP listing instead of count

### Offset verification (all 3 address types)

| Field | UNKNOWN(0) | IPv4(1) | IPv6(2) |
|-------|-----------|---------|---------|
| RouterAS | +4 | +8 | +20 |
| SrcAS | +8 | +12 | +24 |
| SrcPeerAS | +12 | +16 | +28 |
| DstASPathLen | +16 | +20 | +32 |

## [2.2.2] - 2026-02-21

### Fixed
- **sflow-monitor enrichment % calculation**: Was dividing `packets_enriched / packets_forwarded` instead of `packets_enriched / packets_received`. With 2 destinations, forwarded=2×received so enrichment showed ~49.9% instead of ~99.9%

### Added
- **sflow-monitor "No ExtGW" metric**: New row in ENRICHMENT section showing percentage of packets without Extended Gateway record (counter samples, flows without BGP info). Sum of all 4 metrics now equals 100%
- **sflow-monitor reordered bars**: Dropped moved to last position for better visual priority when issues arise
- **docs/RFC_COMPLIANCE.md**: Complete RFC compliance certification document detailing conformance to sFlow v5, RFC 4506 (XDR), and RFC 3176

## [2.2.1] - 2026-02-21

### Fixed
- **PacketsEnriched double-count**: Counter was incremented once per destination instead of once per packet. With 2 destinations, enrichment stats were inflated 2x (showed ~200% instead of ~100%)
- **IPv6 agent datagram bounds check**: Parse() checked minimum 28 bytes (IPv4 header) but IPv6 agent header requires 40 bytes. Malformed IPv6 packets could cause panic
- **Expanded flow sample bounds check**: ParseFlowSample() checked minimum 32 bytes (standard) but expanded format (type=3) requires 44 bytes. Truncated expanded samples could cause panic
- **Expanded flow Input/Output swap**: Input/Output fields read `format` instead of `value` from `interface_expanded` struct. No impact on enrichment (fields unused) but now correctly parsed per spec
- **SrcAS rule break**: Added `break` after first matching SrcAS rule to prevent multiple overlapping rules from being applied (consistent with DstAS behavior)
- **Version constant**: Updated from "2.1.0" to "2.2.0"

### Verified
- Full code audit: 30/30 synthetic XDR offset tests passed
- Live production verification: enriched/received ratio now correct (99.9% vs previous 199.7%)
- All 6 bugs identified and fixed in single release

## [2.2.0] - 2026-02-21

### Added
- **SrcPeerAS enrichment**: Sets SrcPeerAS to router's own AS when SrcPeerAS=0 (locally-originated traffic)
- **RouterAS enrichment**: Sets the `as` field (router's own AS) when it's 0 (inbound traffic from NE8000)
- **ModifyRouterAS**: New sflow library function for in-place RouterAS modification
- **ModifySrcPeerAS**: New sflow library function for in-place SrcPeerAS modification
- **sflow-monitor**: ENRICHMENT RULES table now shows all 4 enriched fields (SrcAS + SrcPeerAS + DstAS + RouterAS)

### Fixed
- **SrcPeerAS offset bug**: XDR offset was +4 too far (pointed to DstASPathLen instead of SrcPeerAS) — caught during RFC 4506 compliance audit
- **RouterAS overwrite**: RouterAS is now only enriched when value is 0. Non-zero values (e.g., destination peer AS on outbound) are preserved
- **DstAS RouterAS reference**: Fixed to use correct packet reference after DstAS resize

### Verified
- Full compliance audit against sFlow v5 specification (sflow.org/sflow_version_5.txt) and RFC 4506 (XDR encoding)
- All XDR offsets verified: RouterAS (+8/+20), SrcAS (+12/+24), SrcPeerAS (+16/+28), DstASPathLen (+20/+32)
- DstAS insertion verified: AS_SEQUENCE=2, 12-byte insert, record_length and sample_length updates correct

## [2.1.1] - 2026-02-21

### Improved
- **sflow-monitor**: Dynamic box sizing — frame auto-sizes to widest line at every refresh
- **sflow-monitor**: Added ENRICHMENT RULES table showing Name, Network, SetAS, Modifies, Condition
- **sflow-monitor**: Removed unnecessary padding inside brackets in flow diagram
- **sflow-monitor**: Dynamic indent alignment for multi-destination flow diagram
- **API /status**: Added `match_as` and `overwrite` fields to `enrichment_rules` response

## [2.1.0] - 2026-02-21

### Added
- **sflow-monitor**: New standalone ASCII dashboard tool for real-time monitoring
  - Live packet/byte rate with sparkline graphs
  - Enrichment percentage progress bars
  - Flow diagram visualization (source -> enricher -> destinations)
  - Destination health table
  - Raw terminal mode with 'q' to quit
  - Auto-detect terminal width
  - Flags: `-url`, `-interval`, `-no-color`, `-version`
- **Telegram HTTP timeout**: Configurable HTTP client timeout (`http_timeout`, default 15s)
- **Telegram IPv6/IPv4 fallback**: Optional IPv6-first with automatic IPv4 fallback (`ipv6_fallback`)
  - Sends degradation alert (max 1/hour) when IPv6 fails
  - Uses separate IPv4-only client for degradation alerts to avoid recursion
- **Telegram rate limiting**: Cooldown for destination flapping alerts (`flap_cooldown`, default 300s)
- **Telegram high_drop_rate**: Automatic alert when drop rate exceeds threshold (`drop_rate_threshold`, default 5.0%)
- **Makefile**: `build-monitor` and `build-static` targets for sflow-monitor

### Fixed
- **destination_up alert**: Fixed alert type sent as `"destination_down"` when destination recovered (was sending wrong event type)

### Changed
- Telegram alerts now use `http.NewRequestWithContext` with configurable timeout instead of `http.Post`
- Config struct extended with `DropRateThreshold`, `HTTPTimeout`, `FlapCooldown`, `IPv6Fallback` fields
- Makefile updated with monitor build/install/uninstall targets

## [2.0.1] - 2026-02-21

### Fixed
- **JSON marshal error handling**: Added error check for `json.Marshal` in Telegram alert payload construction
- **Systemd Type=notify**: Changed service from `Type=simple` to `Type=notify` with `WatchdogSec=30` and `TimeoutStopSec=30`
- **ParseExtendedGateway bounds check**: Added boundary validation for `segLen` in AS path segment parsing to prevent out-of-bounds read on truncated packets
- **Race condition on LastError/LastCheck**: Moved `LastCheck = now` assignment inside mutex lock in `sendToDestination`

## [2.0.0] - 2026-01-23

### Added
- **HTTP API**: `/metrics` (Prometheus), `/status` (JSON), `/health` endpoints
- **Telegram notifications**: Alerts for startup, shutdown, destination_down, destination_up
- **Systemd integration**: sd_notify protocol with READY/STOPPING/WATCHDOG support
- **Health checks**: Automatic destination monitoring with failover support
- **Source whitelist**: Accept sFlow only from authorized IP addresses/CIDRs
- **Hot-reload**: Configuration reload via SIGHUP without service restart
- **Structured logging**: JSON or text format with configurable level and stats interval
- **Multi-destination forwarding**: Send enriched sFlow to multiple collectors simultaneously
- **DstAS enrichment**: XDR-compliant insertion of destination AS path segment for inbound traffic
- **Security hardening**: Systemd NoNewPrivileges, ProtectSystem, ProtectHome, PrivateTmp

### Changed
- Complete rewrite from single-file script to modular Go project
- Configuration moved to YAML format with full validation
- Enrichment rules now support both SrcAS and DstAS modification

## [1.0.0] - 2026-01-15

### Added
- Initial release
- sFlow v5 proxy with SrcAS enrichment
- Single destination forwarding
- Basic logging
- CIDR-based rule matching
- XDR-compliant packet modification
