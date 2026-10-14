# Go backend migration

The migration is complete. [Go backend](go-standalone.md) describes the
current service: `kano-go serve` runs the public API, the complete operator
console, workflows and timers, historical source windows, schedule extraction,
MCP and the built frontend from one process, upgrades older snapshots, and
provides the `seed`, `sync` and `migrate-llm` commands. Releases, the
published image, development and CI run it; the TypeScript implementation is
kept as the rollback path and comparison reference.

The rest of this page is the record of the earlier phases.

Phase eleven is
[Go source workflows and media downloads](go-source-workflows.md): YouTube
recent synchronization, media downloads, asynchronous submission, workflow
CRUD, opt-in timers, cancellation and recovery. The sections below record the
earlier read-only sidecar, which is still what the binary is when it is run
with flags only and no command.

Phase nine adds the persistent workflow execution core described in
[Go workflow execution core](go-workflows.md). It remains side-by-side and
does not change the phase-seven public-read or TypeScript production ownership.

## Goal and boundary

Phase seven adds an independently authenticated, read-only admin sidecar
beside the public-read service. The Go service exposes the existing public
reads and these admin reads:

- `GET /api/health`
- `GET /api/dashboard/revision`
- `GET /api/dashboard?days=3`
- `GET /api/calendar?from=YYYY-MM-DD&to=YYYY-MM-DD&page=1&pageSize=100`
- `GET|HEAD /media/<opaque-id>`
- `GET|HEAD /media/profile/<avatar|banner>`
- `GET /api/admin/session`, `POST /api/admin/login`, `POST /api/admin/logout`
- `GET /api/admin/posts/llm`, `/videos`, `/events`, `/sync/runs`,
  `/activity` and `/overview`

The service remains read-only, makes no external requests, and is not connected
to the production `:7657` route. TypeScript continues to own production
traffic, the admin frontend, every admin mutation, MCP control, synchronization,
source history, workflow scheduling, schedule extraction, LLM routing, media
downloading and production routing. Admin reads are not connected to the
frontend in this phase.

## Admin authentication and ownership

Go uses `kano_admin_session`, an HttpOnly, `SameSite=Strict` in-memory cookie
with the TypeScript eight-hour lifetime and `/api/admin` path. Production
requires `ADMIN_PASSWORD` with at least twelve non-padding characters; only an
explicit `APP_MODE=development` enables password-free local authentication.
Failed logins use the stable `invalid_credentials` and `too_many_attempts`
codes. Cross-site unsafe requests are rejected with `csrf_origin_mismatch`.
Passwords, hashes, session tokens and internal session identifiers never enter
responses or logs.

During side-by-side operation Go and TypeScript have separate session stores.
They must not be joined through plaintext cookies, SQLite secrets or a shared
session database. Before a production admin cutover, choose one session owner,
run a cookie/session compatibility rehearsal, and keep the rollback path of
stopping Go and returning all admin requests to TypeScript. The current Go
admin router contains no POST/PUT/PATCH/DELETE data mutation.

Admin list reads are SQL-bound and page-bounded. Event responses retain manual
locks, tombstones, cancellation fields and provenance. Post LLM errors are
classified rather than returned verbatim, run diagnostics remove stacks,
paths, secrets and raw payloads, and no `raw_json` or provider material enters
the projections. All reads use SQLite read-only mode and do not create or
modify WAL, SHM, journal, snapshot or media files.

## Calendar ownership

Go uses inclusive Asia/Tokyo date-only windows. Ranges must be ordered and at
most seven days; page numbers are one-based and page sizes are clamped to
1..100. The bounded SQLite queries preserve TypeScript ordering, mixed-offset
timestamp sorting, date-only events, tombstones, manual locks, cancellation
fields, day counts, adjacent dates and approved schedule-image projections.
`items` and `events` use the same sanitized public event projection as the
dashboard. The route returns `invalid_calendar_range` for invalid windows and
never returns source item IDs, raw JSON, job IDs or operator fields.

## Media ownership and containment

Go serves only database rows marked `ready` whose opaque 64-character ID,
allowed image MIME, content hash and version query agree. Cache paths are
relative to the derived `data/` root (or the legacy `data/cache/media` root),
reject parent/absolute components, are resolved through symlinks, and must end
at a regular file contained by the cache root. Profile selections are further
restricted to `data/avatar/`, the 15 MiB image limit, supported magic bytes and
the selected active hash/MIME. Pending, missing, failed, unsafe, replaced or
hash-mismatched files return stable public error codes; they are never replaced
with a remote URL. GET/HEAD, range responses, ETag, Last-Modified,
Content-Length and versioned cache headers follow the TypeScript media routes.

## Dashboard parity fixes

Go registers the same deterministic `stream_identity` SQLite function as the
TypeScript service and implements the rules in
`src/lib/stream-identity.ts`: `youtu.be`, `youtube.com/watch?v=`, `/live/`,
and `/shorts/` links with query/share parameters map to a valid YouTube video
ID. Other hosts, invalid IDs and unparseable values retain exact `url:`
identity. The identity is used for video/event association, inherited stream
end times, confirmed cancellation, and live/upcoming eligibility. Tests cover
watch/short/live/shorts variants, share parameters, unrelated IDs, lookalike
hosts and ordinary exact URL matching.

`timeline` and `resources` are read without a row limit, matching the current
TypeScript dashboard. Posts, event candidate branches, videos, schedule
assets and non-schedule assets retain their documented product bounds. Post
previews remain capped at 100 while the SQL count covers the complete request
window. `posts.raw_json` is now a required startup column because schedule
asset source-text filtering reads it; an incomplete schema fails before any
dashboard request.

## Reproducible TypeScript/Go comparison

`node scripts/dashboard-parity.mjs` creates one disposable synthetic SQLite
fixture, renders the TypeScript public HTTP projection and renders the Go
projection with the same fixed timestamp, then compares every object key,
array item and scalar value. Object key order is ignored; missing, extra,
null, boolean, date and ordering differences fail the comparison. The Go
`cmd/kano-parity` utility is a local read-only renderer used by this script. It
runs from the `backend-go` module directory, so the runner works from the
repository root on Windows, Linux and CI; set `GO` to override the executable.
Errors include the command and exit status without printing the snapshot path.
The comparison does not contact X, YouTube, a CDN or a model provider.
The CI backend job runs this comparison after its locked npm installation;
the separate Go job runs the full cross-platform and Docker gate.

When `node_modules` is absent, the runner executes the dependency-free
`TestRenderReadsSyntheticSnapshot` Go smoke and exits with an explicit `SKIPPED`
marker. It does not install packages or make network requests, and the result
must not be reported as TypeScript parity success.

## Featured video compatibility

If the `featured_video_id` setting row is absent, Go uses `focus.video_id` only
when that video exists. If the row is present, a non-empty value is used only
when its video exists; an empty or stale value returns null and never falls
back to focus. Tests cover both states and ensure the selected video remains in
the bounded dashboard list.

## Side-by-side HTTP check

With npm dependencies installed, run `node scripts/side-by-side-http.mjs` or
`make side-by-side-http`. The script creates one disposable synthetic SQLite
snapshot and synthetic PNG files under the ignored `data/` tree, starts
TypeScript and Go on local ephemeral ports, and compares status, selected wire
headers (`Content-Type`, `Cache-Control`, `Vary`, `ETag`, `Content-Length` and
`Last-Modified`) and complete bodies for health, revision, dashboard, calendar,
ready media, selected profile media and an unready media row. Only
`health.now` and `dashboard.meta.generatedAt` are normalized. Unknown paths and
POST requests retain stable sanitized errors. The backend CI job runs this
check after `make install`, `setup-go` and `make ci-backend`; missing local npm
dependencies produce an explicit local `SKIPPED`, while CI has already run the
locked install and therefore fails on a real comparison failure.

## Read-only and WAL verification

The store opens an existing regular file using SQLite `mode=ro`, one
connection, and no migrations or pragmas that change state. Tests hash the
database before and after a dashboard read and verify that no directory,
`-wal`, `-shm` or `-journal` entry is created. A second fixture keeps a
committed WAL transaction open and confirms that Go reads its revision. Missing
files, missing tables/columns, corrupt bytes and closed stores return the
stable public `database_unavailable` error without SQL, paths, stacks or
driver text.

## Toolchain and build matrix

The module remains pinned to Go `1.26.4`, matching the repository's current
Windows toolchain and the CI `go-version-file` setting. `make go-check` runs:

```text
gofmt -d backend-go
go test ./...
go vet ./...
go build ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
```

It also builds the independent `backend-go/Dockerfile`, a scratch image that
contains only the Go service. That image is separate from the production
image and does not configure port `7657` or any production route.

## Performance and bounds

`TestDashboardSynthetic10kCollectionsStayBounded` and
`BenchmarkDashboardSynthetic10k` use 10,000 synthetic posts, events and
videos. The test confirms bounded payload collections and complete post/event
counts; the benchmark reports a repeatable local measurement without making a
CI failure threshold out of machine-specific latency. Run it with:

```text
go test ./backend-go/internal/store/sqlite -run '^$' -bench BenchmarkDashboardSynthetic10k -benchmem
```

The Windows amd64 validation host measured two benchmark iterations at
`521.7–551.9 ms/op`, `88.13 MB/op`, and `1,883,952–1,883,958 allocations/op`;
this is recorded for comparison only and is not a CI threshold.

## Observability and rollback

The Go process emits one JSON request record per request with only `route`,
`method`, `status`, `duration_ms` and `response_bytes`. Query strings, bodies,
database paths, source URLs, providers, credentials and raw errors never enter
stdout. Database failures remain the fixed public `database_unavailable`
response.

The independent `backend-go/Dockerfile` builds a scratch image with no shell,
Node or CGO. `scripts/go-docker-smoke.mjs` creates a synthetic snapshot and
PNG, mounts `data/` read-only, verifies health, dashboard, calendar and ready
media, and compares the mounted tree before and after requests. The check runs
from `make go-check` after the image build. For rollback, start TypeScript on
`:7657`, start Go on `:7658`, stop Go, and verify TypeScript still serves the
snapshot. Production traffic remains on TypeScript `:7657`.

## Phase-six gate and cutover

Rollback is to stop the Go process and continue serving the existing
TypeScript service on `:7657`; no database or schema rollback is needed. Go is
not receiving production traffic in this phase.

`make public-read-gate` is the ordered CI entry point. It installs the locked
dependencies, runs TypeScript contracts, dashboard parity, side-by-side HTTP,
Go checks, the sensitive scan and documentation checks. The separate Go CI job
adds the TypeScript/Go image restart rehearsal after building the independent
Go image. The backend job invokes this target after setup-node and setup-go;
because both comparison scripts return status 2 when dependencies are missing,
CI cannot turn a local `SKIPPED` into a passing comparison.

`docs/operations/go-public-read-cutover.md` records the production-like
snapshot, resource/latency, container startup, route ownership and rollback
steps. Admin, MCP, synchronization, workflows, LLM, media
download/discovery/upload/cleanup, and frontend behavior remain owned by
TypeScript. No schema, frontend request path or production route is changed by
this phase. Only a future, separately approved cutover may change the public
route owner.

## Phase ten: X recent vertical slice

Phase ten adds one development-only writable path beside the TypeScript owner.
`POST /api/admin/sources/x/fetch` accepts only
`{"window":{"mode":"recent","days":7}}` after Go admin login. Start Go
with `APP_MODE=development` and `-enable-x-sync` against a disposable database;
production startup rejects the flag.

The adapter reads public profile HTML from `x.com/<handle>` and detail JSON from
`api.vxtwitter.com/<handle>/status/<id>`, with fake transport injection, fixed
timeout/User-Agent, a 4 MiB response limit, redirect rejection, and typed
errors. A successful run normalizes posts and commits `posts`, pending
`media_assets`/`media_links`, schedule-board `assets`/`schedule_asset_reviews`,
and incremental `sync_state` in one transaction. Duplicate IDs are idempotent;
failed responses retain old posts and cursors, and discovered IDs remain in the
pending queue. Historical/window cursors are untouched. Pending media is never
downloaded, and schedule candidates remain pending/unreviewed without LLM work.

The Go scheduler, X before/range/archive search, YouTube, media downloads, LLM,
MCP, frontend integration and production routing remain
`capability_not_migrated`. Rollback is to disable the flag and continue using
TypeScript synchronization.
