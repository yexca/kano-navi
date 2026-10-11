# Go backend migration: phase one

## Goal and boundary

Phase one establishes an independently buildable Go service without changing
the production TypeScript backend. The service validates Go HTTP, tests,
configuration, graceful shutdown, and read-only SQLite access with the smallest
public API slice:

- `GET /api/health`
- `GET /api/dashboard/revision`

Admin API, MCP control, X and YouTube synchronization, media downloads,
workflow execution, schedule extraction, LLM routing, frontend requests, and
production routing remain owned by the TypeScript service. The root Dockerfile,
Compose service, and existing `7657` listener are unchanged.

## Running alongside TypeScript

The Go module lives under `backend-go/` and has its own `go.mod`. It defaults to
`:7658`, so it can be started beside the TypeScript listener on `:7657` while
both point at the same snapshot. Start it from the repository root with:

```text
go run ./backend-go/cmd/kano-go -database data/database/kano.sqlite
```

The path is passed explicitly with `-database`; no new shared environment
variable was added. A deployment can pass an absolute path or mount the same
read-only snapshot into a separate container. The existing Node Docker build
continues to use the root `Dockerfile`; this phase does not connect the Go
listener to the Compose or production route.

## Read-only database boundary

`internal/store/sqlite.OpenReadOnly` uses the pure-Go `modernc.org/sqlite`
driver and SQLite's `mode=ro` URI option. It opens an existing regular file,
sets one database connection, pings it, and verifies the `sync_runs` and
`app_settings` tables and the columns required by this phase. It never creates a
file or parent directory, executes schema migrations, seeds rows, changes
journal mode, or writes to SQLite.

The store owns the only SQL in this service:

- `GetLatestSync` follows `server/database.ts` and selects the newest completed
  or failed row from `sync_runs`, excluding `running` rows.
- `GetDashboardRevision` reads the `dashboard_revision` app setting and falls
  back to `0`, matching the TypeScript parser's non-negative integer behavior.

`internal/publicview` maps the store record to the same sanitized fields as
`server/public-view.ts`. Counts, raw messages, job IDs, trigger fields, source
item IDs, paths, credentials, and provider metadata never enter the HTTP
response.

## TypeScript to Go package mapping

| TypeScript contract                         | Go package                                   |
| ------------------------------------------- | -------------------------------------------- |
| `server/index.ts` listener and startup      | `cmd/kano-go/main.go` plus `internal/config` |
| `server/database.ts` `getLatestSync`        | `internal/store/sqlite.GetLatestSync`        |
| `server/database.ts` `getDashboardRevision` | `internal/store/sqlite.GetDashboardRevision` |
| `server/public-view.ts` `publicSyncRun`     | `internal/publicview.PublicSyncRun`          |
| `server/app.ts` `/api/health`               | `internal/httpapi` health route              |
| `server/app.ts` `/api/dashboard/revision`   | `internal/httpapi` revision route            |

Successful public reads use the existing JSON field names and ISO timestamps.
Health keeps the TypeScript `now`, service name, and fixed Chinese sync status
messages. Revision keeps `changed` semantics, `Cache-Control: no-store`, and
`Vary: Accept-Encoding`. Unknown paths return `api_not_found`; unsupported
methods return `method_not_allowed`; read failures return the stable
`database_unavailable` code without the underlying error text.

## Driver and deployment effects

`modernc.org/sqlite` is selected because it does not require CGO. This keeps
the module straightforward to build on Windows and in a small Linux builder;
the dependency is pure Go but is larger than a CGO-backed SQLite driver. The
current repository Docker build remains the Node 24 image in the root
`Dockerfile`; a future Go image should use a Linux Go builder and copy the
statically linked binary into a minimal runtime image. Windows operators can
run the same module with the local Go toolchain and an explicit Windows SQLite
path.

## Known limits

This service does not provide the dashboard snapshot, calendar, media, profile,
admin, MCP, synchronization, source history, scheduler, extraction, or LLM
routes. It also does not own database initialization or schema upgrades. The
two required tables are checked at startup, but broader schema compatibility is
intentionally deferred until a later migration slice.

## Suggested phase two

The next slice should add a read-only dashboard projection behind a separately
reviewed store query and fixture set, including bounded event/post/video reads
and media readiness mapping. It should preserve the same public projection
boundary and run beside TypeScript until response and operational parity are
measured. Admin and synchronization writes should remain out of scope until a
separate ownership and rollback plan exists.

Before switching any production traffic, the Go service must have parity tests
against synthetic TypeScript fixtures, bounded-query and timeout measurements,
container and Windows startup checks, read-only file and WAL validation,
observability and rollback procedures, security review, and an explicit route
cutover plan. No production cutover is part of phase one.
