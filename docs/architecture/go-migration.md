# Go backend migration: phase three

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
`cmd/kano-parity` utility is a local read-only renderer used by this script.
The comparison does not contact X, YouTube, a CDN or a model provider.
The CI backend job runs this comparison after its locked npm installation;
the separate Go job runs the full cross-platform and Docker gate.

This checkout has no `node_modules` directory, so the live TypeScript
comparison was not run here; the script reports that dependency state rather
than installing packages or making network requests. Go fixtures and tests
remain runnable without npm dependencies.

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
