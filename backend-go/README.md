# Kano Navi Go service (phase nine)

This module is the ninth migration slice. It runs beside the TypeScript
service, opens an existing Kano SQLite snapshot read-only, and exposes these
public reads:

```text
GET /api/health
GET /api/dashboard/revision
GET /api/dashboard?days=3
GET /api/calendar?from=YYYY-MM-DD&to=YYYY-MM-DD&page=1&pageSize=100
GET|HEAD /media/<opaque-id>
GET|HEAD /media/profile/<avatar|banner>
```

When explicitly configured with `APP_MODE` and `ADMIN_PASSWORD`, the same
process also exposes the independent in-memory admin session and read
projections:

```text
GET  /api/admin/session
POST /api/admin/login
POST /api/admin/logout
GET  /api/admin/posts/llm
GET  /api/admin/videos
GET  /api/admin/events
GET  /api/admin/sync/runs
GET  /api/admin/activity
GET  /api/admin/overview
```

Go listens on `:7658`; TypeScript continues to own `:7657` and all production
routes. The database path is explicit. The service uses the pure-Go
`modernc.org/sqlite` driver with SQLite `mode=ro`, one connection, no
migrations, no seed, no writes, no external requests and no media downloads.

The dashboard and calendar projections match TypeScript's public field set and
ordering. Calendar dates are inclusive Asia/Tokyo dates, limited to seven days;
pagination is bounded to 1..100 and schedule images use the same public asset
projection. Media reads validate opaque IDs, ready status, allowed image MIME,
content hash, version query, regular-file status and symlink containment. Cache
paths are relative to the derived `data/` root (with the legacy
`data/cache/media` namespace retained); selected profile files must remain in
`data/avatar/`, pass image magic-byte checks and match the active hash/MIME.
YouTube watch, short, live, shorts and share links use the same normalized
`stream_identity`; non-YouTube values use exact URL identity. Timeline and
resource reads are complete, while the documented post/event/video preview
bounds remain in place. Startup requires every column used by the projection,
including `posts.raw_json` for schedule source-text filtering.

Admin sessions are deliberately not shared with TypeScript during the
side-by-side phase. Go owns no admin mutation, source sync, workflow execution,
LLM provider call, or media download/upload/discovery/cleanup. A future admin
cutover must select one session owner and rehearse rollback to TypeScript on
`:7657`; the Go admin router is not connected to the frontend.

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

The tool compares both services for health, revision, dashboard, calendar,
ready media, selected profile media and unready media; only `health.now` and
`dashboard.meta.generatedAt` are normalized. It compares sanitized JSON or
bytes plus the relevant cache and validator headers. It also checks stable
sanitized responses for unknown paths and POST methods. CI runs this after
`make install`, Go setup and `make ci-backend`; a missing local `node_modules`
is an explicit local skip and cannot pass the CI job after the locked install.
Go emits only route, method, status, duration and response-byte fields as
structured stdout logs.

The ordered CI entry point is `make public-read-gate`. It runs the locked
installation, TypeScript contracts, dashboard parity, side-by-side HTTP, Go
checks, privacy scanning and documentation checks. With no local
`node_modules`, the two comparison scripts return explicit `SKIPPED` status 2
after the dependency-free Go smoke; this cannot pass the CI backend job after
its locked installation.

`make go-check` also builds the independent scratch image and runs
`scripts/go-docker-smoke.mjs`. That check mounts a synthetic SQLite snapshot
and synthetic media tree read-only, verifies health/dashboard/calendar/ready
media, and confirms no database sidecar, WAL, journal or directory is created.
The Go validation job then runs `make public-read-deployment-smoke`, which
starts the existing TypeScript image and the independent Go image against
copies of one synthetic snapshot, checks public reads and profile media, and
restarts both containers while checking snapshot and media-tree hashes. For
10,000-record resource measurements, run
`node scripts/public-read-benchmark.mjs --output <path outside the repository>`.

Read-only tests verify unchanged database hashes and directory entries,
absence of read-created WAL/journal files, and visibility of committed WAL
data. Missing, incomplete, closed and corrupt snapshots produce only stable
`database_unavailable` responses at the HTTP boundary.

## Ownership and rollback

The Go service has not taken over admin, MCP, synchronization, source history,
workflow scheduling, schedule extraction, LLM routing, media download,
discovery, upload or cleanup. TypeScript still owns production traffic on
`:7657`; calendar and media have not been connected to production routing. To
roll back a future side-by-side deployment, stop Go on `:7658` and continue
using TypeScript on `:7657`.

The phase-nine workflow package (`internal/workflow`) persists definitions,
runs, ordered steps, attempts and leases through the additive SQLite migration
in `internal/store/sqlite`. It uses only synthetic step execution. The scheduler
is opt-in and disabled unless its `Enabled` field is explicitly set. Admin
workflow reads are `GET /api/admin/workflows`, `/api/admin/workflows/:id`,
`/api/admin/runs`, and `/api/admin/runs/:id`; projections contain safe status,
capability, retry and timing fields only.


Phase eight keeps the service read-only by default. The isolated event mutation slice requires `APP_MODE=development` and `-enable-admin-writes` against an existing synthetic SQLite file. It uses the distinct `kano_go_canary_session` cookie and atomic event/source/revision transactions; production remains owned by TypeScript.
