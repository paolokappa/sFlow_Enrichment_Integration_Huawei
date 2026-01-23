# Systemd Integration - Documentazione Completa

## Indice
1. [Panoramica](#panoramica)
2. [Systemd Notify Protocol](#systemd-notify-protocol)
3. [Watchdog Mechanism](#watchdog-mechanism)
4. [Auto-Restart Configuration](#auto-restart-configuration)
5. [Service Unit File](#service-unit-file)
6. [Implementazione Go](#implementazione-go)
7. [Security Hardening](#security-hardening)
8. [Test e Verifica](#test-e-verifica)

---

## Panoramica

Il servizio sFlow ASN Enricher è configurato come **mission-critical** con:
- **Type=notify**: Il servizio notifica systemd quando è pronto
- **WatchdogSec=30**: Systemd verifica che il servizio sia attivo ogni 30 secondi
- **Restart=always**: Riavvio automatico in caso di crash
- **Nice=-10, CPUWeight=200**: Priorità elevata per scheduling CPU

---

## Systemd Notify Protocol

### Documentazione Ufficiale
- [systemd.service(5)](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
- [sd_notify(3)](https://www.freedesktop.org/software/systemd/man/sd_notify.html)

### Come Funziona

Il servizio comunica con systemd tramite un socket Unix (AF_UNIX, SOCK_DGRAM) il cui path è in `$NOTIFY_SOCKET`.

**Messaggi supportati**:

| Messaggio | Significato |
|-----------|-------------|
| `READY=1` | Servizio pronto ad accettare richieste |
| `STOPPING=1` | Servizio in fase di shutdown |
| `WATCHDOG=1` | Heartbeat - il servizio è ancora attivo |
| `STATUS=...` | Stato testuale per `systemctl status` |
| `ERRNO=...` | Codice di errore errno |
| `MAINPID=...` | PID del processo principale |

### Sequenza di Vita del Servizio

```
1. systemd avvia il processo
2. Processo inizializza (config, socket, destinations)
3. Processo invia READY=1
4. systemd considera il servizio "active (running)"
5. [Loop] Processo invia WATCHDOG=1 ogni WatchdogSec/2
6. Alla ricezione di SIGTERM:
   6a. Processo invia STOPPING=1
   6b. Processo completa cleanup
   6c. Processo termina
7. systemd rileva exit, decide se riavviare
```

---

## Watchdog Mechanism

### Scopo

Il watchdog garantisce che il servizio non sia in deadlock o hang. Se il servizio non invia `WATCHDOG=1` entro `WatchdogSec`, systemd lo considera "failed" e lo riavvia.

### Configurazione systemd

```ini
[Service]
Type=notify
WatchdogSec=30
```

### Calcolo dell'Intervallo

Dalla documentazione systemd:
> "It is recommended that this setting is used together with Type=notify. If WatchdogSec= is used without Type=notify, the service manager will not be notified of service readiness."

**Best Practice**: Inviare il watchdog a metà dell'intervallo configurato.

```
WatchdogSec=30 → Invia WATCHDOG=1 ogni 15 secondi
```

### Environment Variable

Quando systemd avvia il servizio con watchdog attivo, imposta:
```
WATCHDOG_USEC=30000000   # 30 secondi in microsecondi
```

Il servizio legge questa variabile per determinare l'intervallo.

---

## Auto-Restart Configuration

### Opzioni di Restart

| Opzione | Comportamento |
|---------|---------------|
| `no` | Mai riavviare |
| `on-success` | Solo se exit code = 0 |
| `on-failure` | Solo se exit code ≠ 0 o killed |
| `on-abnormal` | Solo se killed da signal o timeout |
| `on-abort` | Solo se killed da signal |
| `on-watchdog` | Solo se watchdog timeout |
| `always` | Sempre riavviare |

### Rate Limiting

```ini
[Unit]
StartLimitIntervalSec=300
StartLimitBurst=5
```

**Significato**: Massimo 5 avvii in 300 secondi (5 minuti). Se superato, il servizio entra in stato "failed" e non viene più riavviato automaticamente.

### RestartSec

```ini
[Service]
RestartSec=3
```

Attende 3 secondi prima di riavviare dopo un crash. Questo previene:
- CPU thrashing in caso di crash immediato
- Problemi di resource exhaustion
- Log flooding

---

## Service Unit File

### File Completo: `/etc/systemd/system/sflow-enricher.service`

```ini
[Unit]
Description=sFlow ASN Enricher - Mission Critical
Documentation=https://github.com/paolokappa/sFlow_Enrichment_Integration_Huawei
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=notify
ExecStart=/usr/local/bin/sflow-enricher -config /etc/sflow-enricher/config.yaml
ExecReload=/bin/kill -HUP $MAINPID

# Auto-restart configuration
Restart=always
RestartSec=3
WatchdogSec=30

# Shutdown handling
TimeoutStartSec=30
TimeoutStopSec=30
KillMode=mixed
KillSignal=SIGTERM

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadOnlyPaths=/etc/sflow-enricher

# Resource limits & priority
Nice=-10
LimitNOFILE=65535
MemoryMax=256M
CPUWeight=200

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=sflow-enricher

[Install]
WantedBy=multi-user.target
```

### Spiegazione Dettagliata

#### [Unit] Section

| Direttiva | Valore | Spiegazione |
|-----------|--------|-------------|
| `Description` | sFlow ASN Enricher - Mission Critical | Nome descrittivo |
| `Documentation` | URL GitHub | Link alla documentazione |
| `After` | network-online.target | Avvia dopo che la rete è attiva |
| `Wants` | network-online.target | Dipendenza soft sulla rete |
| `StartLimitIntervalSec` | 300 | Finestra temporale per rate limiting (5 min) |
| `StartLimitBurst` | 5 | Max avvii nella finestra |

#### [Service] Section - Lifecycle

| Direttiva | Valore | Spiegazione |
|-----------|--------|-------------|
| `Type` | notify | Usa sd_notify protocol |
| `ExecStart` | /usr/local/bin/sflow-enricher ... | Comando di avvio |
| `ExecReload` | /bin/kill -HUP $MAINPID | Comando per reload config |
| `Restart` | always | Riavvia sempre dopo exit |
| `RestartSec` | 3 | Attesa prima di riavviare |
| `WatchdogSec` | 30 | Timeout watchdog |

#### [Service] Section - Shutdown

| Direttiva | Valore | Spiegazione |
|-----------|--------|-------------|
| `TimeoutStartSec` | 30 | Max tempo per startup |
| `TimeoutStopSec` | 30 | Max tempo per shutdown |
| `KillMode` | mixed | SIGTERM al main, SIGKILL ai figli |
| `KillSignal` | SIGTERM | Segnale di terminazione graceful |

#### [Service] Section - Priority

| Direttiva | Valore | Spiegazione |
|-----------|--------|-------------|
| `Nice` | -10 | Priorità CPU elevata (-20 = max, 19 = min) |
| `LimitNOFILE` | 65535 | Max file descriptor aperti |
| `MemoryMax` | 256M | Limite memoria hard |
| `CPUWeight` | 200 | Peso CPU relativo (100 = default, 200 = doppio) |

---

## Implementazione Go

### Funzione sdNotify

```go
// sdNotify sends a notification to systemd via the notify socket
func sdNotify(state string) {
    socketPath := os.Getenv("NOTIFY_SOCKET")
    if socketPath == "" {
        return  // Non siamo sotto systemd
    }

    conn, err := net.Dial("unixgram", socketPath)
    if err != nil {
        return
    }
    defer conn.Close()

    conn.Write([]byte(state))
}
```

**Note**:
- `NOTIFY_SOCKET` è impostato solo se `Type=notify`
- Il socket è AF_UNIX di tipo SOCK_DGRAM (datagram)
- Path tipico: `/run/systemd/notify` o abstract socket

### Funzioni Wrapper

```go
// sdReady notifica systemd che il servizio è pronto
func sdReady() {
    sdNotify("READY=1")
    logInfo("Systemd notified: READY", nil)
}

// sdWatchdog invia il heartbeat
func sdWatchdog() {
    sdNotify("WATCHDOG=1")
}

// sdStopping notifica l'inizio dello shutdown
func sdStopping() {
    sdNotify("STOPPING=1")
}
```

### Watchdog Goroutine

```go
func startWatchdog() {
    // Leggi l'intervallo dall'environment
    watchdogUSec := os.Getenv("WATCHDOG_USEC")
    if watchdogUSec == "" {
        return  // Watchdog non configurato
    }

    usec, err := strconv.ParseInt(watchdogUSec, 10, 64)
    if err != nil || usec <= 0 {
        return
    }

    // Notifica a metà dell'intervallo (best practice)
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

### Sequence Completa nel main()

```go
func main() {
    // 1. Load config
    cfg, err = config.Load(configPath)

    // 2. Setup destinations
    setupDestinations()

    // 3. Setup listener
    listener, err := net.ListenUDP("udp", listenAddr)

    // 4. Start background services
    go startHTTPServer()
    go healthChecker()
    go statsReporter()

    // 5. NOTIFY READY - servizio operativo
    sdReady()
    startWatchdog()

    // 6. Send Telegram startup notification
    sendTelegramAlert("startup", ...)

    // 7. Signal handling
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

    // 8. Start packet processing
    go processPackets(listener, stopChan)

    // 9. Wait for signals
    for sig := range sigChan {
        switch sig {
        case syscall.SIGHUP:
            cfg.Reload(configPath)  // Hot reload
        case syscall.SIGINT, syscall.SIGTERM:
            sdStopping()  // NOTIFY STOPPING
            sendTelegramAlertWithWait("shutdown", ..., true)  // Blocking
            close(stopChan)
            listener.Close()
            wg.Wait()
            return  // Exit gracefully
        }
    }
}
```

---

## Security Hardening

### Direttive di Sicurezza

| Direttiva | Effetto |
|-----------|---------|
| `NoNewPrivileges=yes` | Il processo non può acquisire nuovi privilegi |
| `ProtectSystem=strict` | Filesystem montato read-only tranne /dev, /proc, /sys |
| `ProtectHome=yes` | /home, /root, /run/user inaccessibili |
| `PrivateTmp=yes` | /tmp privato isolato |
| `ReadOnlyPaths=/etc/sflow-enricher` | Config directory read-only |

### Perché Queste Scelte

1. **NoNewPrivileges**: Previene privilege escalation se il binario viene compromesso

2. **ProtectSystem=strict**: Il servizio non può modificare il sistema. Se deve scrivere log, usa journald.

3. **ProtectHome**: Il servizio non ha bisogno di accedere a home directory

4. **PrivateTmp**: Isola i file temporanei, previene symlink attacks

5. **ReadOnlyPaths**: La configurazione non deve essere modificata a runtime

---

## Test e Verifica

### 1. Verifica Startup

```bash
systemctl start sflow-enricher
systemctl status sflow-enricher
```

Output atteso:
```
● sflow-enricher.service - sFlow ASN Enricher - Mission Critical
     Loaded: loaded (/etc/systemd/system/sflow-enricher.service; enabled)
     Active: active (running) since ...
```

### 2. Test Watchdog

```bash
# Verifica che il watchdog sia attivo
journalctl -u sflow-enricher | grep -i watchdog
```

Output atteso:
```
[INFO] Watchdog started map[interval:15s]
```

### 3. Test Auto-Restart

```bash
# Simula crash con kill -9 (SIGKILL, non intercettabile)
kill -9 $(pgrep sflow-enricher)

# Verifica riavvio automatico
sleep 5
systemctl status sflow-enricher
```

Output atteso:
```
Active: active (running)
```

Con log:
```
Main process exited, code=killed, status=9/KILL
sflow-enricher.service: Scheduled restart job
Started sFlow ASN Enricher - Mission Critical
```

### 4. Test Graceful Shutdown

```bash
# Invia SIGTERM (shutdown graceful)
systemctl stop sflow-enricher

# Verifica che Telegram riceva la notifica
```

### 5. Test Rate Limiting

```bash
# Crash multipli rapidi
for i in {1..6}; do
    kill -9 $(pgrep sflow-enricher) 2>/dev/null
    sleep 1
done

# Dopo 5 crash in < 5 minuti, il servizio non viene più riavviato
systemctl status sflow-enricher
```

Output atteso dopo il 6° crash:
```
Active: failed (Result: start-limit-hit)
```

### 6. Reset dopo Rate Limit

```bash
# Resetta il contatore
systemctl reset-failed sflow-enricher

# Ora può ripartire
systemctl start sflow-enricher
```

---

## Comandi Utili

```bash
# Status dettagliato
systemctl status sflow-enricher -l

# Log in tempo reale
journalctl -u sflow-enricher -f

# Ultimi 100 log
journalctl -u sflow-enricher -n 100

# Reload configurazione (SIGHUP)
systemctl reload sflow-enricher

# Restart completo
systemctl restart sflow-enricher

# Verifica unit file
systemd-analyze verify /etc/systemd/system/sflow-enricher.service

# Mostra proprietà runtime
systemctl show sflow-enricher

# Mostra cgroup e risorse
systemctl status sflow-enricher --no-pager -l
```

---

## Riferimenti

1. [systemd.service(5) - Service unit configuration](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
2. [systemd.exec(5) - Execution environment](https://www.freedesktop.org/software/systemd/man/systemd.exec.html)
3. [sd_notify(3) - Notify service manager](https://www.freedesktop.org/software/systemd/man/sd_notify.html)
4. [systemd for Developers I](https://0pointer.de/blog/projects/socket-activation.html)
5. [systemd for Developers II](https://0pointer.de/blog/projects/socket-activation2.html)

---

## Autore

**Paolo Caparrelli** - GOLINE SA
**Email**: soc@goline.ch
**Data**: 23/01/2026

**Co-Authored-By**: Claude Opus 4.5 (Anthropic)
