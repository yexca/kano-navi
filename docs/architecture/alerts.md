# Operator Alerts

How the server tells the operator that it needs attention: the rule
catalog, incidents, the delivery outbox, channels and the heartbeat.
`backend/internal/alerts` owns all of it and its
[admin routes](admin-api/alerts.md); setup is in
[Alerts](../operations/alerts.md).

## Evaluation

The server process evaluates the rule catalog at start and then once a
minute. A pass reads local SQLite and in-process state only; it makes no
source request, does not use the single-flight queue, and keeps running when
`WORKFLOW_SCHEDULER_ENABLED=0`. A run stuck in the queue therefore cannot
delay the alert about it.

| State         | When                                      | Rules evaluated | Deliveries and heartbeat |
| ------------- | ----------------------------------------- | --------------- | ------------------------ |
| `on`          | production mode                           | yes             | sent                     |
| `development` | `APP_MODE=development`                    | yes             | never queued or sent     |
| `off`         | `ALERTS_ENABLED=0` or a read-only process | no              | no                       |

An operator's channel or heartbeat test sends in every state.

## Rule Catalog

The rules are fixed in code. The operator sets each rule's switch, its
parameter within bounds, its severity (`info`, `warning` or `critical`) and
whether its recovery is delivered.

| Rule                 | Subject                                  | Holds while                                                                                                      | Parameter         | Default      |
| -------------------- | ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ----------------- | ------------ |
| `run_failed_streak`  | source, or `live`                        | the latest N finished runs of an enabled source schedule, or of the live-status probe, are `failed` or `blocked` | runs, 1–20 (3)    | on, warning  |
| `source_stale`       | `x`, `youtube`, `bilibili`, `shop`       | an enabled schedule's source has had no successful read for N hours                                              | hours, 1–168 (24) | on, warning  |
| `login_expired`      | `bilibili`                               | the Bilibili login cookie is checked and expired                                                                 |                   | on, critical |
| `secrets_unreadable` | `llm`, `proxy`, `web_bot_auth`, `alerts` | a stored secret of that area no longer decrypts                                                                  |                   | on, critical |

- A streak counts runs finished in the last seven days, newest first, and
  stops at the first `succeeded` or `partial` run; `cancelled` runs do not
  count. A schedule the operator turned off is left out, and so is the
  live-status probe when it does not run in the process.
- Freshness is the source's `meta.sourceFetchedAt` (see
  [Failure and freshness](../operations/reliability.md#failure-and-freshness)).
  A source never read counts from its schedule's last change. A source
  disabled by `SKIP_*` is left out.
- `secrets_unreadable` checks enabled model providers, outbound proxies, the
  stored Web Bot Auth key, and the alert channels and heartbeat themselves.

## Incidents

An incident is one rule holding for one subject, keyed by rule and subject
(`alert_incidents`, one open row per key):

- A finding without an open incident opens one and queues an `opened`
  delivery to every channel in use with a readable secret.
- While it stays open, a pass refreshes its last-seen time, detail and
  severity (from the rule's current setting) and sends nothing.
- When an evaluated rule no longer finds it, the incident is resolved and a
  `resolved` delivery is queued if the rule sends recovery.
- Turning a rule off resolves its incidents without a delivery and drops
  their pending deliveries (`rule_off`). A rule whose
  check failed keeps its incidents until a later pass evaluates it.
- The operator can acknowledge an open incident (`acknowledged_at`), which
  takes it off the console's Alerts badge and changes no delivery. It stays
  acknowledged while it is open; the same problem returning after it
  resolved opens a new, unacknowledged incident.

An incident's detail holds counts, hours, times and fixed error codes only.

## Delivery Outbox

`alert_deliveries` is a durable outbox; pending rows survive a restart. Each
pass sends at most 20 due rows one at a time, after evaluation and the
heartbeat, for at most 45 seconds:

- A channel that answers `timeout` or `unreachable` gets no further attempt
  in the same pass; its other rows wait for the next one.
- A sent row is `sent`. A `timeout`, `unreachable`, `http_429` or `http_5xx`
  attempt is retried after 30 seconds, 2 minutes, 10 minutes and 1 hour; the
  fifth failure, or any other code, makes it `failed`.
- A row whose channel was removed or is not in use is `dropped`, as is a row
  still pending a day after it was queued (`expired`) or whose rule was
  turned off (`rule_off`).
- A row records only its status, attempt count and one fixed error code:
  `timeout`, `unreachable`, `rejected`, `http_3xx`, `http_4xx`, `http_429`,
  `http_5xx`, `secret_unreadable`, `failed`, `channel_removed`,
  `channel_off`, `expired` or `rule_off`. Response bodies, headers and addresses are
  never stored or logged.

Once a day the pass prunes on the
[run retention windows](workflows/run-execution.md): sent rows follow the
succeeded window; failed and dropped rows and resolved incidents follow the
failed window, and an incident stays while a kept delivery names it; `0`
keeps them.

## Messages

Every message is fixed English text built from the rule, the subject's
display name and the incident's detail, with the opening (and recovery) time
in Asia/Tokyo, for example "X runs are failing / The last 3 runs failed
(x_source_failed)." A channel with the console link adds
`<origin>/admin#<view>`, where the origin is `ADMIN_ORIGIN`, else
`PUBLIC_ORIGIN`; without either no link is added. Messages never carry raw
errors, paths, source item IDs, run IDs or secrets.

## Channels

| Type       | Request                                                                                   | Secret                                         |
| ---------- | ----------------------------------------------------------------------------------------- | ---------------------------------------------- |
| `ntfy`     | `POST {server}/` with a JSON publish: topic, title, message, priority, tags, click link   | optional access token, sent as a bearer header |
| `telegram` | `POST https://api.telegram.org/bot{token}/sendMessage` with chat id and plain text        | bot token (required)                           |
| `webhook`  | `POST {url}` with the JSON body below, `X-Kano-Event` and, with a key, `X-Kano-Signature` | optional signing key                           |

ntfy priorities are 3, 4 and 5 for `info`, `warning` and `critical`; the
default server is `https://ntfy.sh`. At most eight channels are configured.

```json
{
  "version": 1,
  "kind": "opened",
  "rule": "source_stale",
  "subject": "bilibili",
  "severity": "warning",
  "title": "Bilibili has not been read",
  "message": "No successful read for 26 hours.\nOpened 2026-01-02 09:00 JST",
  "openedAt": "2026-01-02T00:00:00.000Z",
  "sentAt": "2026-01-02T00:00:05.000Z",
  "link": "https://kano.example.invalid/admin#sources"
}
```

`kind` is `opened`, `resolved` or `test`; a test has no rule or subject.
`X-Kano-Signature` is `sha256=` and the lowercase hex HMAC-SHA256 of the raw
body under the signing key.

## Heartbeat

The heartbeat sends `GET` to an operator address, such as an Uptime Kuma push
monitor or healthchecks.io, every 1–60 minutes (default 5) while alerts are
`on`, so the monitor notices a stopped process or host, which in-process
alerts cannot report. A ping is due once the interval less half a pass
interval has passed, so a pass that starts slightly early still sends it. Its last answer and failure are kept in memory. An
external monitor may poll `GET /api/health` instead.

## Settings and Secrets

Channels, rule settings and the heartbeat are stored as JSON in
`app_settings` under `alerts`; a fresh database has no row, every rule at its
default, no channel and no heartbeat. Channel secrets, the whole webhook
address and the whole heartbeat address (both often carry a token in their
path) are sealed with `LLM_SECRETS_KEY` in the
[secret store format](../development/security.md#external-request-rules)
and never returned; the console sees only a webhook's host. A secret or address that no longer decrypts marks its channel or
the heartbeat unreadable: it is skipped, `secrets_unreadable` reports `alerts`,
and the sealed value is kept until the operator enters it again. A saved
secret is only sent to the address it was entered for: changing an ntfy
server or webhook address requires entering the secret again.

## Destinations

Deliveries and the heartbeat use the `notify`
[outbound proxy](outbound-proxy.md#areas) area. Every destination is a public
address:

- A saved address must be `https` with a public hostname or IP address and no
  user credentials; a webhook or ntfy address may not carry a token in its
  query (use the secret field). Loopback, private, link-local, shared
  (`100.64.0.0/10`, which includes Tailscale), documentation and reserved
  ranges, `localhost` and local-only names (`.local`, `.lan`, `.internal`,
  `.home.arpa`, `.localhost`, `.test`, `.invalid`) are refused.
- Before a request is sent, directly or through a proxy, its host is resolved
  and refused with `rejected` unless every address is public.
- A direct connection checks the address it actually dials, so a name that
  changes its answer cannot reach a private address. Direct connections of
  this area ignore `HTTP_PROXY` and `HTTPS_PROXY`; route them through the
  proxy pool instead.
- Requests time out after 10 seconds, read at most 64 KiB of the response and
  never follow a redirect.

## Related Docs

- [Alerts setup](../operations/alerts.md)
- [Alerts admin API](admin-api/alerts.md)
- [Secure development](../development/security.md#external-request-rules)
