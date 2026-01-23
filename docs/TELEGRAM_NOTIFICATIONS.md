# Telegram Notifications - Documentazione Completa

## Indice
1. [Panoramica](#panoramica)
2. [Telegram Bot API](#telegram-bot-api)
3. [Configurazione](#configurazione)
4. [Tipi di Alert](#tipi-di-alert)
5. [Formato Messaggi](#formato-messaggi)
6. [Implementazione Go](#implementazione-go)
7. [Gestione Shutdown](#gestione-shutdown)
8. [Troubleshooting](#troubleshooting)

---

## Panoramica

Il sistema di notifiche Telegram fornisce alerting in tempo reale per:
- Avvio/arresto del servizio
- Cambiamenti di stato delle destinazioni (up/down)
- Situazioni di alto drop rate

---

## Telegram Bot API

### Documentazione Ufficiale
- [Telegram Bot API](https://core.telegram.org/bots/api)
- [sendMessage Method](https://core.telegram.org/bots/api#sendmessage)

### Endpoint Utilizzato

```
POST https://api.telegram.org/bot{token}/sendMessage
```

### Parametri Request

| Parametro | Tipo | Obbligatorio | Descrizione |
|-----------|------|--------------|-------------|
| `chat_id` | Integer/String | Sì | ID chat o @username canale |
| `text` | String | Sì | Testo del messaggio |
| `parse_mode` | String | No | "Markdown" o "MarkdownV2" o "HTML" |
| `disable_notification` | Boolean | No | Invia silenziosamente |

### Response

```json
{
  "ok": true,
  "result": {
    "message_id": 123,
    "from": {"id": 123456, "is_bot": true, "first_name": "BotName"},
    "chat": {"id": -YOUR_CHAT_ID_HERE, "title": "Channel Name", "type": "channel"},
    "date": 1706040000,
    "text": "Message content"
  }
}
```

### Rate Limits

Dalla documentazione Telegram:
- Max 30 messaggi/secondo a chat diverse
- Max 1 messaggio/secondo alla stessa chat (burst ok)
- Max 20 messaggi/minuto allo stesso gruppo

Per il nostro uso (alerting occasionale), non ci sono problemi di rate limiting.

---

## Configurazione

### File: `/etc/sflow-enricher/config.yaml`

```yaml
# Telegram notifications
telegram:
  enabled: true
  bot_token: "YOUR_BOT_TOKEN_HERE"
  chat_id: "-YOUR_CHAT_ID_HERE"
  alert_on:
    - "startup"
    - "shutdown"
    - "destination_down"
    - "destination_up"
    - "high_drop_rate"
```

### Parametri

| Parametro | Tipo | Descrizione |
|-----------|------|-------------|
| `enabled` | bool | Abilita/disabilita notifiche |
| `bot_token` | string | Token del bot Telegram |
| `chat_id` | string | ID della chat/canale/gruppo |
| `alert_on` | []string | Lista dei tipi di alert abilitati |

### Chat ID

- **Utente singolo**: Numero positivo (es. `123456789`)
- **Gruppo**: Numero negativo (es. `-123456789`)
- **Supergroup/Canale**: Numero negativo con prefisso 100 (es. `-1001234567890`)

### Come Ottenere il Chat ID

1. Aggiungi il bot al gruppo/canale
2. Invia un messaggio nel gruppo
3. Visita: `https://api.telegram.org/bot{TOKEN}/getUpdates`
4. Cerca `"chat":{"id":...}` nella risposta JSON

---

## Tipi di Alert

### 1. startup
**Trigger**: Servizio avviato e pronto

**Informazioni incluse**:
- Indirizzo di ascolto
- Versione
- Lista regole di enrichment
- Lista destinazioni

### 2. shutdown
**Trigger**: Servizio in fase di arresto (SIGTERM/SIGINT)

**Informazioni incluse**:
- Uptime totale
- Pacchetti ricevuti
- Pacchetti enriched
- Pacchetti dropped
- Stato finale destinazioni

### 3. destination_down
**Trigger**: Health check fallito per una destinazione

**Informazioni incluse**:
- Nome destinazione
- Errore specifico
- Conteggio destinazioni healthy/totali

### 4. destination_up
**Trigger**: Destinazione tornata healthy dopo un down

**Informazioni incluse**:
- Nome destinazione
- Conteggio destinazioni healthy/totali

### 5. high_drop_rate
**Trigger**: Drop rate supera una soglia (non ancora implementato)

**Informazioni incluse**:
- Drop rate percentuale
- Pacchetti dropped/totali

---

## Formato Messaggi

### Struttura Standard

```
{ICON} *sFlow ASN Enricher*
━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `hostname`
🏷️ *Event:* `event_type`
💬 *Details:* {message specifico}
🕐 *Time:* `DD/MM/YYYY HH:MM:SS`
```

### Icone per Tipo

| Tipo | Icona | Significato |
|------|-------|-------------|
| startup | 🟢 | Servizio avviato |
| shutdown | 🔴 | Servizio in arresto |
| destination_down | 🔻 | Destinazione down |
| destination_up | 🔺 | Destinazione up |
| high_drop_rate | 📉 | Alto drop rate |
| default | ℹ️ | Informazione generica |

### Esempio: Startup

```
🟢 *sFlow ASN Enricher*
━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `matomo.goline.ch`
🏷️ *Event:* `startup`
💬 *Details:* Service started on `0.0.0.0:6343`
📦 Version: `2.0.0`
📋 Rules:
   • `goline-ipv4` → AS202032
   • `goline-ipv6` → AS202032
🎯 Destinations:
   • `cloudflare` (162.159.65.1:6343)
   • `noction` (208.122.196.72:6343)
🕐 *Time:* `23/01/2026 20:06:02`
```

### Esempio: Shutdown

```
🔴 *sFlow ASN Enricher*
━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `matomo.goline.ch`
🏷️ *Event:* `shutdown`
💬 *Details:* Service shutting down
⏱️ Uptime: `2h15m30s`
📥 Received: `15234`
✅ Enriched: `14890`
❌ Dropped: `12`
🎯 Destinations:
   ✅ `cloudflare`: 15234 pkts
   ✅ `noction`: 15234 pkts
🕐 *Time:* `23/01/2026 22:21:32`
```

### Esempio: Destination Down

```
🔻 *sFlow ASN Enricher*
━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `matomo.goline.ch`
🏷️ *Event:* `destination_down`
💬 *Details:* Destination `cloudflare` is *DOWN*
🔥 Error: `dial udp: connection refused`
📊 Status: `1/2` destinations healthy
🕐 *Time:* `23/01/2026 20:15:00`
```

---

## Implementazione Go

### Struttura Config

```go
type TelegramConfig struct {
    Enabled  bool     `yaml:"enabled"`
    BotToken string   `yaml:"bot_token"`
    ChatID   string   `yaml:"chat_id"`
    AlertOn  []string `yaml:"alert_on"`
}
```

### Funzione Principale

```go
func sendTelegramAlert(alertType, message string) {
    sendTelegramAlertWithWait(alertType, message, false)
}

func sendTelegramAlertWithWait(alertType, message string, blocking bool) {
    if !cfg.Telegram.Enabled {
        return
    }

    // Verifica se questo tipo di alert è abilitato
    enabled := false
    for _, t := range cfg.Telegram.AlertOn {
        if t == alertType {
            enabled = true
            break
        }
    }
    if !enabled {
        return
    }

    logInfo("Sending Telegram notification", map[string]interface{}{
        "type": alertType,
    })

    doSend := func() {
        hostname, _ := os.Hostname()

        // Icona basata sul tipo di alert
        icon := "ℹ️"
        switch alertType {
        case "startup":
            icon = "🟢"
        case "shutdown":
            icon = "🔴"
        case "destination_down":
            icon = "🔻"
        case "destination_up":
            icon = "🔺"
        case "high_drop_rate":
            icon = "📉"
        }

        // Formato messaggio con data europea DD/MM/YYYY
        fullMessage := fmt.Sprintf(
            "%s *sFlow ASN Enricher*\n"+
                "━━━━━━━━━━━━━━━━━━━━━\n"+
                "📍 *Host:* `%s`\n"+
                "🏷️ *Event:* `%s`\n"+
                "💬 *Details:* %s\n"+
                "🕐 *Time:* `%s`",
            icon, hostname, alertType, message,
            time.Now().Format("02/01/2006 15:04:05"))

        url := fmt.Sprintf(
            "https://api.telegram.org/bot%s/sendMessage",
            cfg.Telegram.BotToken)

        payload := map[string]interface{}{
            "chat_id":    cfg.Telegram.ChatID,
            "text":       fullMessage,
            "parse_mode": "Markdown",
        }

        jsonPayload, _ := json.Marshal(payload)

        resp, err := http.Post(url, "application/json",
            bytes.NewBuffer(jsonPayload))
        if err != nil {
            logError("Failed to send Telegram alert", err, nil)
            return
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
            logError("Telegram API error",
                fmt.Errorf("status code: %d", resp.StatusCode), nil)
        }
    }

    // Blocking vs async
    if blocking {
        doSend()
    } else {
        go doSend()
    }
}
```

### Markdown Escaping

Telegram Markdown richiede escape di alcuni caratteri:

| Carattere | Escape |
|-----------|--------|
| `_` | `\_` |
| `*` | `\*` |
| `` ` `` | `` \` `` |
| `[` | `\[` |

Nel nostro caso, usiamo backtick per codice inline che non richiede escape interno.

---

## Gestione Shutdown

### Il Problema

Durante lo shutdown (SIGTERM), il processo deve:
1. Inviare la notifica Telegram
2. Attendere che venga inviata
3. Solo poi terminare

Se la notifica è asincrona (goroutine), il processo potrebbe terminare prima che la HTTP request completi.

### La Soluzione: Blocking Mode

```go
case syscall.SIGINT, syscall.SIGTERM:
    sdStopping()
    logInfo("Received shutdown signal", map[string]interface{}{
        "signal": sig.String(),
    })

    // Invia notifica e ATTENDI che completi (blocking=true)
    sendTelegramAlertWithWait("shutdown", fmt.Sprintf(
        "Service shutting down\n"+
            "⏱️ Uptime: `%s`\n"+
            "📥 Received: `%d`\n"+
            "✅ Enriched: `%d`\n"+
            "❌ Dropped: `%d`\n"+
            "🎯 Destinations:%s",
        time.Since(stats.StartTime).Round(time.Second),
        atomic.LoadUint64(&stats.PacketsReceived),
        atomic.LoadUint64(&stats.PacketsEnriched),
        atomic.LoadUint64(&stats.PacketsDropped),
        destStats), true)  // <-- blocking=true

    // Solo ORA chiudiamo tutto
    close(stopChan)
    listener.Close()
    wg.Wait()
    return
```

### Timeout Considerations

`TimeoutStopSec=30` nella unit file garantisce che systemd attenda fino a 30 secondi prima di forzare SIGKILL. La HTTP request a Telegram normalmente completa in < 1 secondo.

---

## Troubleshooting

### Notifica Non Arriva

1. **Verifica token**:
   ```bash
   curl "https://api.telegram.org/bot{TOKEN}/getMe"
   ```
   Risposta OK: `{"ok":true,"result":{"id":...,"is_bot":true,...}}`

2. **Verifica chat_id**:
   ```bash
   curl "https://api.telegram.org/bot{TOKEN}/sendMessage" \
     -d "chat_id={CHAT_ID}" \
     -d "text=Test"
   ```

3. **Verifica che il bot sia nel gruppo/canale**:
   - Per i canali: il bot deve essere amministratore

4. **Controlla i log**:
   ```bash
   journalctl -u sflow-enricher | grep -i telegram
   ```

### Errori Comuni

| Errore | Causa | Soluzione |
|--------|-------|-----------|
| 401 Unauthorized | Token invalido | Verifica token |
| 400 Bad Request: chat not found | Chat ID errato | Verifica chat_id |
| 403 Forbidden: bot was kicked | Bot rimosso dal gruppo | Aggiungi di nuovo il bot |
| 429 Too Many Requests | Rate limit | Riduci frequenza messaggi |

### Test Manuale

```bash
# Test con curl
curl -X POST "https://api.telegram.org/botYOUR_BOT_TOKEN_HERE/sendMessage" \
  -H "Content-Type: application/json" \
  -d '{
    "chat_id": "-YOUR_CHAT_ID_HERE",
    "text": "🧪 *Test Message*\n━━━━━━━━━━━━━━━\nThis is a test from sflow-enricher",
    "parse_mode": "Markdown"
  }'
```

---

## Riferimenti

1. [Telegram Bot API - Official Documentation](https://core.telegram.org/bots/api)
2. [Telegram Bot API - sendMessage](https://core.telegram.org/bots/api#sendmessage)
3. [Telegram Formatting Options](https://core.telegram.org/bots/api#formatting-options)
4. [Telegram Bot Rate Limits](https://core.telegram.org/bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this)

---

## Autore

**Paolo Caparrelli** - GOLINE SA
**Email**: soc@goline.ch
**Data**: 23/01/2026

**Co-Authored-By**: Claude Opus 4.5 (Anthropic)
