# Go source workflows and media downloads

Phase eleven implements YouTube recent synchronization, source-media downloads,
workflow management and asynchronous execution in Go. These are development
capabilities. Production routing and the remaining services still belong to
TypeScript; default Go startup remains read-only.

## Enablement and API

Use an existing disposable current-schema snapshot, or add `-initialize` to
create and seed it entirely with Go. Initialization requires a non-empty
`LLM_SECRETS_KEY` and explicit `APP_MODE=development` before opening SQLite.
Enable only the desired flags:

- `-enable-x-sync`: X recent reads and snapshot writes.
- `-enable-youtube-sync`: RSS, stream discovery and watch details.
- `-enable-media-downloads`: pending X/YouTube image downloads.
- `-enable-scheduler`: source schedule timers in this API process.
- `-enable-admin-writes`: manual events, also alongside source flags.

`-initialize` compiles the canonical fresh SQL schema and public profile,
timeline and resources into the binary. Fresh source snapshots, Featured data
and provider/model/routes stay empty. Reseeding only inserts missing static
IDs and retains existing snapshots and operator settings. Unknown/legacy
schemas fail validation before any seed write; full legacy upgrades remain
with TypeScript. Initialization does not implicitly enable event/source writes.
`scripts/export-go-bootstrap.mjs` regenerates the embedded schema and seeds
from an in-memory database and public source constants, without reading `.env`
or operator data. Node is a development export tool, not a Go startup dependency.

Production rejects these flags before opening SQLite. Source configuration
reuses existing `.env.example` variables; there are no new credentials or
environment variables. Go does not load `.env` automatically. Do not run Go and
TypeScript writers against the same snapshot concurrently. The independent
scratch image includes a CA bundle for verified HTTPS and no Node or shell.

Admin authentication and origin protection apply to every mutation:

| Route                                   | Behavior                                     |
| --------------------------------------- | -------------------------------------------- |
| `POST /api/admin/sources/x/fetch`       | Recent X window, 1..365 days                 |
| `POST /api/admin/sources/youtube/fetch` | Recent YouTube window, 1..365 days           |
| `POST /api/admin/runs`                  | Run picked steps once (`kind: "workflow"`)   |
| `POST /api/admin/runs/:id/cancel`       | Cancel queued or active work                 |
| `POST /api/admin/runs/:id/retry`        | Retry eligible failed steps, retain attempts |
| `GET /api/admin/runs/:id`               | Read durable steps, attempts and status      |

Source bodies are `{ "window": { "mode": "recent", "days": 7 } }`. Duplicate
keys, trailing content, unknown fields and invalid types are rejected. History
modes remain unsupported. Source endpoints run only their source; pick
`media` in a one-off step run to download pending images. Saved workflows
are retired ([ADR-0003](../decisions/ADR-0003-per-source-sync-schedules.md)).

## Execution and storage

Submission reserves a queued run before returning `202`. Its bounded process
context outlives HTTP; repeated refreshes create new runs, while an active run
returns `409 workflow_busy`. Definitions order steps as `x`, `youtube`, `media`,
`schedule`. Source windows are frozen on each run independently of timers and
later edits. Retry retains completed steps and their original windows.

Heartbeats renew long batches. Source/media transactions check the same
owner/generation/expiry fence before committing. Cancellation stops active
network requests and records terminal steps/attempts. Startup and a separate
30-second recovery loop recover expired leases, including when a restart occurs
before the old lease expires. Opt-in timers share the queue. Completion updates
compatible `sync_runs` history and dashboard revision.

Schema version 2 adds `workflow_runs.source_lookback_json` and
`workflow_run_steps.adapter_capability` without rebuilding or clearing tables.
The constrained v1 capability and old attempts remain intact. Read-only Go can
read v1 snapshots; writable opening upgrades them atomically. Real adapters
project `x_recent`, `youtube_recent`, or `media_download`. Missing capabilities
finish as `blocked`, never as successful synthetic work.

## Sources and media

X passes requested days to the adapter, reloads pending IDs and prioritizes
unfinished work within the shared account budget. Media uses TypeScript's full
SHA-256 source-URL identity. The development source opener repairs phase-ten
short IDs and links transactionally with deferred FK checks, retaining cache
metadata, bytes and unique source URLs. Repeat repair is idempotent. Image
replacement invalidates automatic approval and increments the asset version
while preserving manual review decisions.

YouTube permits only fixed `www.youtube.com` RSS/channel/watch paths, validates
identities, rejects redirects, and enforces a ten-second timeout and 4 MiB body
limit. Ordinary video checks require a matching playable player; upcoming
streams need a valid start instant. Escaped player payloads are supported.
Failed detail IDs rotate behind untouched work in a bounded durable queue.
Successful ordinary checks finish; active reservations remain eligible. Failed
RSS reads write no replacement snapshot. Videos, reservation events, evidence,
media and cursors commit atomically. Refresh preserves manual locks, tombstones
and cancellation. Japan dates derive from actual instants. Recent RSS results
do not claim historical completeness or API backfill.

Media steps select at most 20 pending opaque-ID X/YouTube rows. Downloads allow
HTTPS subdomains of `twimg.com` and `ytimg.com`; userinfo, unexpected ports and
credential query parameters are rejected. Each of at most three redirects is
revalidated within a single ten-second deadline. Bodies are capped at 10 MiB;
image magic bytes and supported declared MIME must agree. Rooted filesystem
operations confine atomic content-addressed writes to `data/x/sha256/` or
`data/youtube/sha256/`. Metadata stores relative paths and safe error codes.
Failed redownloads retain ready snapshots. Versioned public reads never fetch
media. Retry backoff, dimension probing and resized copies are described in
the [media contract](media-cache.md); cached files are not garbage-collected.

## Validation and remaining migration

`make go-test` runs contracts and vet without Docker. `make go-check` adds
formatting, CGO-free tests/builds, Linux/Windows builds and the independent image.
Disposable container smoke checks read-only state, combined admin/source flags,
workflow CRUD, blocked unsupported steps and production rejection. Fake
transports exercise complete X → YouTube → media execution, API cancellation,
manual locks, rollback, leases, v1 upgrades, identity repair and response bounds.
No validation fetches live source, CDN or model data.

[Go standalone backend](go-standalone.md) has since added additive schema
upgrades, selected-profile startup repair, LLM configuration, schedule
extraction and image review, profile media, MCP, the complete operator console
and static frontend serving. Still required before TypeScript can be removed:

- Source API historical windows and atomic history checkpoints.
- The rebuilding migration for snapshots that predate event sources.
- Replacement of the published production image and the Vite development API.

Rollback is to stop the Go writer and return to TypeScript. Additive workflow
state and repaired media identities need no snapshot or cached-file deletion.
