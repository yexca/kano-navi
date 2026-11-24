# Alerts

Operator alert channels, rules, the heartbeat, open incidents and the
delivery history. Behavior is in [Operator alerts](../alerts.md).

| Method and path                             | Body                                         | Result                         |
| ------------------------------------------- | -------------------------------------------- | ------------------------------ |
| `GET /alerts`                               |                                              | alert settings (below)         |
| `PUT /alerts`                               | any subset of `{channels, rules, heartbeat}` | alert settings                 |
| `GET /alerts/deliveries`                    |                                              | `{deliveries}`, the latest 100 |
| `POST /alerts/channels/{id}/test`           |                                              | `{ok, error?, latencyMs}`      |
| `POST /alerts/heartbeat/test`               |                                              | `{ok, error?, latencyMs}`      |
| `POST /alerts/incidents/{id}/acknowledge`   |                                              | alert settings                 |
| `DELETE /alerts/incidents/{id}/acknowledge` |                                              | alert settings                 |

```json
{
  "state": "on",
  "evaluatedAt": "2026-01-02T00:00:00.000Z",
  "evaluationFailed": false,
  "hasOrigin": true,
  "channelTypes": ["ntfy", "telegram", "webhook"],
  "channels": [
    {"id", "name", "type", "enabled", "server", "topic", "chatId", "urlHost", "link", "hasSecret", "secretUnreadable"}
  ],
  "rules": [
    {"code", "param", "default", "min", "max", "severity", "enabled",
     "setting": {"enabled", "severity", "threshold", "notifyResolved"}}
  ],
  "heartbeat": {"enabled", "intervalMinutes", "hasUrl", "urlUnreadable", "lastOkAt", "lastFailureAt", "lastError"},
  "incidents": [
    {"id", "rule", "subject", "severity", "status", "detail", "openedAt", "lastSeenAt", "resolvedAt", "acknowledgedAt"}
  ]
}
```

- `state` is `on`, `development` or `off`; `incidents` lists the open ones,
  newest first. A rule's top-level `severity` and `enabled` are its defaults.
- `PUT` replaces the whole channel list when it carries `channels`, merges
  the rules it names into the stored ones, and replaces the heartbeat when it
  carries `heartbeat`. A channel is
  `{id?, name, type, enabled, server, topic, chatId, url?, link, secret?}`; a
  new channel gets an id. A webhook address is write-only: the response
  carries only its `urlHost`, and omitting `url` keeps the stored address. Omitting `secret` keeps the stored one, which is
  refused with `400` when the channel's ntfy server or webhook address
  changed; `""` clears it. The heartbeat is
  `{enabled, intervalMinutes, url?}`, where omitting `url` keeps the stored
  address. Invalid input answers `400 invalid_input` with a message; a
  missing `LLM_SECRETS_KEY` answers `503 secrets_key_unavailable`.
- A delivery is
  `{id, channelId, kind, status, attempts, lastError, createdAt, finishedAt, rule, subject, severity}`;
  a test has no rule, subject or severity.
- A test sends through a saved channel or the saved heartbeat address once,
  in any state and whether or not it is in use, and a channel test is
  recorded as a `test` delivery. A channel or heartbeat whose secret is
  unreadable answers `400`; an unknown channel `404 alert_channel_not_found`.
- `POST` acknowledges an open incident and keeps the first acknowledgement
  time; `DELETE` clears it. An id that names no open incident answers
  `404 alert_incident_not_found`.

`GET /overview` reports the number of open, unacknowledged incidents as
`alerts.open`.
