# Architecture

## System Shape

```text
Public X / YouTube pages          OpenAI Responses API
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
          +--> src/main.jsx  -- public dashboard
          +--> src/admin.jsx -- hidden schedule administration
          |
          +--> server/sync-jobs.js -- single-flight asynchronous jobs
```

Synchronization and page reads are separate paths. The browser reads the local
database through the API, and a manual refresh only requests the API again. This
keeps external request frequency under control and lets the page show the last
successful snapshot while a source is unavailable.

## Layers and Responsibilities

### Presentation: `src/`

`src/main.jsx` maps dashboard JSON to component state, calendar navigation, theme
switching, account-source labels, and accessible links. `src/admin.jsx` owns the
hidden `/admin` interface for schedule-extractor settings, Featured-video
selection, and schedule CRUD. `src/components/ui/` contains
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
management, paginated event queries, asynchronous job status, video listing,
and event CRUD endpoints. Mutating requests with a browser `Origin` are checked
against the local origin; production still relies on the HttpOnly admin session
and SameSite cookie.
`APP_MODE=development` bypasses authentication for local work. Production
requires a configured password and uses in-memory HttpOnly cookie sessions;
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
tombstone so a later extraction cannot recreate the same event.

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
- Schedule: retain YouTube reservations as unified `events`, first select X
  candidates with the keyword stage, then pass matching posts plus ready cached
  images to `server/schedule-extractor.js`. The visual stage supports multiple
  OpenAI-compatible Responses or Chat Completions providers in an ordered
  failover route. API keys are encrypted at rest with `LLM_SECRETS_KEY`.
- Media: register discovered URLs as `media_assets`/`media_links`, then download
  a bounded pending batch during the same synchronization command.
- If one source fails, record the error; only successfully obtained data is upserted and old data remains.

## Request and Failure Flow

1. The browser requests `/api/dashboard?days=3` after loading.
2. The API reads SQLite, calculates each event's `isUpcoming`, resolves ready
   media to `/media/<id>`, and returns the snapshot plus synchronization metadata.
3. The page shows the snapshot time. If the API is unavailable after a load, it keeps the known state and shows a retry affordance.
4. A maintainer or an authorized MCP client starts an asynchronous job. The
   single-flight job manager writes a `sync_runs` row with `job_id` and
   `triggered_by`; a failed source retains the previous snapshot.
5. Completion increments `dashboard_revision`. The public page polls the small
   revision endpoint and reloads its stored snapshot; page refresh never starts
   an external fetch.
6. A maintainer who opens `/admin` can change the model, provider route,
   schedule-candidate
   settings, Featured video, or curate events. Those changes go through the
   local API and lock affected events against automatic replacement.

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
