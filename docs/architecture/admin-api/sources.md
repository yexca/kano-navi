# Sources and Outbound Proxies

Source settings and outbound proxy routes.

## Sources

| Method and path                               | Body                                                                               | Result                         |
| --------------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------ |
| `GET /sources`                                |                                                                                    | per-source status (below)      |
| `PUT /sources/{source}/schedule`              | `{enabled, intervalMinutes, sync, chain}`                                          | `SourceSchedule`               |
| `POST /sources/{source}/schedule/start`       |                                                                                    | `SourceSchedule`               |
| `POST /sources/{source}/run`                  |                                                                                    | `202 {run}`; `409 busy`        |
| `GET /sources/youtube/push`                   |                                                                                    | push status                    |
| `PUT /sources/youtube/push`                   | `{enabled, callbackUrl}`                                                           | push status                    |
| `POST /sources/youtube/push/rotate-secret`    |                                                                                    | push status                    |
| `GET /sources/shop/web-bot-auth`              |                                                                                    | key status (below)             |
| `PUT /sources/shop/web-bot-auth`              | `{origin}`                                                                         | key status                     |
| `POST /sources/shop/web-bot-auth/key`         |                                                                                    | key status                     |
| `DELETE /sources/shop/web-bot-auth/key`       |                                                                                    | key status                     |
| `GET /sources/youtube/live-probe`             |                                                                                    | live probe status              |
| `PUT /sources/youtube/live-probe`             | `{enabled, intervalMinutes, bilibiliEnabled, bilibiliIntervalMinutes}`, any subset | live probe status              |
| `PUT /sources/youtube/live-probe/videos/{id}` | `{stopped}`                                                                        | live probe status              |
| `GET /sources/youtube/ytdlp`                  |                                                                                    | `{available, version}`         |
| `POST /sources/youtube/ytdlp/update`          |                                                                                    | updated `{available, version}` |
| `GET /sync-settings`                          |                                                                                    | `{settings, fields}`           |
| `PUT /sync-settings`                          | any subset of the setting fields                                                   | `{settings, fields}`           |
| `GET /sources/x/related`                      |                                                                                    | related accounts (below)       |
| `PUT /sources/x/related`                      | `{accounts: [{handle, keywords}]}`                                                 | related accounts               |

```json
{
  "schedules": [SourceSchedule],
  "x":       {"skipped", "methods": [FetchMethod], "accounts": [{"account", "oldest"}]},
  "youtube": {"skipped", "methods": [FetchMethod], "oldest", "push": PushStatus, "liveProbe": LiveProbeStatus},
  "bilibili": {"available", "skipped", "methods": [FetchMethod], "oldest", "account", "videos", "signedIn", "login", "loginCheckedAt", "minIntervalMinutes", "lastAttemptAt", "lastSuccessAt", "blockedUntil", "lastError",
               "dynamics": {"posts", "methods": [FetchMethod], "oldest", "lastAttemptAt", "lastSuccessAt", "lastError", "skipped"},
               "live": {"roomId", "liveStatus", "title", "liveTime", "checkedAt", "lastLiveAt", "blockedUntil", "lastError"}},
  "shop": {"skipped", "methods": [FetchMethod], "status": {"sales", "lastAttemptAt", "lastSuccessAt", "blockedUntil", "lastError"},
           "webBotAuth": WebBotAuthStatus}
}
```

`shop.status` counts the shop's visible sale entries and reports its read
bookkeeping: `blockedUntil` is when a 429 answer's `Retry-After` passes.

## Outbound proxies

| Method and path         | Body                                                              | Result                                                |
| ----------------------- | ----------------------------------------------------------------- | ----------------------------------------------------- |
| `GET /proxy`            |                                                                   | `{proxies, routes, directFallback, areas, testAreas}` |
| `PUT /proxy`            | any subset of `{proxies, routes, directFallback}`                 | the same settings                                     |
| `POST /proxy/{id}/test` | `{area?}` (`x`, `youtube` (default), `bilibili`, `shop`, `media`) | `{ok, status?, error?, latencyMs, proxy}`             |

`proxies` is the ordered queue; each proxy is `{id, name, scheme, host, port,
username, hasPassword, passwordUnreadable, enabled, health}`, where `health`
is `{lastSuccessAt, lastFailureAt, coolingUntil}` from this process. A save
replaces each field it carries: `proxies` replaces the whole queue (a proxy
without `id` is new), `routes` maps every area (`x`, `youtube`,
`bilibili`, `shop`, `media`, `llm`, `notify`) to `{enabled, proxyIds}`. In a proxy write,
an omitted `password` keeps the stored password of the proxy with the same
`id` while its scheme, host and port stay the same (a changed one is
`400 invalid_input` until a password is sent), `""` clears it, and a
username-less proxy has none. Passwords are
write-only. Selections drop unknown ids; an unknown area, a route turned on
without any proxy, or an invalid protocol, address or port is
`400 invalid_input`. Unparseable stored settings answer reads with
`503 proxy_settings_unavailable`; a save replaces them. Storing a password
without `LLM_SECRETS_KEY` is `503 secrets_key_unavailable`. A test reports a
failed attempt as `200 {ok: false, error: "timeout" | "failed"}`; an unknown
proxy is `404 proxy_not_found`. See [Outbound proxies](../outbound-proxy.md).
