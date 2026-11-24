# Alerts

Setting up operator alerts and the heartbeat in `/admin` → Alerts. How alerts
are evaluated and delivered is in [Operator alerts](../architecture/alerts.md).

Alerts send only in production mode. In development mode the page shows
**Development: not sending**: rules still open and resolve incidents, but no
delivery is queued, so a local database never notifies anyone. Set
`ALERTS_ENABLED=0` to turn evaluation off.

## Channels

Add one or more channels; every channel in use receives each opening and
recovery. **Send test** sends a test message through one channel.

- **ntfy**: a topic on `https://ntfy.sh` or on a public self-hosted server.
  Topics on ntfy.sh are readable by anyone who knows the name, so choose an
  unguessable topic or protect it with an access token. A channel's console
  link puts the site's `/admin` address in every message; turn it off for a
  topic others can read.
- **Telegram**: create a bot with BotFather, start a chat with it (or add it
  to a group or channel), and enter the bot token and the chat id, or an
  `@channel` name for a public channel.
- **Webhook**: any public `https` address that accepts the
  [JSON body](../architecture/alerts.md#channels). With a signing key, verify
  `X-Kano-Signature` before trusting a request. A relay such as Apprise or
  n8n forwards alerts to email or other services.

Destinations on a private network, this machine or a Tailscale address are
refused. When the server cannot reach a service directly, route the
**Alerts** area through a proxy in `/admin` → Proxies.

## Rules

Each rule has a switch, its runs or hours, a severity and whether recovery is
sent. The defaults alert after three failed runs in a row, after a source
has gone 24 hours without a successful read, when the Bilibili login cookie
expires, and when stored secrets cannot be decrypted (for example after
`LLM_SECRETS_KEY` changed).

## Open Alerts

The nav badge on Alerts counts open alerts nobody has acknowledged. The
check button on an open alert acknowledges it and the undo button reverses
that; an acknowledged alert stays listed, dimmed, until it resolves, and
sends its recovery as usual. If the problem returns later it opens a new
alert that counts again.

## Heartbeat

In-process alerts cannot report a stopped server. Create a push monitor in
Uptime Kuma or a check in healthchecks.io, enter its push address and turn on
**Send heartbeat**; the monitor alerts when the pings stop. The address must
be public. Alternatively, point an external monitor at `GET /api/health`.

## Related Docs

- [Configuration](configuration.md)
- [Reliability and freshness](reliability.md)
- [Troubleshooting](troubleshooting.md)
