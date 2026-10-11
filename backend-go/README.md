# Kano Navi Go read-only service

This module is the second migration slice. It runs beside the TypeScript
service, opens an existing Kano SQLite snapshot read-only, and exposes the
public health, revision, and dashboard reads:

```text
GET /api/health
GET /api/dashboard/revision
GET /api/dashboard?days=3
```

The dashboard returns the current public profile, summary, bounded posts,
events, videos, focus, timeline, resources, schedule assets, profile media,
media status, and synchronization/revision metadata. Its JSON projection is
kept separate from SQL and excludes raw JSON, source item IDs, job IDs,
provider data, credentials, database paths, and absolute paths.

From the repository root:

```text
go run ./backend-go/cmd/kano-go -database data/database/kano.sqlite
```

Go listens on `:7658`; the TypeScript service continues to own `:7657` and all
production routes. The database path is explicit. The service uses the
pure-Go `modernc.org/sqlite` driver with SQLite `mode=ro`, one connection, no
migrations, no seed, no writes, no external requests, and no media downloads.
It validates the tables and columns needed by the complete dashboard at
startup and returns stable `database_unavailable` responses for read failures.

The current module declares `go 1.26.4`, and the available Windows toolchain
was verified as `go1.26.4`. CI and Docker toolchain pinning are planned for the
next phase; this repository does not yet run Go in CI.

Run checks from this directory:

```text
gofmt -w (Get-ChildItem -Recurse -Filter *.go).FullName
go test ./...
go vet ./...
go build ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./...
```

Tests use temporary SQLite fixtures and synthetic records. They verify days
clamping, empty snapshots, public post/video/event projections, cancellation
and tombstone semantics, date and offset ordering, media readiness, sensitive
field exclusion, bounded reads, schema/query failures, and `Vary` plus cache
headers. They do not access the maintained database or external services.

The Go service has not taken over calendar, admin, MCP, synchronization,
source history, workflow scheduling, schedule extraction, LLM routing, media
download, or `/media` serving. Frontend and production route behavior remain
unchanged until a later cutover review.
