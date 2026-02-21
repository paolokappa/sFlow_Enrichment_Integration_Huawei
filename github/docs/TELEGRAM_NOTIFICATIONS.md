# Telegram Notifications - Complete Documentation

## Table of Contents
1. [Overview](#overview)
2. [Telegram Bot API](#telegram-bot-api)
3. [Configuration](#configuration)
4. [Alert Types](#alert-types)
5. [Message Format](#message-format)
6. [Go Implementation](#go-implementation)
7. [IPv6/IPv4 Fallback](#ipv6ipv4-fallback)
8. [Rate Limiting](#rate-limiting)
9. [Shutdown Handling](#shutdown-handling)
10. [Troubleshooting](#troubleshooting)

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
  drop_rate_threshold: 5.0
  http_timeout: 15
  flap_cooldown: 300
  ipv6_fallback: false
```

### Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `enabled` | bool | `false` | Enable/disable notifications |
| `bot_token` | string | `""` | Telegram bot token |
| `chat_id` | string | `""` | Chat/channel/group ID |
| `alert_on` | []string | `[]` | List of enabled alert types |
| `drop_rate_threshold` | float64 | `5.0` | Drop rate % to trigger `high_drop_rate` alert |
| `http_timeout` | int | `15` | HTTP timeout in seconds for API calls |
| `flap_cooldown` | int | `300` | Seconds between alerts for same destination |
| `ipv6_fallback` | bool | `false` | Try IPv6 first, fallback to IPv4 |

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
**Trigger**: Drop rate exceeds `drop_rate_threshold` (default 5.0%)

**Information included**:
- Current drop rate percentage
- Dropped packets in the interval
- Total packets received in the interval

The drop rate is calculated from deltas between stats intervals, not cumulative totals. This ensures alerts fire on current conditions, not historical data.

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
📍 *Host:* `myserver.example.com`
🏷️ *Event:* `startup`
💬 *Details:* Service started on `0.0.0.0:6343`
📦 Version: `2.0.0`
📋 Rules:
   • `my-network-ipv4` → AS64512
   • `my-network-ipv6` → AS64512
🎯 Destinations:
   • `primary-collector` (198.51.100.1:6343)
   • `secondary-collector` (198.51.100.2:6343)
🕐 *Time:* `23/01/2026 20:06:02`
```

### Example: Shutdown

```
🔴 *sFlow ASN Enricher*
━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `myserver.example.com`
🏷️ *Event:* `shutdown`
💬 *Details:* Service shutting down
⏱️ Uptime: `2h15m30s`
📥 Received: `15234`
✅ Enriched: `14890`
❌ Dropped: `12`
🎯 Destinations:
   ✅ `primary-collector`: 15234 pkts
   ✅ `secondary-collector`: 15234 pkts
🕐 *Time:* `23/01/2026 22:21:32`
```

### Example: Destination Down

```
🔻 *sFlow ASN Enricher*
━━━━━━━━━━━━━━━━━━━━━
📍 *Host:* `myserver.example.com`
🏷️ *Event:* `destination_down`
💬 *Details:* Destination `primary-collector` is *DOWN*
🔥 Error: `dial udp: connection refused`
📊 Status: `1/2` destinations healthy
🕐 *Time:* `23/01/2026 20:15:00`
```

---

## Go Implementation

### Config Structure

```go
type TelegramConfig struct {
    Enabled           bool     `yaml:"enabled"`
    BotToken          string   `yaml:"bot_token"`
    ChatID            string   `yaml:"chat_id"`
    AlertOn           []string `yaml:"alert_on"`
    DropRateThreshold float64  `yaml:"drop_rate_threshold"` // default 5.0
    HTTPTimeout       int      `yaml:"http_timeout"`        // default 15
    FlapCooldown      int      `yaml:"flap_cooldown"`       // default 300
    IPv6Fallback      bool     `yaml:"ipv6_fallback"`       // default false
}
```

### HTTP Client Initialization

The Telegram HTTP client is initialized at startup with configurable timeout and optional IPv6/IPv4 fallback:

```go
func initTelegramClient() {
    timeout := time.Duration(cfg.Telegram.HTTPTimeout) * time.Second
    if cfg.Telegram.IPv6Fallback {
        telegramClient = &http.Client{
            Timeout: timeout,
            Transport: &http.Transport{
                DialContext: ipv6FallbackDialer(),
            },
        }
    } else {
        telegramClient = &http.Client{Timeout: timeout}
    }
}
```

### Main Function

```go
func sendTelegramAlertWithWait(alertType, message string, blocking bool) {
    if !cfg.Telegram.Enabled { return }

    // Check if this alert type is enabled
    enabled := false
    for _, t := range cfg.Telegram.AlertOn {
        if t == alertType { enabled = true; break }
    }
    if !enabled { return }

    doSend := func() {
        hostname, _ := os.Hostname()

        icon := "ℹ️"
        switch alertType {
        case "startup":         icon = "🟢"
        case "shutdown":        icon = "🔴"
        case "destination_down": icon = "🔻"
        case "destination_up":  icon = "🔺"
        case "high_drop_rate":  icon = "📉"
        }

        fullMessage := fmt.Sprintf(
            "%s *sFlow ASN Enricher*\n"+
                "━━━━━━━━━━━━━━━━━━━━━\n"+
                "📍 *Host:* `%s`\n"+
                "🏷️ *Event:* `%s`\n"+
                "💬 *Details:* %s\n"+
                "🕐 *Time:* `%s`",
            icon, hostname, alertType, message,
            time.Now().Format("02/01/2006 15:04:05"))

        apiURL := fmt.Sprintf(
            "https://api.telegram.org/bot%s/sendMessage",
            cfg.Telegram.BotToken)

        payload := map[string]interface{}{
            "chat_id": cfg.Telegram.ChatID,
            "text": fullMessage, "parse_mode": "Markdown",
        }
        jsonPayload, err := json.Marshal(payload)
        if err != nil {
            logError("Failed to marshal Telegram payload", err, nil)
            return
        }

        ctx, cancel := context.WithTimeout(context.Background(),
            time.Duration(cfg.Telegram.HTTPTimeout)*time.Second)
        defer cancel()

        req, _ := http.NewRequestWithContext(ctx, "POST", apiURL,
            bytes.NewBuffer(jsonPayload))
        req.Header.Set("Content-Type", "application/json")

        resp, err := telegramClient.Do(req)
        if err != nil {
            logError("Failed to send Telegram alert", err, nil)
            return
        }
        defer resp.Body.Close()
    }

    if blocking { doSend() } else { go doSend() }
}
```

### Rate-Limited Alerts

For destination state changes, alerts are rate-limited to prevent notification flooding during flapping:

```go
func sendRateLimitedAlert(alertType, destName, message string) {
    alertCooldownsMu.Lock()
    key := alertType + ":" + destName
    if last, ok := alertCooldowns[key]; ok {
        cooldown := time.Duration(cfg.Telegram.FlapCooldown) * time.Second
        if time.Since(last) < cooldown {
            alertCooldownsMu.Unlock()
            return // Cooldown active, skip alert
        }
    }
    alertCooldowns[key] = time.Now()
    alertCooldownsMu.Unlock()

    sendTelegramAlert(alertType, message)
}
```

### Drop Rate Monitoring

Drop rate is checked every stats interval using counter deltas:

```go
func checkDropRate() {
    currentReceived := atomic.LoadUint64(&stats.PacketsReceived)
    currentDropped := atomic.LoadUint64(&stats.PacketsDropped)

    deltaReceived := currentReceived - prevReceived
    deltaDropped := currentDropped - prevDropped
    prevReceived = currentReceived
    prevDropped = currentDropped

    if deltaReceived > 0 {
        dropRate := float64(deltaDropped) / float64(deltaReceived) * 100
        if dropRate >= cfg.Telegram.DropRateThreshold {
            sendTelegramAlert("high_drop_rate", fmt.Sprintf(
                "Drop rate: %.1f%%\nDropped: %d / Received: %d (last interval)",
                dropRate, deltaDropped, deltaReceived))
        }
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

## IPv6/IPv4 Fallback

When `ipv6_fallback: true` is set, the Telegram client uses a custom dialer that:

1. Attempts connection via IPv6 (`tcp6`) with a 5-second connect timeout
2. If IPv6 fails, falls back to IPv4 (`tcp4`)
3. Sends a degradation alert (max 1 per hour) when fallback occurs
4. The degradation alert uses a separate IPv4-only HTTP client to avoid recursion

```go
func ipv6FallbackDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
    return func(ctx context.Context, network, addr string) (net.Conn, error) {
        // Try IPv6 first with 5s timeout
        d6 := net.Dialer{Timeout: 5 * time.Second}
        conn, err := d6.DialContext(ctx, "tcp6", addr)
        if err == nil {
            return conn, nil
        }
        // Fallback to IPv4
        d4 := net.Dialer{Timeout: 5 * time.Second}
        conn, err4 := d4.DialContext(ctx, "tcp4", addr)
        if err4 == nil {
            // Send degradation alert (max 1/hour, via IPv4-only client)
            go sendIPv6DegradationAlert(err)
            return conn, nil
        }
        return nil, err4
    }
}
```

The degradation alert is sent using a separate IPv4-only `http.Client` to prevent the fallback dialer from triggering recursion.

---

## Rate Limiting

Destination state change alerts (`destination_down`, `destination_up`) use per-destination cooldown to prevent notification storms during network instability.

**How it works:**
- Each alert is keyed by `alertType:destinationName`
- If the same key was alerted within `flap_cooldown` seconds, the alert is suppressed
- The cooldown timer resets with each sent alert

**Example with `flap_cooldown: 300`:**
```
T+0s:   primary-collector DOWN → alert sent ✓
T+10s:  primary-collector UP   → alert sent ✓
T+15s:  primary-collector DOWN → SUPPRESSED (only 5s since last primary-collector alert)
T+310s: primary-collector DOWN → alert sent ✓ (cooldown expired)
```

Different destinations have independent cooldowns:
```
T+0s:   primary-collector DOWN → alert sent ✓
T+5s:   secondary-collector DOWN    → alert sent ✓ (different destination)
```

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
**Date**: 21/02/2026

**Co-Authored-By**: Claude Opus 4.6 (Anthropic)
