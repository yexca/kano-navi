# Go backend migration: phase four

## Goal and boundary

Phase three makes the Go public dashboard projection semantically equivalent to
the TypeScript projection and adds a cutover validation gate. The Go service
still exposes only:

- `GET /api/health`
- `GET /api/dashboard/revision`
- `GET /api/dashboard?days=3`

The service remains read-only, makes no external requests, does not serve
`/media`, and is not connected to the production `:7657` route. TypeScript
continues to own production traffic and all write and operator capabilities:
admin, MCP control, synchronization, source history, workflow scheduling,
schedule extraction, LLM routing, media downloading and media serving.

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
snapshot, starts TypeScript and Go on local ephemeral ports, and compares
status, Content-Type, Cache-Control, Vary and complete JSON for health,
revision and dashboard requests with days `1`, `3`, `30`, `invalid` and `999`.
Only `health.now` and `dashboard.meta.generatedAt` are normalized because they
are generated per request. Unknown paths and POST requests are checked for
stable sanitized errors. The Go HTTP tests exercise the same matrix without npm
dependencies.

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

The independent `backend-go/Dockerfile` builds a scratch image. Mount a copied
synthetic snapshot read-only, verify `/api/health` and
`/api/dashboard/revision`, and confirm that no database sidecars or directories
appear. For rollback, start TypeScript on `:7657`, start Go on `:7658`, stop Go,
and verify TypeScript still serves the snapshot. Production traffic remains on
TypeScript `:7657`; calendar, admin, MCP, synchronization, workflows, LLM and
media are not migrated.

## Rollback and cutover gate

Rollback is to stop the Go process and continue serving the existing
TypeScript service on `:7657`; no database or schema rollback is needed. Go is
not receiving production traffic in this phase.

Before a future cutover, the remaining review must cover a production-like
side-by-side HTTP comparison over a maintained snapshot, resource and latency
observability, deployment-specific Docker startup, route ownership and
operator rollback rehearsal. Admin, MCP, synchronization, workflows, LLM,
media download/serving and frontend behavior are intentionally still owned by
TypeScript.
