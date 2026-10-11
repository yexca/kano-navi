# Kano Navi Go read-only service

This module is the fourth migration slice. It runs beside the TypeScript
service, opens an existing Kano SQLite snapshot read-only, and exposes the
public health, revision and dashboard reads:

```text
GET /api/health
GET /api/dashboard/revision
GET /api/dashboard?days=3
```

Go listens on `:7658`; TypeScript continues to own `:7657` and all production
routes. The database path is explicit. The service uses the pure-Go
`modernc.org/sqlite` driver with SQLite `mode=ro`, one connection, no
migrations, no seed, no writes, no external requests and no media downloads.

The dashboard projection matches TypeScript's public field set and ordering.
YouTube watch, short, live, shorts and share links use the same normalized
`stream_identity`; non-YouTube values use exact URL identity. Timeline and
resource reads are complete, while the documented post/event/video preview
bounds remain in place. Startup requires every column used by the projection,
including `posts.raw_json` for schedule source-text filtering.

## Checks

From the repository root, run the complete Go gate:

```text
make go-check
```

It runs formatting, tests, vet, native and CGO-disabled builds, Linux and
Windows amd64 cross-builds, and an independent scratch Docker image build from
`backend-go/Dockerfile`. The module declares Go `1.26.4` and CI reads that
version from `backend-go/go.mod`.

The synthetic store test and benchmark use 10,000 posts, events and videos to
verify bounded dashboard payloads and accurate counts:

```text
go test ./backend-go/internal/store/sqlite -run '^$' -bench BenchmarkDashboardSynthetic10k -benchmem
```

`node scripts/dashboard-parity.mjs` compares a fixed synthetic fixture through
the TypeScript public projection and the Go `cmd/kano-parity` renderer. It
compares all keys and values, including nulls, booleans, dates and array
ordering. The runner starts Go from `backend-go` (or uses `GO`) so it works
from the repository root. Without `node_modules`, it runs the dependency-free
Go synthetic snapshot smoke and prints `SKIPPED`; it never installs packages.

For a local HTTP comparison over one disposable snapshot, run:

```text
make side-by-side-http
```

The tool compares both services for health, revision and dashboard requests;
only `health.now` and `dashboard.meta.generatedAt` are normalized. It also
checks stable sanitized responses for unknown paths and POST methods. Go emits
only route, method, status, duration and response-byte fields as structured
stdout logs.

Read-only tests verify unchanged database hashes and directory entries,
absence of read-created WAL/journal files, and visibility of committed WAL
data. Missing, incomplete, closed and corrupt snapshots produce only stable
`database_unavailable` responses at the HTTP boundary.

## Ownership and rollback

The Go service has not taken over calendar, admin, MCP, synchronization,
source history, workflow scheduling, schedule extraction, LLM routing, media
download or `/media` serving. No production route or frontend behavior changes
in this phase. To roll back a future side-by-side deployment, stop Go and
continue using the TypeScript service on `:7657`.
