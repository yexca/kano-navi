# Core Boundaries

This is an unofficial, read-mostly fan board. Original public sources remain
authoritative. Page views read a prepared local snapshot; synchronization owns
external reads.

## System Shape

```text
Public X / YouTube pages          OpenAI-compatible LLM APIs
          |                                ^
          v                                |
scripts/sync.ts ---- media download / schedule extraction
          |
          v
data/database/kano.sqlite  -- snapshot, cursors, settings, and media metadata
          |
          +--> data/x/       -- ignored content-addressed X media files
          +--> data/youtube/ -- ignored content-addressed YouTube media files
          +--> data/avatar/  -- ignored selected profile media
          |
          v
server/index.ts   -- dashboard, admin API, health, /media/<id>, and /mcp
          |
          +--> src/dashboard/ -- public dashboard
          +--> src/admin/ -- hidden operator console
          |
          +--> server/sync-jobs.ts -- single-flight asynchronous jobs
          +--> server/workflow-scheduler.ts -- timed saved workflows
```

Synchronization and page reads are separate paths. The browser reads the local
database through the API, and a manual refresh only requests the API again. This
keeps external request frequency under control and lets the page show the last
successful snapshot while a source is unavailable.

## Request and Failure Flow

1. The browser requests `/api/dashboard?days=3` after loading.
2. The API reads SQLite, calculates each event's `isUpcoming`, resolves ready
   media to `/media/<id>`, derives the `summary` block, and returns the snapshot
   plus public synchronization metadata (`server/public-view.ts`).
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

## Extension Points

- For a new data type, define fields in the schema, seed data, query, and API mapping before adding the page block.
- For a new source, add an isolated adapter in the synchronization layer with a destination policy, timeout, and parsing-failure behavior. Do not scatter network requests across React components.
- For a new page block, reuse the existing `Card`, `Tabs`, `Tooltip`, and CSS tokens, and design explicit loading, empty, stale-snapshot, and error states.
- For media work, read `docs/architecture/media-cache.md` first. Keep source URLs in the
  database, cache bytes outside Git, and preserve old files when a fetch fails.

## Related Contracts

- [Backend and HTTP](backend.md)
- [Frontend](frontend.md)
- [Data model and precedence](data-model.md)
- [Sources and extraction](sources.md)
- [Workflows and fetch windows](workflows.md)
- [Deployment security](../operations/security.md)
