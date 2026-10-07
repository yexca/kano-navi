# Architecture

## System Shape

```text
Public X / YouTube pages          OpenAI-compatible LLM APIs
          |                                ^
          v                                |
scripts/sync.mjs ---- media download / schedule extraction
          |
          v
data/database/kano.sqlite  -- snapshot, cursors, settings, and media metadata
          |
          +--> data/x/       -- ignored content-addressed X media files
          +--> data/youtube/ -- ignored content-addressed YouTube media files
          +--> data/avatar/  -- ignored selected profile media
          |
          v
server/index.js   -- dashboard, admin API, health, /media/<id>, and /mcp
          |
          +--> src/dashboard/ -- public dashboard
          +--> src/admin/ -- hidden operator console
          |
          +--> server/sync-jobs.js -- single-flight asynchronous jobs
          +--> server/workflow-scheduler.js -- timed saved workflows
```

Synchronization and page reads are separate paths. The browser reads the local
database through the API, and a manual refresh only requests the API again. This
keeps external request frequency under control and lets the page show the last
successful snapshot while a source is unavailable.

## Layers and Responsibilities

### Presentation: `src/`

`src/main.jsx` only routes between the public board, the static `/history`
page in `src/history/`, and `/admin`. History copy and image metadata are
static; `scripts/import-history-media.mjs` imports a local reference archive
into the existing media tables and guarded cache without network requests.
See [History and visual archive](history.md). The board
lives in `src/dashboard/`: `use-dashboard.js` owns the snapshot request and
revision polling, `format.js` owns Japan-time and event-status helpers,
`content.js` holds static public links, and `components/` renders the header,
hero and spotlight, weekly schedule, X feed, videos, and archive. Its styles are
in `src/dashboard/dashboard.css` on top of the shared tokens in `src/index.css`. The hidden `/admin` console
lives in `src/admin/`: `index.jsx` registers the views and renders the shell,
`use-admin-data.js` owns every admin request and job polling, `views/` holds
the workflow, schedule, detection-rule, LLM-provider, and content views, and
`admin.css` styles them with `adm-` prefixed selectors because it ships in the
same bundle as the board. `src/components/ui/` contains
basic UI primitives, while the CSS files contain layout and design tokens. The
presentation layer must not import `better-sqlite3` or call X, YouTube, OpenAI,
or a third-party proxy directly.

### API: `server/app.js` and `server/index.js`

`server/app.js` builds an Express app around an injected database, while
`server/index.js` opens the runtime database and listener. Express exposes the
health and dashboard read endpoints and serves `dist/` in production.
Responses should remain stable and sanitized; do not expose
`raw_json`, stack traces, credentials, or absolute local paths to the browser.
Ready runtime media is served only through the opaque-ID `/media/:id` route.

`server/admin-api.js` exposes session, model/schedule/video settings, provider
management, the model catalog (`/providers/:id/models`, `/models/discover`,
`/models/remove`), route targets (`/routes/:route`), saved workflows
(`/workflows`, `/workflows/:id/run`), paginated event queries, asynchronous
job status, video listing, and event CRUD endpoints. Mutating requests with a browser `Origin` are checked
against the local origin; production still relies on the HttpOnly admin session
and SameSite cookie.
The server defaults to `APP_MODE=production` and validates the admin password
before opening or seeding SQLite. An explicit `APP_MODE=development` bypasses
authentication for local work. Production uses in-memory HttpOnly cookie sessions;
restarting the process invalidates all sessions. The API exposes whether an
OpenAI key is configured, never its value.

### Persistence: `server/database.js`

This module creates the SQLite schema, provides seed/upsert/query functions, and
keeps the database at `data/database/kano.sqlite`. Raw source data may be retained in
internal `raw_json`, but API mapping removes it. Writes should use the existing
transaction and upsert patterns so a partial synchronization cannot erase known
records.

Automatic event writes have lower precedence than `manual_locked` records.
Manual edits and confirmations lock the row; manual deletion keeps a hidden
tombstone so a later extraction cannot recreate the same event. LLM cancellation
is a separate evidence overlay: it may mark an existing automatic or manual
event as `llm_suspected`, but it never deletes the row or changes its core
schedule fields. A human can clear that overlay or set `manual_confirmed` with
a reason.

LLM providers store an explicit `text`/`image` capability array in
`capabilities_json`; `vision_capable` remains a derived migration field for old
clients. The provider routes `schedule_board`, `schedule_message`, and legacy
`schedule_vision` each have their own priority order.

`server/media-cache.js` owns cache-root path validation, source URL identities,
content hashes, and atomic file writes. `server/media-downloader.js` performs
bounded downloads from the X and YouTube image hosts and promotes verified
files to ready cache rows.

### Synchronization: `scripts/sync.mjs` and `server/sync-jobs.js`

The synchronization layer handles timeouts, parsing, field normalization, and
`sync_runs` records:

- X: use Snowflake timestamps per configured account to limit each first run to
  seven days, then fetch unknown IDs with a bounded known-item refresh allowance;
  successful account snapshots are merged by publication time.
- YouTube: keep six RSS entries on the first run, then follow a durable cursor;
  active reservations are rechecked even when no new upload appears.
- Schedule: retain YouTube reservations as unified `events`, select board
  candidates with the keyword stage, and separately use a cheap heuristic to
  identify ordinary X messages that may contain a schedule. Merge and de-duplicate
  both candidate paths under one limit, then pass each post plus ready cached
  images to `server/schedule-extractor.js`. The extractor chooses the
  `schedule_board` or `schedule_message` route, filters providers by the
  original text/image modality, makes at most three total calls per provider,
  and then fails over by priority. API keys are encrypted at rest with
  `LLM_SECRETS_KEY`. Each stored post also has a processing state and can be
  queued for a forced reprocess from the admin API; queued posts bypass the
  normal keyword candidate heuristic on the next scan.
- Media: register discovered URLs as `media_assets`/`media_links`, then download
  a bounded pending batch during the same synchronization command.
- Workflows: `server/workflow-catalog.js` declares the modular steps (`x`,
  `youtube`, `media`, `schedule`) in execution order and `scripts/sync.mjs`
  maps each to a runner. `runSync({ steps })` runs a subset and reports
  per-step progress; `SKIP_*` flags remain a process-wide kill switch. A new
  step needs one catalog entry and one runner, and the console renders it
  automatically. `server/workflow-scheduler.js` checks saved workflows every
  30 seconds and starts a due one through the single-flight job manager; a
  workflow that finds the queue busy stays due for the next tick.
- If one source fails, record the error; only successfully obtained data is upserted and old data remains.

## Request and Failure Flow

1. The browser requests `/api/dashboard?days=3` after loading.
2. The API reads SQLite, calculates each event's `isUpcoming`, resolves ready
   media to `/media/<id>`, derives the `summary` block, and returns the snapshot
   plus public synchronization metadata (`server/public-view.js`).
3. The page shows the snapshot time. If the API is unavailable after a load, it keeps the known state and shows a retry affordance.
4. A maintainer or an authorized MCP client starts an asynchronous job. The
   single-flight job manager writes a `sync_runs` row with `job_id` and
   `triggered_by`; a failed source retains the previous snapshot.
5. Completion increments `dashboard_revision`. The public page polls the small
   revision endpoint and reloads its stored snapshot; page refresh never starts
   an external fetch.
6. A maintainer who opens `/admin` can change the model, the independent board
   and message stages, each provider route's priority order, capability flags,
   Featured video, or curate events. Those changes go through the local API and
   lock affected events against automatic replacement.

### MCP boundary: `/mcp`

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

## Extension Points

- For a new data type, define fields in the schema, seed data, query, and API mapping before adding the page block.
- For a new source, add an isolated adapter in the synchronization layer with a destination policy, timeout, and parsing-failure behavior. Do not scatter network requests across React components.
- For a new page block, reuse the existing `Card`, `Tabs`, `Tooltip`, and CSS tokens, and design explicit loading, empty, stale-snapshot, and error states.
- For media work, read `docs/media-cache.md` first. Keep source URLs in the
  database, cache bytes outside Git, and preserve old files when a fetch fails.
