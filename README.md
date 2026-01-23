# sFlow Enricher

[![Version](https://img.shields.io/badge/version-2.0.0-blue.svg)]()
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg)]()
[![License](https://img.shields.io/badge/license-MIT-green.svg)]()

Proxy sFlow ad alte prestazioni che arricchisce i pacchetti con informazioni ASN prima di inoltrarli ai collector.

## Indice

- [Panoramica](#panoramica)
- [Problema Risolto](#problema-risolto)
- [Architettura](#architettura)
- [Funzionalità](#funzionalità)
- [Requisiti](#requisiti)
- [Installazione](#installazione)
- [Configurazione](#configurazione)
- [Utilizzo](#utilizzo)
- [API HTTP](#api-http)
- [Monitoraggio](#monitoraggio)
- [Documentazione](#documentazione)

---

## Panoramica

**sFlow Enricher** è un proxy sFlow v5 scritto in Go che riceve pacchetti sFlow da dispositivi di rete (es. Huawei NetEngine 8000), modifica il campo Source AS in base a regole configurabili, e inoltra i pacchetti arricchiti a multipli collector (es. Cloudflare Magic Transit, Noction Flow Analyzer).

### Caso d'uso principale

Il router Huawei NetEngine 8000 di Goline SA (AS202032) invia pacchetti sFlow con `SrcAS = 0` per il traffico che origina dalla propria rete. Questo proxy intercetta i pacchetti, identifica gli IP sorgente appartenenti alla rete Goline (185.54.80.0/22), e sostituisce `SrcAS = 0` con `SrcAS = 202032` prima di inoltrare ai collector.

---

## Problema Risolto

### Prima (senza sFlow Enricher)

```
NetEngine 8000                     Cloudflare / Noction
     │                                    │
     │  sFlow: SrcIP=185.54.80.30        │
     │         SrcAS=0  ← PROBLEMA!      │
     └───────────────────────────────────►│
                                          │
                      I collector vedono SrcAS=0
                      e non possono attribuire il
                      traffico all'AS corretto
```

### Dopo (con sFlow Enricher)

```
NetEngine 8000        sFlow Enricher           Cloudflare / Noction
     │                      │                         │
     │  SrcAS=0            │  SrcAS=202032           │
     └─────────────────────►│ (arricchito)           │
                            └────────────────────────►│
                                                      │
                            I collector vedono SrcAS=202032
                            e attribuiscono correttamente
                            il traffico a Goline SA
```

---

## Architettura

```
                                    ┌─────────────────────────────────────┐
                                    │         sFlow Enricher              │
                                    │         185.54.81.40:6343           │
                                    │                                     │
┌─────────────────┐                 │  ┌─────────────────────────────┐   │
│  NetEngine 8000 │  sFlow UDP      │  │     Packet Processing       │   │
│   185.54.80.2   │────────────────►│  │                             │   │
│                 │   port 6343     │  │  1. Ricevi pacchetto sFlow  │   │
│  Interfacce:    │                 │  │  2. Verifica whitelist      │   │
│  - GE0/1/0-5    │                 │  │  3. Estrai SrcIP dal flow   │   │
│  - 100GE0/1/54  │                 │  │  4. Match con regole        │   │
│                 │                 │  │  5. Modifica SrcAS se match │   │
│  Rate: 1:1000   │                 │  │  6. Inoltra a destinazioni  │   │
└─────────────────┘                 │  └──────────────┬──────────────┘   │
                                    │                 │                   │
                                    │  ┌──────────────┴──────────────┐   │
                                    │  │        HTTP API :8080       │   │
                                    │  │  /health  /status  /metrics │   │
                                    │  └─────────────────────────────┘   │
                                    └─────────────────┬───────────────────┘
                                                      │
                              ┌───────────────────────┼───────────────────────┐
                              │                       │                       │
                              ▼                       ▼                       ▼
                   ┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐
                   │    Cloudflare    │   │     Noction      │   │   (Ulteriori)    │
                   │  Magic Transit   │   │  Flow Analyzer   │   │                  │
                   │ 162.159.65.1:6343│   │208.122.196.72:6343│  │                  │
                   └──────────────────┘   └──────────────────┘   └──────────────────┘
```

---

## Funzionalità

### Core
| Funzionalità | Descrizione |
|--------------|-------------|
| **ASN Enrichment** | Modifica SrcAS basandosi su IP sorgente e regole configurabili |
| **Multi-Destination** | Inoltra simultaneamente a multipli collector |
| **sFlow v5** | Supporto completo per sFlow versione 5 (IPv4 e IPv6) |

### Operatività
| Funzionalità | Descrizione |
|--------------|-------------|
| **Hot-Reload** | Ricarica configurazione senza restart (SIGHUP) |
| **Health Checks** | Monitoraggio automatico disponibilità destinazioni |
| **Failover** | Reindirizzamento automatico a destinazione backup |
| **Whitelist** | Accetta sFlow solo da sorgenti autorizzate |

### Osservabilità
| Funzionalità | Descrizione |
|--------------|-------------|
| **Prometheus Metrics** | Endpoint `/metrics` per scraping |
| **HTTP Status API** | Endpoint `/status` con statistiche JSON |
| **Health Endpoint** | Endpoint `/health` per load balancer |
| **JSON Logging** | Log strutturati per ELK/Loki |
| **Telegram Alerts** | Notifiche su startup, shutdown, failures |

### Performance
| Funzionalità | Descrizione |
|--------------|-------------|
| **Buffer Pool** | Riutilizzo buffer per ridurre allocazioni |
| **Async I/O** | Goroutine dedicate per I/O non bloccante |
| **Socket Tuning** | Buffer socket ottimizzati (4MB read, 2MB write) |

---

## Requisiti

- **OS**: Linux (testato su Ubuntu 24.04 LTS)
- **Go**: 1.21+ (solo per compilazione)
- **Rete**: Porta UDP 6343 disponibile
- **RAM**: ~10-20 MB
- **CPU**: Minimo, gestisce facilmente 100k+ pkt/s

---

## Installazione

### Da Sorgenti

```bash
# Clona o copia il progetto
cd /root/sFlow_Enrichment_Integration_Huawei/sflow-enricher

# Compila
make build

# Installa (binario + config + systemd)
sudo make install

# Abilita e avvia
sudo systemctl enable --now sflow-enricher
```

### Verifica Installazione

```bash
# Controlla stato servizio
systemctl status sflow-enricher

# Controlla versione
sflow-enricher -version

# Controlla health
curl http://127.0.0.1:8080/health
```

### File Installati

| Percorso | Descrizione |
|----------|-------------|
| `/usr/local/bin/sflow-enricher` | Binario eseguibile |
| `/etc/sflow-enricher/config.yaml` | File di configurazione |
| `/etc/systemd/system/sflow-enricher.service` | Unit systemd |

---

## Configurazione

File di configurazione: `/etc/sflow-enricher/config.yaml`

### Esempio Minimo

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

### Esempio Completo (Goline SA)

```yaml
# sFlow Enricher Configuration v2.0
# Goline SA - AS202032

listen:
  address: "0.0.0.0"
  port: 6343

http:
  enabled: true
  address: "127.0.0.1"
  port: 8080

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

enrichment:
  rules:
    - name: "goline-ipv4"
      network: "185.54.80.0/22"
      match_as: 0
      set_as: 202032
      overwrite: false

    - name: "goline-ipv6"
      network: "2a02:4460::/32"
      match_as: 0
      set_as: 202032
      overwrite: false

security:
  whitelist_enabled: true
  whitelist_sources:
    - "185.54.80.2"

logging:
  level: "info"
  format: "text"
  stats_interval: 60

telegram:
  enabled: false
  bot_token: ""
  chat_id: ""
  alert_on:
    - "startup"
    - "shutdown"
    - "destination_down"
```

Per la documentazione completa della configurazione, vedi [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

---

## Utilizzo

### Comandi Servizio

```bash
# Avvia
sudo systemctl start sflow-enricher

# Ferma
sudo systemctl stop sflow-enricher

# Riavvia
sudo systemctl restart sflow-enricher

# Ricarica configurazione (hot-reload)
sudo systemctl reload sflow-enricher

# Stato
sudo systemctl status sflow-enricher

# Log in tempo reale
journalctl -u sflow-enricher -f
```

### Modalità Debug

```bash
# Ferma servizio
sudo systemctl stop sflow-enricher

# Avvia manualmente con debug
/usr/local/bin/sflow-enricher -config /etc/sflow-enricher/config.yaml -debug

# Output esempio:
# [DEBUG] Enriching packet map[new_as:202032 old_as:0 rule:goline-ipv4 src_ip:185.54.80.30]
```

### Hot-Reload Configurazione

Modifica il file di configurazione e ricarica senza interrompere il servizio:

```bash
# Modifica config
sudo nano /etc/sflow-enricher/config.yaml

# Ricarica (nessun pacchetto perso)
sudo systemctl reload sflow-enricher
# oppure
sudo kill -HUP $(pgrep sflow-enricher)
```

**Parametri ricaricabili senza restart:**
- Regole di enrichment
- Whitelist
- Configurazione Telegram
- Livello di log

---

## API HTTP

### Endpoints

| Endpoint | Metodo | Descrizione |
|----------|--------|-------------|
| `/health` | GET | Health check (OK/DEGRADED) |
| `/status` | GET | Statistiche JSON complete |
| `/metrics` | GET | Metriche formato Prometheus |

### GET /health

```bash
$ curl http://127.0.0.1:8080/health
OK
```

Codici risposta:
- `200 OK` - Tutte le destinazioni healthy
- `503 Service Unavailable` - Una o più destinazioni down

### GET /status

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
    "packets_filtered": 0,
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

### GET /metrics

```bash
$ curl -s http://127.0.0.1:8080/metrics
```

```
# HELP sflow_enricher_packets_received_total Total packets received
# TYPE sflow_enricher_packets_received_total counter
sflow_enricher_packets_received_total 125000

# HELP sflow_enricher_packets_enriched_total Total packets enriched
# TYPE sflow_enricher_packets_enriched_total counter
sflow_enricher_packets_enriched_total 85000

# HELP sflow_enricher_destination_healthy Destination health status
# TYPE sflow_enricher_destination_healthy gauge
sflow_enricher_destination_healthy{destination="cloudflare"} 1
sflow_enricher_destination_healthy{destination="noction"} 1
```

Per la documentazione completa dell'API, vedi [docs/API.md](docs/API.md).

---

## Monitoraggio

### Verifica Rapida

```bash
# Servizio attivo?
systemctl is-active sflow-enricher

# Health check
curl -s http://127.0.0.1:8080/health

# Statistiche
curl -s http://127.0.0.1:8080/status | jq '{received: .stats.packets_received, enriched: .stats.packets_enriched, dropped: .stats.packets_dropped}'

# Traffico sFlow
tcpdump -i any udp port 6343 -c 10 -n
```

### Prometheus

Aggiungi a `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'sflow-enricher'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: /metrics
    scrape_interval: 15s
```

### Alerting

Metriche chiave da monitorare:

| Metrica | Alert se | Descrizione |
|---------|----------|-------------|
| `sflow_enricher_packets_dropped_total` | > 0 | Problemi di forwarding |
| `sflow_enricher_destination_healthy` | = 0 | Destinazione down |
| `sflow_enricher_uptime_seconds` | reset improvviso | Servizio crashato |

---

## Documentazione

| Documento | Descrizione |
|-----------|-------------|
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md) | Riferimento completo configurazione |
| [docs/API.md](docs/API.md) | Documentazione API HTTP |
| [docs/OPERATIONS.md](docs/OPERATIONS.md) | Guida operativa e troubleshooting |

---

## Struttura Progetto

```
sflow-enricher/
├── README.md                      # Questo file
├── Makefile                       # Build, install, uninstall
├── config.yaml                    # Configurazione esempio
├── go.mod                         # Go module definition
├── go.sum                         # Go dependencies checksum
│
├── cmd/
│   └── sflow-enricher/
│       └── main.go                # Entry point applicazione
│
├── internal/
│   ├── config/
│   │   └── config.go              # Gestione configurazione
│   └── sflow/
│       └── sflow.go               # Parser/encoder sFlow v5
│
├── docs/
│   ├── CONFIGURATION.md           # Documentazione configurazione
│   ├── API.md                     # Documentazione API
│   └── OPERATIONS.md              # Guida operativa
│
├── systemd/
│   └── sflow-enricher.service     # Unit file systemd
│
└── build/
    └── sflow-enricher             # Binario compilato
```

---

## Informazioni Tecniche

### Protocollo sFlow v5

sFlow Enricher implementa il parsing e la modifica di pacchetti sFlow v5 secondo la specifica [sFlow Version 5](https://sflow.org/sflow_version_5.txt).

**Record supportati:**
- Flow Sample (enterprise=0, format=1)
- Expanded Flow Sample (enterprise=0, format=3)
- Raw Packet Header (enterprise=0, format=1)
- Extended Gateway (enterprise=0, format=1003) - dove viene modificato SrcAS

### Performance

Testato con:
- **Throughput**: 100k+ pacchetti/secondo
- **Latenza**: < 1ms di processing
- **Memoria**: ~10-20 MB stabile
- **CPU**: < 5% su singolo core

---

## Licenza

MIT License - Goline SA

---

## Versione

**2.0.0** - Gennaio 2026

### Changelog

- v2.0.0: Release completa con tutte le funzionalità
  - ASN enrichment per IPv4 e IPv6
  - Multi-destination forwarding
  - Hot-reload configurazione
  - Prometheus metrics
  - HTTP status API
  - Health checks con failover
  - Whitelist sorgenti
  - Telegram alerts
  - JSON logging
  - Buffer pool optimization
