# Backend and HTTP

## API: `server/app.ts` and `server/index.ts`

`server/app.ts` builds an Express app around an injected database, while
`server/index.ts` opens the runtime database and listener. Express exposes the
health and dashboard read endpoints and serves `dist/` in production.
Development and bundle preview mount this same Express app through
`server/local-api.ts` in Vite's HTTP server. `/api`, `/media`, and `/mcp` are
handled before the frontend middleware, keeping authentication, media access,
and MCP control on the same origin. All entry points default to port `7657`;
development and preview refuse to switch ports when it is occupied.
Responses should remain stable and sanitized; do not expose
`raw_json`, stack traces, credentials, or absolute local paths to the browser.
Ready runtime media is served only through the opaque-ID `/media/:id` route.

`server/admin-api.ts` exposes session, model/schedule/video settings, provider
management, the model catalog (`/providers/:id/models`, `/models/discover`,
`/models/remove`), route targets (`/routes/:route`), saved workflows
(`/workflows`, `/workflows/:id/run`), paginated event queries, asynchronous
job status, video listing, and event CRUD endpoints. Mutating requests reject
cross-site Fetch Metadata. Browser `Sec-Fetch-Site: same-origin` requests remain
valid when a reverse proxy rewrites Host or terminates HTTPS; other requests
with an `Origin` must match the observed local origin. Production also requires
the HttpOnly admin session and SameSite cookie.
The server defaults to `APP_MODE=production` and validates the admin password
and requires a non-blank `LLM_SECRETS_KEY` in every mode before opening or
seeding SQLite. An explicit `APP_MODE=development` bypasses
authentication for local work. Production uses in-memory HttpOnly cookie sessions;
restarting the process invalidates all sessions. The API exposes provider-key availability, never plaintext or ciphertext.

## MCP Boundary: `/mcp`

`/mcp` uses stateless Streamable HTTP. `dashboard_read`, `schedule_list`,
`posts_list`, and `health_read` are public read-only tools and do not require a
credential. Control tools are only registered for requests carrying
`Authorization: Bearer <MCP_CONTROL_TOKEN>`: they can advance a dashboard
revision, start or inspect an asynchronous sync, or run the automatic schedule
scan. The token is independent of `ADMIN_PASSWORD` and browser sessions.

The endpoint deliberately has no tool for manual event confirmation, editing,
deletion, bulk approval, profile-media selection/upload, SQL, file paths, DOM
commands, or arbitrary URL proxying. Human confirmation remains a human-only
workflow at `/admin`.

## Public Snapshot Contract

GET /api/health returns service status, the current ISO timestamp, and a
whitelisted synchronization summary; it does not return a database path or label. GET /api/dashboard?days=3
clamps days to 1 through 30 and returns profile, summary, posts, events, videos,
focus, timeline, resources, schedule assets, media status, and metadata.
The summary contains nextEvent, nextStream, latestVideo, latestPost, and counts;
latestPost survives an empty requested window. Metadata includes configured
X accounts and the Featured video ID.

server/public-view.ts owns shared public event, synchronization, and selected
profile-media projections for HTTP and MCP. A public sync summary contains only
id, source, start/finish times, status, and a fixed status message. Public profile
slots contain only id, slot, source, sourceUrl, status, isActive, and publicUrl.
Cache paths, source references, download errors, and candidate metadata remain
available through authenticated admin reads.
Per-source counters, raw fetch errors, job IDs, and internal source item IDs
stay in the guarded admin API. GET /api/dashboard/revision reports a small
revision hint without external work. Timestamps are parseable ISO 8601 strings;
display and date-window boundaries use Asia/Tokyo.

Media responses follow the [media HTTP contract](media-cache.md#http-contract).
Selected profile media uses /media/profile/avatar and /media/profile/banner.

## Related Docs

- [Data model](data-model.md)
- [Workflows](workflows.md)
- [Deployment security](../operations/security.md)
- [Secure development](../development/security.md)

## Bounded Public Calendar

`GET /api/calendar?from=2026-10-05&to=2026-10-11&page=1&pageSize=100`
reads an inclusive range of at most seven Japan dates, with 1–100 events per
page in stable chronological order. It returns sanitized `items` / `events`,
`total`, `totalPages`, `hasNext`, `dayCounts`, `revision`, matching approved
schedule images and `adjacent.previous` / `adjacent.next` event dates. Invalid
ranges return 400. Every read uses the same public event projection as the
board; it does not fetch platforms or models. MCP `schedule_list` remains the
bounded, paginated history reader; `dashboard_read` shares the bounded snapshot.

Dashboard SQL selects up to 100 recent post previews plus the latest-post
fallback, up to 100 current-week event previews plus 30 upcoming and 30 live
focus candidates and at most one linked event per returned video, and bounded
video candidates plus Featured selection and the latest completed video.
Known end times remove finished streams before limiting focus candidates and
retain streams with explicit longer durations. Media is resolved only for returned owners. Historical
event and media growth therefore does not enlarge a homepage response.
Full event totals and upcoming counts are separate SQL aggregates; calendar
pagination preserves access to every activity instead of dropping later rows.

JSON parser failures return `invalid_json` (400), excessive bodies return
`request_too_large` (413), and unsupported encodings return 415. The global
handler logs only a fixed event/code/status object, including unexpected errors;
it never serializes error messages, stacks, raw bodies, headers or keys.
