# Go backend migration: phase five

## Goal and boundary

Phase five adds the public calendar and read-only media projections beside the
TypeScript service and makes the HTTP comparison a required CI gate. The Go
service exposes:

- `GET /api/health`
- `GET /api/dashboard/revision`
- `GET /api/dashboard?days=3`
- `GET /api/calendar?from=YYYY-MM-DD&to=YYYY-MM-DD&page=1&pageSize=100`
- `GET|HEAD /media/<opaque-id>`
- `GET|HEAD /media/profile/<avatar|banner>`

The service remains read-only, makes no external requests, and is not connected
to the production `:7657` route. TypeScript continues to own production
traffic and all write/operator capabilities: admin, MCP control,
synchronization, source history, workflow scheduling, schedule extraction, LLM
routing, media downloading and production routing. Calendar and media have not
been cut over.

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

The Windows amd64 validation host measured two benchmark iterations at about
`533 ms/op`, `88 MB/op`, and `1.88 million allocations/op`; this is recorded
for comparison only and is not a CI threshold.

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

## Rollback and cutover gate

Rollback is to stop the Go process and continue serving the existing
TypeScript service on `:7657`; no database or schema rollback is needed. Go is
not receiving production traffic in this phase.

Before a future cutover, the remaining review must cover a production-like
side-by-side HTTP comparison over a maintained snapshot, resource and latency
observability, deployment-specific Docker startup, route ownership and
operator rollback rehearsal. Admin, MCP, synchronization, workflows, LLM,
media download/discovery/upload/cleanup, and frontend behavior remain owned by
TypeScript. No schema, frontend request path or production route changed in
this phase.
