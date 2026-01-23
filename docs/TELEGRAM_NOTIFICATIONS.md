# Telegram Notifications - Complete Documentation

## Table of Contents
1. [Overview](#overview)
2. [Telegram Bot API](#telegram-bot-api)
3. [Configuration](#configuration)
4. [Alert Types](#alert-types)
5. [Message Format](#message-format)
6. [Go Implementation](#go-implementation)
7. [Shutdown Handling](#shutdown-handling)
8. [Troubleshooting](#troubleshooting)

---

## Overview

The Telegram notification system provides real-time alerting for:
- Service startup/shutdown
- Destination state changes (up/down)
- High drop rate situations

---

## Telegram Bot API

### Official Documentation
- [Telegram Bot API](https://core.telegram.org/bots/api)
- [sendMessage Method](https://core.telegram.org/bots/api#sendmessage)

### Endpoint Used

```
POST https://api.telegram.org/bot{token}/sendMessage
```

### Request Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `chat_id` | Integer/String | Yes | Chat ID or @username of channel |
| `text` | String | Yes | Message text |
| `parse_mode` | String | No | "Markdown" or "MarkdownV2" or "HTML" |
| `disable_notification` | Boolean | No | Send silently |

### Response

```json
{
  "ok": true,
  "result": {
    "message_id": 123,
    "from": {"id": 123456, "is_bot": true, "first_name": "BotName"},
    "chat": {"id": -1001234567890, "title": "Channel Name", "type": "channel"},
    "date": 1706040000,
    "text": "Message content"
  }
}
```

### Rate Limits

From Telegram documentation:
- Max 30 messages/second to different chats
- Max 1 message/second to the same chat (burst ok)
- Max 20 messages/minute to the same group

For our use case (occasional alerting), there are no rate limiting concerns.

---

## Configuration

### File: `/etc/sflow-enricher/config.yaml`

```yaml
# Telegram notifications
telegram:
  enabled: true
  bot_token: "YOUR_BOT_TOKEN_HERE"
  chat_id: "YOUR_CHAT_ID_HERE"
  alert_on:
    - "startup"
    - "shutdown"
    - "destination_down"
    - "destination_up"
    - "high_drop_rate"
```

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `enabled` | bool | Enable/disable notifications |
| `bot_token` | string | Telegram bot token |
| `chat_id` | string | Chat/channel/group ID |
| `alert_on` | []string | List of enabled alert types |

### Chat ID

- **Single user**: Positive number (e.g., `123456789`)
- **Group**: Negative number (e.g., `-123456789`)
- **Supergroup/Channel**: Negative number with 100 prefix (e.g., `-1001234567890`)

### How to Get the Chat ID

1. Add the bot to the group/channel
2. Send a message in the group
3. Visit: `https://api.telegram.org/bot{TOKEN}/getUpdates`
4. Look for `"chat":{"id":...}` in the JSON response

---

## Alert Types

### 1. startup
**Trigger**: Service started and ready

**Information included**:
- Listen address
- Version
- Enrichment rules list
- Destinations list

### 2. shutdown
**Trigger**: Service shutting down (SIGTERM/SIGINT)

**Information included**:
- Total uptime
- Packets received
- Packets enriched
- Packets dropped
- Final destination status

### 3. destination_down
**Trigger**: Health check failed for a destination

**Information included**:
- Destination name
- Specific error
- Healthy/total destinations count

### 4. destination_up
**Trigger**: Destination became healthy after being down

**Information included**:
- Destination name
- Healthy/total destinations count

### 5. high_drop_rate
**Trigger**: Drop rate exceeds threshold (not yet implemented)

**Information included**:
- Drop rate percentage
- Dropped/total packets

---

## Message Format

### Standard Structure

```
{ICON} *sFlow ASN Enricher*
━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `hostname`
🏷️ *Event:* `event_type`
💬 *Details:* {specific message}
🕐 *Time:* `DD/MM/YYYY HH:MM:SS`
```

### Icons by Type

| Type | Icon | Meaning |
|------|------|---------|
| startup | 🟢 | Service started |
| shutdown | 🔴 | Service stopping |
| destination_down | 🔻 | Destination down |
| destination_up | 🔺 | Destination up |
| high_drop_rate | 📉 | High drop rate |
| default | ℹ️ | Generic information |

### Example: Startup

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

### Example: Shutdown

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

### Example: Destination Down

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

## Go Implementation

### Config Structure

```go
type TelegramConfig struct {
    Enabled  bool     `yaml:"enabled"`
    BotToken string   `yaml:"bot_token"`
    ChatID   string   `yaml:"chat_id"`
    AlertOn  []string `yaml:"alert_on"`
}
```

### Main Function

```go
func sendTelegramAlert(alertType, message string) {
    sendTelegramAlertWithWait(alertType, message, false)
}

func sendTelegramAlertWithWait(alertType, message string, blocking bool) {
    if !cfg.Telegram.Enabled {
        return
    }

    // Check if this alert type is enabled
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

        // Icon based on alert type
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

        // Message format with European date DD/MM/YYYY
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

Telegram Markdown requires escaping certain characters:

| Character | Escape |
|-----------|--------|
| `_` | `\_` |
| `*` | `\*` |
| `` ` `` | `` \` `` |
| `[` | `\[` |

In our case, we use backticks for inline code which doesn't require internal escaping.

---

## Shutdown Handling

### The Problem

During shutdown (SIGTERM), the process must:
1. Send the Telegram notification
2. Wait for it to be sent
3. Only then terminate

If the notification is asynchronous (goroutine), the process might terminate before the HTTP request completes.

### The Solution: Blocking Mode

```go
case syscall.SIGINT, syscall.SIGTERM:
    sdStopping()
    logInfo("Received shutdown signal", map[string]interface{}{
        "signal": sig.String(),
    })

    // Send notification and WAIT for completion (blocking=true)
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

    // Only NOW close everything
    close(stopChan)
    listener.Close()
    wg.Wait()
    return
```

### Timeout Considerations

`TimeoutStopSec=30` in the unit file ensures systemd waits up to 30 seconds before forcing SIGKILL. The HTTP request to Telegram normally completes in < 1 second.

---

## Troubleshooting

### Notification Not Arriving

1. **Verify token**:
   ```bash
   curl "https://api.telegram.org/bot{TOKEN}/getMe"
   ```
   OK response: `{"ok":true,"result":{"id":...,"is_bot":true,...}}`

2. **Verify chat_id**:
   ```bash
   curl "https://api.telegram.org/bot{TOKEN}/sendMessage" \
     -d "chat_id={CHAT_ID}" \
     -d "text=Test"
   ```

3. **Verify bot is in the group/channel**:
   - For channels: the bot must be an administrator

4. **Check logs**:
   ```bash
   journalctl -u sflow-enricher | grep -i telegram
   ```

### Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| 401 Unauthorized | Invalid token | Verify token |
| 400 Bad Request: chat not found | Wrong Chat ID | Verify chat_id |
| 403 Forbidden: bot was kicked | Bot removed from group | Add the bot again |
| 429 Too Many Requests | Rate limit | Reduce message frequency |

### Manual Test

```bash
# Test with curl
curl -X POST "https://api.telegram.org/bot{YOUR_TOKEN}/sendMessage" \
  -H "Content-Type: application/json" \
  -d '{
    "chat_id": "{YOUR_CHAT_ID}",
    "text": "🧪 *Test Message*\n━━━━━━━━━━━━━━━\nThis is a test from sflow-enricher",
    "parse_mode": "Markdown"
  }'
```

---

## References

1. [Telegram Bot API - Official Documentation](https://core.telegram.org/bots/api)
2. [Telegram Bot API - sendMessage](https://core.telegram.org/bots/api#sendmessage)
3. [Telegram Formatting Options](https://core.telegram.org/bots/api#formatting-options)
4. [Telegram Bot Rate Limits](https://core.telegram.org/bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this)

---

## Author

**Paolo Caparrelli** - GOLINE SA
**Email**: soc@goline.ch
**Date**: 23/01/2026

**Co-Authored-By**: Claude Opus 4.5 (Anthropic)
