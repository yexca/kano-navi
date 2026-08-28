# Architecture

## System Shape

```text
Public X / YouTube pages
          |
          v
scripts/sync.mjs  -- parse, normalize, register, and write
          |
          v
data/kano.sqlite  -- server-side snapshot and media metadata
          |
          +--> data/cache/media/ -- ignored content-addressed media files
          |
          v
server/index.js   -- /api/health, /api/dashboard, and /media/<id>
          |
          v
src/main.jsx      -- React dashboard and interactions
```

Synchronization and page reads are separate paths. The browser reads the local
database through the API, and a manual refresh only requests the API again. This
keeps external request frequency under control and lets the page show the last
successful snapshot while a source is unavailable.

## Layers and Responsibilities

### Presentation: `src/`

`src/main.jsx` maps dashboard JSON to component state, date filtering, theme
switching, and accessible links. `src/components/ui/` contains basic UI
primitives, while `src/index.css` contains layout and design tokens. The
presentation layer must not import `better-sqlite3` or call X, YouTube, or a
third-party proxy directly.

### API: `server/app.js` and `server/index.js`

`server/app.js` builds an Express app around an injected database, while
`server/index.js` opens the runtime database and listener. Express exposes the
health and dashboard read endpoints and serves `dist/` in production.
Responses should remain stable and sanitized; do not expose
`raw_json`, stack traces, credentials, or absolute local paths to the browser.
Ready runtime media is served only through the opaque-ID `/media/:id` route.

### Persistence: `server/database.js`

This module creates the SQLite schema, provides seed/upsert/query functions, and
keeps the database at `data/kano.sqlite`. Raw source data may be retained in
internal `raw_json`, but API mapping removes it. Writes should use the existing
transaction and upsert patterns so a partial synchronization cannot erase known
records.

`server/media-cache.js` owns cache-root path validation, source URL identities,
content hashes, and atomic file writes. It has no HTTP or SQLite side effects.

### Synchronization: `scripts/sync.mjs`

The synchronization layer handles timeouts, parsing, field normalization, and
`sync_runs` records:

- X: read public profile status IDs, then read public status JSON.
- YouTube: read channel RSS for videos, then inspect streams/video pages for scheduled times.
- Schedule: retain X schedule information and YouTube reservations as unified `events`.
- Media: register discovered remote URLs as pending `media_assets` rows and
  `media_links`; a downloader can later promote them to ready files.
- If one source fails, record the error; only successfully obtained data is upserted and old data remains.

## Request and Failure Flow

1. The browser requests `/api/dashboard?days=3` after loading.
2. The API reads SQLite, calculates each event's `isUpcoming`, resolves ready
   media to `/media/<id>`, and returns the snapshot plus synchronization metadata.
3. The page shows the snapshot time. If the API is unavailable after a load, it keeps the known state and shows a retry affordance.
4. A maintainer runs `npm run sync` on the server; the result is written to the database and `sync_runs`.
5. The next page read shows the new snapshot without rebuilding the frontend.

## Extension Points

- For a new data type, define fields in the schema, seed data, query, and API mapping before adding the page block.
- For a new source, add an isolated adapter in the synchronization layer with a destination policy, timeout, and parsing-failure behavior. Do not scatter network requests across React components.
- For a new page block, reuse the existing `Card`, `Tabs`, `Tooltip`, and CSS tokens, and design explicit loading, empty, stale-snapshot, and error states.
- For media work, read `docs/media-cache.md` first. Keep source URLs in the
  database, cache bytes outside Git, and preserve old files when a fetch fails.
