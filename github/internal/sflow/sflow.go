package sflow

import (
	"encoding/binary"
	"fmt"
	"net"
)

const (
	// sFlow v5 constants
	SFlowVersion5 = 5

	// Address types
	AddressTypeIPv4 = 1
	AddressTypeIPv6 = 2

	// Sample types (enterprise 0)
	SampleTypeFlowSample         = 1
	SampleTypeCounterSample      = 2
	SampleTypeExpandedFlowSample = 3

	// Flow record types (enterprise 0)
	FlowRecordRawPacketHeader = 1
	FlowRecordEthernetFrame   = 2
	FlowRecordIPv4            = 3
	FlowRecordIPv6            = 4
	FlowRecordExtendedSwitch  = 1001
	FlowRecordExtendedRouter  = 1002
	FlowRecordExtendedGateway = 1003
)

// Datagram represents an sFlow v5 datagram
type Datagram struct {
	Version       uint32
	AgentAddrType uint32
	AgentAddr     net.IP
	SubAgentID    uint32
	SequenceNum   uint32
	Uptime        uint32
	NumSamples    uint32
	Samples       []Sample
	Raw           []byte // Original raw bytes
}

// Sample represents a generic sample
type Sample struct {
	Enterprise uint32
	Format     uint32
	Length     uint32
	Data       []byte
	Offset     int // Offset in original packet
}

// FlowSample represents parsed flow sample data
type FlowSample struct {
	SequenceNum   uint32
	SourceIDType  uint32
	SourceIDIndex uint32
	SamplingRate  uint32
	SamplePool    uint32
	Drops         uint32
	Input         uint32
	Output        uint32
	NumRecords    uint32
	Records       []FlowRecord
}

// FlowRecord represents a flow record within a sample
type FlowRecord struct {
	Enterprise uint32
	Format     uint32
	Length     uint32
	Data       []byte
	Offset     int // Offset within the sample data
}

// ExtendedGateway represents extended gateway data
type ExtendedGateway struct {
	NextHopType    uint32
	NextHop        net.IP
	AS             uint32 // Router's own AS
	SrcAS          uint32 // Source AS
	SrcPeerAS      uint32 // Source peer AS
	DstASPathLen   uint32
	DstASPath      []uint32
	CommunitiesLen uint32
	Communities    []uint32
	LocalPref      uint32
}

// Parse parses an sFlow v5 datagram
func Parse(data []byte) (*Datagram, error) {
	if len(data) < 28 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}

	d := &Datagram{Raw: make([]byte, len(data))}
	copy(d.Raw, data)

	offset := 0

	// Version
	d.Version = binary.BigEndian.Uint32(data[offset:])
	offset += 4
	if d.Version != SFlowVersion5 {
		return nil, fmt.Errorf("unsupported sFlow version: %d", d.Version)
	}

	// Agent address type
	d.AgentAddrType = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Agent address
	switch d.AgentAddrType {
	case AddressTypeIPv4:
		d.AgentAddr = net.IP(data[offset : offset+4])
		offset += 4
	case AddressTypeIPv6:
		d.AgentAddr = net.IP(data[offset : offset+16])
		offset += 16
	default:
		return nil, fmt.Errorf("unsupported agent address type: %d", d.AgentAddrType)
	}

	// Sub-agent ID
	d.SubAgentID = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Sequence number
	d.SequenceNum = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Uptime
	d.Uptime = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Number of samples
	d.NumSamples = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Parse samples
	for i := uint32(0); i < d.NumSamples && offset < len(data); i++ {
		if offset+8 > len(data) {
			break
		}

		sampleHeader := binary.BigEndian.Uint32(data[offset:])
		sampleLen := binary.BigEndian.Uint32(data[offset+4:])

		sample := Sample{
			Enterprise: sampleHeader >> 12,
			Format:     sampleHeader & 0xFFF,
			Length:     sampleLen,
			Offset:     offset,
		}

		offset += 8

		if offset+int(sampleLen) > len(data) {
			break
		}

		sample.Data = data[offset : offset+int(sampleLen)]
		offset += int(sampleLen)

		d.Samples = append(d.Samples, sample)
	}

	return d, nil
}

// ParseFlowSample parses flow sample data
func ParseFlowSample(data []byte, expanded bool) (*FlowSample, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("flow sample too short")
	}

	fs := &FlowSample{}
	offset := 0

	fs.SequenceNum = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	if expanded {
		fs.SourceIDType = binary.BigEndian.Uint32(data[offset:])
		offset += 4
		fs.SourceIDIndex = binary.BigEndian.Uint32(data[offset:])
		offset += 4
	} else {
		sourceID := binary.BigEndian.Uint32(data[offset:])
		fs.SourceIDType = sourceID >> 24
		fs.SourceIDIndex = sourceID & 0x00FFFFFF
		offset += 4
	}

	fs.SamplingRate = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	fs.SamplePool = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	fs.Drops = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	if expanded {
		fs.Input = binary.BigEndian.Uint32(data[offset:])
		offset += 4
		// Skip input format for expanded
		offset += 4
		fs.Output = binary.BigEndian.Uint32(data[offset:])
		offset += 4
		// Skip output format for expanded
		offset += 4
	} else {
		fs.Input = binary.BigEndian.Uint32(data[offset:])
		offset += 4
		fs.Output = binary.BigEndian.Uint32(data[offset:])
		offset += 4
	}

	fs.NumRecords = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Parse flow records
	for i := uint32(0); i < fs.NumRecords && offset < len(data); i++ {
		if offset+8 > len(data) {
			break
		}

		recordHeader := binary.BigEndian.Uint32(data[offset:])
		recordLen := binary.BigEndian.Uint32(data[offset+4:])

		record := FlowRecord{
			Enterprise: recordHeader >> 12,
			Format:     recordHeader & 0xFFF,
			Length:     recordLen,
			Offset:     offset,
		}

		offset += 8

		if offset+int(recordLen) > len(data) {
			break
		}

		record.Data = data[offset : offset+int(recordLen)]
		offset += int(recordLen)

		fs.Records = append(fs.Records, record)
	}

	return fs, nil
}

// ParseExtendedGateway parses extended gateway record
func ParseExtendedGateway(data []byte) (*ExtendedGateway, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("extended gateway data too short")
	}

	eg := &ExtendedGateway{}
	offset := 0

	eg.NextHopType = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	switch eg.NextHopType {
	case AddressTypeIPv4:
		eg.NextHop = net.IP(data[offset : offset+4])
		offset += 4
	case AddressTypeIPv6:
		eg.NextHop = net.IP(data[offset : offset+16])
		offset += 16
	default:
		return nil, fmt.Errorf("unsupported next hop type: %d", eg.NextHopType)
	}

	if offset+12 > len(data) {
		return nil, fmt.Errorf("extended gateway data too short for AS fields")
	}

	eg.AS = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	eg.SrcAS = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	eg.SrcPeerAS = binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Parse AS path segments if present
	if offset+4 <= len(data) {
		eg.DstASPathLen = binary.BigEndian.Uint32(data[offset:])
		offset += 4

		for i := uint32(0); i < eg.DstASPathLen && offset+8 <= len(data); i++ {
			// AS path segment: type (4 bytes) + length (4 bytes) + ASNs
			segType := binary.BigEndian.Uint32(data[offset:])
			offset += 4
			_ = segType // segment type (AS_SET, AS_SEQUENCE)

			segLen := binary.BigEndian.Uint32(data[offset:])
			offset += 4

			for j := uint32(0); j < segLen && offset+4 <= len(data); j++ {
				asn := binary.BigEndian.Uint32(data[offset:])
				offset += 4
				eg.DstASPath = append(eg.DstASPath, asn)
			}
		}
	}

	// Communities and local pref may follow
	if offset+4 <= len(data) {
		eg.CommunitiesLen = binary.BigEndian.Uint32(data[offset:])
		offset += 4

		for i := uint32(0); i < eg.CommunitiesLen && offset+4 <= len(data); i++ {
			comm := binary.BigEndian.Uint32(data[offset:])
			offset += 4
			eg.Communities = append(eg.Communities, comm)
		}
	}

	if offset+4 <= len(data) {
		eg.LocalPref = binary.BigEndian.Uint32(data[offset:])
	}

	return eg, nil
}

// GetSrcIPFromRawPacket extracts source IP from raw packet header record
func GetSrcIPFromRawPacket(data []byte) net.IP {
	if len(data) < 20 {
		return nil
	}

	// Raw packet header format:
	// protocol (4) + frame_length (4) + stripped (4) + header_length (4) + header...
	offset := 0

	protocol := binary.BigEndian.Uint32(data[offset:])
	offset += 4
	_ = protocol

	// frame_length
	offset += 4
	// stripped
	offset += 4

	headerLen := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	if offset+int(headerLen) > len(data) || headerLen < 20 {
		return nil
	}

	header := data[offset : offset+int(headerLen)]

	// Check if it's an Ethernet frame (look for IPv4/IPv6)
	// Ethernet header: dst(6) + src(6) + type(2) = 14 bytes
	if len(header) >= 34 {
		etherType := binary.BigEndian.Uint16(header[12:14])

		switch etherType {
		case 0x0800: // IPv4
			if len(header) >= 34 {
				return net.IP(header[26:30]) // Source IP in IPv4 header
			}
		case 0x86DD: // IPv6
			if len(header) >= 54 {
				return net.IP(header[22:38]) // Source IP in IPv6 header
			}
		case 0x8100: // VLAN tagged
			if len(header) >= 38 {
				innerType := binary.BigEndian.Uint16(header[16:18])
				if innerType == 0x0800 && len(header) >= 38 {
					return net.IP(header[30:34])
				}
			}
		}
	}

	return nil
}

// ModifySrcAS modifies the source AS in the raw packet at the specified offset
func ModifySrcAS(packet []byte, sampleOffset int, recordOffset int, newAS uint32) {
	// Calculate absolute offset to SrcAS field in extended gateway
	// Record header: 8 bytes (enterprise/format + length)
	// Extended gateway: NextHopType (4) + NextHop (4 or 16) + AS (4) + SrcAS (4)

	// First, read the sample data to find the record
	if sampleOffset+8 > len(packet) {
		return
	}

	sampleLen := binary.BigEndian.Uint32(packet[sampleOffset+4:])
	sampleDataStart := sampleOffset + 8

	if sampleDataStart+int(sampleLen) > len(packet) {
		return
	}

	// recordOffset is relative to sample data
	absRecordOffset := sampleDataStart + recordOffset

	if absRecordOffset+8 > len(packet) {
		return
	}

	recordLen := binary.BigEndian.Uint32(packet[absRecordOffset+4:])
	recordDataStart := absRecordOffset + 8

	if recordDataStart+int(recordLen) > len(packet) {
		return
	}

	// Read next hop type to determine offset
	if recordDataStart+4 > len(packet) {
		return
	}
	nextHopType := binary.BigEndian.Uint32(packet[recordDataStart:])

	var srcASOffset int
	switch nextHopType {
	case AddressTypeIPv4:
		srcASOffset = recordDataStart + 4 + 4 + 4 // type + ipv4 + AS
	case AddressTypeIPv6:
		srcASOffset = recordDataStart + 4 + 16 + 4 // type + ipv6 + AS
	default:
		return
	}

	if srcASOffset+4 > len(packet) {
		return
	}

	// Write new SrcAS
	binary.BigEndian.PutUint32(packet[srcASOffset:], newAS)
}
