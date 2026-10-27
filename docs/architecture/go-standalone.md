# Go backend

The Go binary in `backend-go/` is the backend. `kano-go serve` runs the public
API, the operator console, workflows and timers, schedule extraction, MCP and
the built frontend from one process, against the SQLite snapshot and `data/`
tree. Releases, the published image, local development and CI all run it; no
Node runtime is shipped. Node remains the frontend build toolchain.

The TypeScript backend of releases up to 0.1.3 was removed after the
migration; see [Rollback](#rollback) for returning to such a release.

## Commands

```text
kano-go serve        Serve the whole site (API, console, workflows, MCP, frontend)
kano-go seed         Create the database if missing and add missing static records
kano-go sync         Run one full synchronization and exit
kano-go migrate-llm  Seal OPENAI_API_KEY as the key of the openai-default provider
kano-go healthcheck  Probe the local service and exit
```

Every command reads `.env` from the working directory; variables already in
the environment win. `-database` overrides `data/database/kano.sqlite`, and
`serve` also accepts `-static` (default `dist`) and `-listen` (default `PORT`,
`7657`). `seed -overwrite` restores the shipped profile, timeline and resource
wording; it never touches source snapshots, events, media or providers.
`migrate-llm -force` replaces an already stored key.

From the repository root:

```bash
make build   # frontend into dist/
make start   # builds backend-go/bin/kano-go and runs `kano-go serve`
make seed    # or: make sync
```

The npm scripts `start`, `seed`, `sync` and `migrate:llm` run the same commands
through `scripts/run-app.mjs`. In the image, `docker compose run --rm kano sync`
(or `seed`, `migrate-llm`) runs a command against the mounted `/app/data`.

`kano-go sync` uses the same executor and SQLite single-flight reservation as
the service. If the service is already synchronizing, the command reports a
busy queue instead of starting a second run.

Running the binary with flags only (no command) keeps the read-only sidecar
described in [Go backend migration](go-migration.md). Only the read-only
container smoke in `make go-check` uses it.

## Development

`make dev` builds the backend, starts it on `127.0.0.1:KANO_API_PORT` (default
`7658`) and starts Vite on `PORT`. Vite serves the frontend with hot reload and
proxies `/api`, `/media` and `/mcp` to the backend with the browser's Host
header, so authentication, media and MCP stay on one origin. `make preview`
does the same with the built bundle. Restart after changing Go source.

## Startup order

1. `ADMIN_PASSWORD` (production default, at least 12 non-padding characters)
   and a non-blank `LLM_SECRETS_KEY` are validated before SQLite is opened.
2. A missing database is created from the embedded schema
   (`internal/store/sqlite/bootstrap.sql`) and public static seeds. Source
   snapshots, Featured data and providers start empty.
3. An existing snapshot from an earlier release is upgraded in one
   transaction: missing tables, columns and indexes are added from the embedded
   schema, followed by the documented backfills (schedule periods, post account
   handles, focus video IDs, provider capabilities and retries, the one-time
   route-target copy, default catalog models, and the merge of per-stage
   routes into the single model queue). A snapshot whose `events` table predates event sources is rebuilt
   in place: every row keeps its ID, gains its Japan calendar date and becomes
   its own source evidence, and events curated from X are locked. Nothing else
   is dropped or cleared. A database without the core tables is refused
   unchanged.
4. Missing static seed rows are inserted; existing rows and settings are kept.
5. The selected avatar and banner are reconciled with the files on disk
   (`active_sha256`, `active_mime_type`), bumping the dashboard revision only
   when something changed.
6. A new installation receives the two unscheduled starting workflows
   (`full-refresh`, `sources-only`) once; deleting them does not recreate them.
7. Interrupted workflow runs are recovered. Once per database, the timers of
   saved workflows that read a source move to the source schedules, and on
   every start a Bilibili interval below `BILIBILI_MIN_INTERVAL_MINUTES` is
   raised to it (see [Workflows](workflows.md#upgrade-from-workflow-timers)).
8. The timer starts unless `WORKFLOW_SCHEDULER_ENABLED=0`. It submits due
   saved workflows and due source schedules, earliest first.

## Route ownership

| Area                                           | Package                                                         |
| ---------------------------------------------- | --------------------------------------------------------------- |
| Public reads, media, health                    | `internal/httpapi`, `publicview`                                |
| Session, login, origin guard                   | `internal/adminauth`                                            |
| Admin route table, overview, events, lists     | `internal/adminapi` + `store/sqlite`                            |
| Runs, workflows, source fetch                  | `internal/adminapi`, `internal/jobs`, `internal/workflow`       |
| Historical source windows                      | `internal/source/history`, `internal/workflow/history_steps.go` |
| Extraction, providers, models, routes          | `internal/llmadmin`                                             |
| Profile media, schedule images, post reprocess | `internal/contentadmin`                                         |
| Schedule extraction step                       | `internal/schedule`                                             |
| `/mcp`                                         | `internal/mcpapi`                                               |
| Built frontend and SPA fallback                | `internal/httpapi/static.go`                                    |

`internal/adminapi` owns the single `/api/admin` route table described in the
[Admin API](admin-api.md). Feature packages implement `httpx.AdminModule` by
returning their routes; the router authenticates the session, applies the
origin guard and refuses writes to a read-only database before a module
handler runs.

## Jobs and workflows

The console's job list is a projection of durable workflow runs. A saved
workflow runs under its own ID. A full sync, an automatic scan and a
single-source fetch run from unsaved definitions with the reserved IDs
`@sync`, `@scan` and `@fetch`; they never appear among saved workflows. All of
them share the SQLite single-flight reservation, so a second start answers
`409` with `busy` and the active run.

Differences from releases up to 0.1.3 that operators can observe:

- Jobs survive a restart and are recovered; the list is no longer limited to
  the current process lifetime.
- Sync and scan jobs also report per-step progress.
- A failed X or YouTube step is recorded and the remaining steps still run; the
  job finishes as partial, or failed when no step succeeded.
- Steps disabled through `SKIP_X`, `SKIP_YOUTUBE`, `SKIP_MEDIA` or `SKIP_LLM`
  are recorded as skipped.

Each step's counters are merged into the job's `sync_runs.counts_json` row.

## Historical source windows

`POST /api/admin/runs` with `{"kind": "fetch", "source", "window"}` accepts
`recent`, `before`, `range` and `count` windows. The selected window is frozen on the run (`workflow_runs.
fetch_windows_json`), so a recovered or retried run never changes it.

`before` and `range` use the official APIs and need the environment-only
`X_API_BEARER_TOKEN` or `YOUTUBE_API_KEY`, except that X `range` windows page
the public FxTwitter timeline without the token, as X `count` windows always
do; a YouTube `count` window reads the recent step's uploads listing. `GET /api/admin/sources` reports `historyAvailable` (the credential) and
`windows` (the modes it can run now) per source, and a mode outside `windows`
is rejected with a validation message. With the credential, a `recent` window
is also served by the API (YouTube still reads the feed to refresh
reservations); without it, an X `recent` window pages the timeline back to its
start and a YouTube one reads the feed.
Source schedules, YouTube pushes and saved workflows carry no window: their
runs read X and YouTube by the source schedule's frozen sync (incremental
with a cap, or the newest `count`), whatever the credentials
([Workflows](workflows.md#source-schedules)). A full sync or scan (console,
MCP or `kano-go sync`) carries neither and stays incremental.

`internal/source/history` calls only `api.x.com/2/tweets/search/all` and
`www.googleapis.com/youtube/v3/search`, plus the FxTwitter timeline through the
X source client for timeline windows. Credentials travel as request headers,
redirects are rejected, responses are limited to 4 MiB, and failures are
reduced to controlled codes (`source_api_http_<status>`,
`source_api_invalid_response`, ...) before they reach run history. Each page
commits in one transaction with its continuation in `sync_state`
(`x-window`/`youtube-window`, `x-timeline-window` for timeline cursors, and
`x-backfill`/`youtube-backfill` for `before`),
so a repeated fetch resumes after the last committed page and the incremental
cursors are never touched. Checkpoint keys match the ones releases up to
0.1.3 stored. X accounts rotate across runs and share the
`X_MAX_STATUS_REQUESTS` budget; pages per account are bounded by
`SOURCE_HISTORY_MAX_PAGES`. The contract is described in
[Workflows](workflows.md#fetch-windows-and-historical-backfill).

## Transfer

Public JSON and build text are gzip-compressed with the path rules in
[Backend](backend.md#text-transfer-and-static-caching). The Go listener does
not produce Brotli or deflate; clients that advertise only those receive the
identity representation.

## Rollback

The last TypeScript release image (`yexca/kano-navi:0.1.3`) runs on the same
`data/` directory; its source is the `v0.1.3` tag. Stop the Go process first:
only one process may own a snapshot, since both write SQLite and both would
start timed workflows. That release ignores `source_schedules`: workflows
whose timers moved there stay untimed until an operator enables them again.

The additive `workflow_*` tables and columns are ignored by that release, and
provider secrets use the same encryption format in both directions. Saved
workflows keep their definitions; their last status may read as an unknown
value until the next run. A legacy `events` table that was rebuilt on upgrade
is already in the layout 0.1.3 expects.
