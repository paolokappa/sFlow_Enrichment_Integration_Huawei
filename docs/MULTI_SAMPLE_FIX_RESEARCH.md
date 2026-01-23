# Multi-Sample Bug Fix - Documentazione Tecnica Completa

## Indice
1. [Problema Identificato](#problema-identificato)
2. [Ricerca sFlow v5 Specification](#ricerca-sflow-v5-specification)
3. [Ricerca RFC 4506 - XDR Encoding](#ricerca-rfc-4506---xdr-encoding)
4. [Ricerca RFC 3176 - sFlow Original](#ricerca-rfc-3176---sflow-original)
5. [Analisi Implementazioni Esistenti](#analisi-implementazioni-esistenti)
6. [Strategie di Fix Analizzate](#strategie-di-fix-analizzate)
7. [Soluzione Implementata](#soluzione-implementata)
8. [Verifica e Test](#verifica-e-test)

---

## Problema Identificato

### Contesto
Il sFlow ASN Enricher per Huawei NetEngine deve modificare i pacchetti sFlow v5 per:
1. **SrcAS**: Impostare AS202032 quando `SrcAS=0` e `src_ip` appartiene a `185.54.80.0/22` o `2a02:4460::/32`
2. **DstAS**: Inserire AS202032 nel `DstASPath` quando è vuoto e `dst_ip` appartiene alle reti Goline

### Il Bug Multi-Sample
Quando `ModifyDstAS()` inserisce 12 byte per il segmento AS path:

```
PRIMA della modifica:
[Header Datagram][Sample0 @ offset 100][Sample1 @ offset 300][Sample2 @ offset 500]

DOPO la modifica di Sample0 (+12 byte):
[Header Datagram][Sample0+12 @ offset 100][Sample1 @ offset 312][Sample2 @ offset 512]
                                           ↑                      ↑
                                           Gli offset memorizzati (300, 500) sono ora INVALIDI!
```

Il problema: `datagram.Samples[1].Offset` contiene ancora `300` (calcolato dal pacchetto originale), ma nel nuovo pacchetto Sample1 si trova a `312`.

---

## Ricerca sFlow v5 Specification

### Fonte: [sflow.org/SFLOW-DATAGRAM5.txt](https://sflow.org/SFLOW-DATAGRAM5.txt)

### Struttura Datagram v5

```c
struct sample_datagram_v5 {
   address agent_address;        // IP address of sampling agent
   unsigned int sub_agent_id;    // Distinguishes datagram streams
   unsigned int sequence_number; // Incremented with each datagram
   unsigned int uptime;          // Milliseconds since boot
   sample_record samples<>;      // Variable-length array of samples
};
```

### Struttura Sample Record

```c
struct sample_record {
   data_format sample_type;      // 4 bytes: enterprise << 12 | format
   opaque sample_data<>;         // Variable-length opaque data
};
```

**Chiave**: L'uso di `opaque<>` indica dati di lunghezza variabile con prefisso di lunghezza (XDR encoding).

### Struttura Flow Sample

```c
struct flow_sample {
   unsigned int sequence_number;
   sflow_data_source source_id;
   unsigned int sampling_rate;
   unsigned int sample_pool;
   unsigned int drops;
   interface input;
   interface output;
   flow_record flow_records<>;   // Variable-length array di record
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

### Nota Critica dalla Specifica

> "Applications receiving sFlow data must always use the opaque length information when decoding opaque<> structures so that encountering extended structures will not cause decoding errors."

> "Adding length fields to structures provides for two different types of extensibility. The second type involves being able to extend the length of an already-existing structure in a way that need not break compatibility with collectors which understand an older version."

---

## Ricerca RFC 4506 - XDR Encoding

### Fonte: [RFC 4506 - XDR: External Data Representation Standard](https://www.rfc-editor.org/rfc/rfc4506)

### Principi Fondamentali XDR

1. **Unità Base**: 4 byte, 32 bit, serializzati in big-endian
2. **Allineamento**: Tutti i dati devono essere allineati a 4 byte
3. **Tipi più piccoli**: Occupano comunque 4 byte dopo l'encoding

### Variable-Length Opaque Data

Dalla RFC 4506, Sezione 4.10:

```
opaque identifier<m>;    // con limite massimo m
opaque identifier<>;     // senza limite (max 2^32 - 1)
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

### Implicazioni per la Modifica

Quando inseriamo 12 byte per DstAS:
- 4 byte: segment type (AS_SEQUENCE = 2)
- 4 byte: segment length (1 ASN)
- 4 byte: ASN value (202032)

Totale: 12 byte, già allineato a 4 byte (12 % 4 = 0), nessun padding necessario.

**IMPORTANTE**: Dopo l'inserimento, dobbiamo aggiornare:
1. `DstASPathLen`: da 0 a 1 (numero di segmenti)
2. `record_length`: +12 byte
3. `sample_length`: +12 byte

---

## Ricerca RFC 3176 - sFlow Original

### Fonte: [RFC 3176 - InMon Corporation's sFlow](https://datatracker.ietf.org/doc/rfc3176/)

### Status RFC
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

### Differenze tra RFC 3176 e sFlow v5
- sFlow v5 aggiunge il campo `nexthop` prima degli AS
- sFlow v5 usa encoding più esplicito per i path segment
- sFlow v5 aggiunge campi per communities e localpref

---

## Analisi Implementazioni Esistenti

### 1. Google gopacket/layers/sflow.go

**Fonte**: [github.com/google/gopacket/blob/master/layers/sflow.go](https://github.com/google/gopacket/blob/master/layers/sflow.go)

**Approccio al parsing**:
```go
// DecodeFromBytes itera attraverso i sample
for i := uint32(0); i < s.SampleCount; i++ {
    // Switch su sample type
    switch sampleType {
    case SFlowTypeFlowSample:
        // Decodifica e avanza il puntatore
    case SFlowTypeCounterSample:
        // Decodifica e avanza il puntatore
    }
}
```

**Gestione AS Path**:
```go
type SFlowExtendedGatewayFlowRecord struct {
    // ...
    ASPathCount  uint32
    ASPath       []SFlowASDestination
}

type SFlowASDestination struct {
    Type    SFlowASPathType  // AS_SET or AS_SEQUENCE
    Count   uint32
    Members []uint32         // Array di ASN
}
```

**Nota**: gopacket è progettato solo per il parsing, non per la modifica/re-encoding.

### 2. Cistern/sflow

**Fonte**: [github.com/Cistern/sflow](https://github.com/Cistern/sflow)

**Caratteristiche**:
- Supporta sia decoding che encoding
- Ha un `NewEncoder()` che scrive su `io.Writer`
- Struttura round-trip: Decode → Modify → Encode

**File encoder**:
- `encoder.go` - Core encoding logic
- `*_encode_test.go` - Test per encoding

**Limitazione**: API non stabile ("API stability is not guaranteed")

### 3. Cloudflare goflow/goflow2

**Fonte**: [github.com/netsampler/goflow2](https://github.com/netsampler/goflow2)

**Architettura**:
```
Datagram → Decoder → Go Structs → Producer → Protobuf/Kafka
```

**Nota importante**:
> "sFlow is a stateless protocol which sends the full header of a packet with router information (interfaces, destination AS)"

goflow2 non modifica i pacchetti sFlow, li converte in un formato interno.

### 4. pmacct

**Fonte**: Analisi del codice sorgente pmacct

**Approccio**: pmacct NON modifica i pacchetti sFlow. Invece:
1. Riceve pacchetti sFlow
2. Usa un BGP daemon interno per ottenere informazioni AS
3. Arricchisce i dati a livello di collector (dopo il parsing)

```c
// Da sflow.h di pmacct
struct sflow_extended_gateway {
    uint32_t nexthop_type;
    // ... campi
    uint32_t dst_as_path_len;
    uint32_t *dst_as_path;  // Puntatore, non modifica il pacchetto originale
};
```

### 5. VerizonDigital/vflow

**Fonte**: [github.com/VerizonDigital/vflow](https://pkg.go.dev/github.com/VerizonDigital/vflow/sflow)

**Struttura FlowSample**:
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

## Strategie di Fix Analizzate

### Strategia A: Rimuovere DstAS Enrichment
**Pro**: Zero rischi, nessuna modifica di dimensione pacchetto
**Contro**: Non risolve il problema DstAS=0 per traffico inbound

### Strategia B: Decode-Modify-Reencode
**Approccio**: Usare Cistern/sflow per decodificare, modificare le struct Go, re-encodare.

**Pro**:
- Più robusto
- Non richiede tracking manuale degli offset

**Contro**:
- Aggiunge dipendenza esterna
- API non stabile
- Overhead di performance (doppio parsing)
- Richiede implementazione completa di tutti i tipi di record

### Strategia C: Cumulative Offset Tracking
**Approccio**: Tracciare i byte inseriti e aggiustare tutti gli offset successivi.

```go
cumulativeOffset := 0
for i, sample := range datagram.Samples {
    adjustedOffset := sample.Offset + cumulativeOffset
    // ... modifica ...
    if bytesInserted > 0 {
        cumulativeOffset += bytesInserted
    }
}
```

**Pro**: Flessibile
**Contro**:
- Codice complesso
- Richiede passare stato attraverso le funzioni
- Facile introdurre bug

### Strategia D: Reverse Order Processing ✓ SCELTA
**Approccio**: Processare i sample dall'ultimo al primo.

**Logica matematica**:
```
Offset Sample[0] = 100
Offset Sample[1] = 300
Offset Sample[2] = 500

Processo Sample[2] prima (offset 500):
- Inserisco 12 byte a offset 500
- Sample[0] offset 100 → VALIDO (100 < 500)
- Sample[1] offset 300 → VALIDO (300 < 500)

Processo Sample[1] (offset 300):
- Inserisco 12 byte a offset 300
- Sample[0] offset 100 → VALIDO (100 < 300)

Processo Sample[0] (offset 100):
- Inserisco 12 byte a offset 100
- COMPLETATO
```

**Pro**:
- Nessuna dipendenza esterna
- Codice minimale (1 riga cambiata)
- Matematicamente corretto
- Zero overhead di performance

**Contro**: Nessuno identificato

---

## Soluzione Implementata

### Codice Modificato

**File**: `cmd/sflow-enricher/main.go`, funzione `enrichPacket()`

**Prima** (iterazione forward):
```go
for _, sample := range datagram.Samples {
    // ... processo sample ...
}
```

**Dopo** (iterazione reverse):
```go
// CRITICAL: Process samples in REVERSE ORDER to handle packet resizing correctly.
// When ModifyDstAS inserts 12 bytes into a sample, it shifts all subsequent data.
// By processing from last to first, we ensure earlier sample offsets remain valid.
// This is the correct approach for XDR variable-length data modification.
for i := len(datagram.Samples) - 1; i >= 0; i-- {
    sample := datagram.Samples[i]
    // ... processo sample ...
}
```

### Perché Funziona

1. **Invariante preservato**: Gli offset di sample[0..N-1] sono sempre validi quando processo sample[N]

2. **Nessuna dipendenza tra sample**: Ogni sample contiene i propri flow record, non ci sono riferimenti cross-sample

3. **XDR compliance**: L'inserimento di 12 byte (già allineato a 4 byte) non viola le regole XDR

4. **Aggiornamenti length corretti**: `ModifyDstAS()` aggiorna già:
   - `DstASPathLen`: 0 → 1
   - `record_length`: +12
   - `sample_length`: +12

---

## Verifica e Test

### Test in Debug Mode

```bash
/usr/local/bin/sflow-enricher -config /etc/sflow-enricher/config.yaml -debug
```

### Output Verificato

**SrcAS Enrichment** (traffico outbound):
```
[DEBUG] Gateway AS values map[src_as:0 src_ip:185.54.80.30 ...]
[DEBUG] Enriching SrcAS map[new_as:202032 old_as:0 rule:goline-ipv4 src_ip:185.54.80.30]
```

**DstAS Enrichment** (traffico inbound):
```
[DEBUG] Gateway AS values map[dst_as:0 dst_as_path:[] dst_ip:185.54.80.24 ...]
[DEBUG] Enriching DstAS map[dst_ip:185.54.80.24 new_as:202032 rule:goline-ipv4]
```

**IPv6 Support**:
```
[DEBUG] Enriching SrcAS map[new_as:202032 old_as:0 rule:goline-ipv6 src_ip:2a02:4460:1:1::15]
[DEBUG] Enriching DstAS map[dst_ip:2a02:4460:1:1::22 new_as:202032 rule:goline-ipv6]
```

### Metriche di Successo

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
- **Entrambe le destinazioni healthy**

---

## Fonti e Riferimenti

### Specifiche Ufficiali
1. [sFlow v5 Datagram Specification](https://sflow.org/SFLOW-DATAGRAM5.txt)
2. [sFlow Version 5 Full Spec](https://sflow.org/sflow_version_5.txt)
3. [RFC 4506 - XDR Encoding Standard](https://www.rfc-editor.org/rfc/rfc4506)
4. [RFC 3176 - Original sFlow Specification](https://datatracker.ietf.org/doc/rfc3176/)

### Implementazioni di Riferimento
5. [Google gopacket sflow.go](https://github.com/google/gopacket/blob/master/layers/sflow.go)
6. [Cistern/sflow - Go encoder/decoder](https://github.com/Cistern/sflow)
7. [Cloudflare goflow2](https://github.com/netsampler/goflow2)
8. [VerizonDigital vflow](https://github.com/VerizonDigital/vflow)

### Documentazione Vendor
9. [InMon sFlow Agent v5](https://inmon.com/technology/InMon_Agentv5.pdf)
10. [sFlow Wikipedia](https://en.wikipedia.org/wiki/SFlow)

---

## Autore

**Paolo Caparrelli** - GOLINE SA
**Email**: soc@goline.ch
**Data**: 23/01/2026

**Co-Authored-By**: Claude Opus 4.5 (Anthropic)
