# Kano Navi Go backend

This module is the backend. `kano-go serve` runs the whole site: public API,
operator console, workflows and timers, historical source windows, schedule
extraction, MCP and the built frontend. `kano-go seed`, `sync`, `migrate-llm`
and `healthcheck` are its maintenance commands.

From the repository root, `make build && make start` runs it against `data/`,
`dist/` and `.env`; `make dev` runs it behind Vite; `make docker-build` builds
the release image from the root `Dockerfile`. See
[Go backend](../docs/architecture/go-standalone.md) for commands, startup
order, route ownership and rollback.

## Layout

```text
cmd/kano-go        entry point and commands
cmd/kano-fixture   synthetic snapshot generator for the container smoke
internal/httpapi   public reads, media, admin routes, static frontend
internal/adminauth session, login, origin guard
internal/store     SQLite schema, upgrades, queries and writes
internal/source    X and YouTube adapters, historical API windows
internal/workflow  durable runs, steps, queue, timers
internal/jobs      operator job projection of workflow runs
internal/schedule  LLM schedule extraction
internal/llmadmin  providers, models, routes, settings
internal/contentadmin  profile media, schedule images, post reprocess
internal/mcpapi    /mcp tools
```

The module uses the pure-Go `modernc.org/sqlite` driver and builds with
`CGO_ENABLED=0`. It declares Go `1.26.4`; CI reads that version from
`go.mod`.

## Checks

From the repository root:

```text
make go-test    go test and go vet
make go-check   formatting, tests, vet, CGO-free and Linux/Windows builds,
                and the read-only container smoke
make smoke      seed twice and read a fresh snapshot through kano-go serve
```

Tests use fake transports, synthetic records and temporary SQLite files; none
contacts X, YouTube, a CDN or a model provider. `internal/schedule/testdata`
holds reference vectors recorded from the original TypeScript implementation,
which keep stored identities and fingerprints compatible with earlier
releases.

The synthetic store benchmark uses 10,000 posts, events and videos:

```text
go test ./internal/store/sqlite -run '^$' -bench BenchmarkDashboardSynthetic10k -benchmem
```

## Read-only sidecar

Run with flags only and no command, the binary opens an existing snapshot in
SQLite `mode=ro` on `:7658` and serves the public reads without migrations,
seeds or writes. `backend-go/Dockerfile` packages that mode, and
`scripts/go-docker-smoke.mjs` (part of `make go-check`) mounts a synthetic
`data/` tree read-only and verifies that no database sidecar, WAL, journal or
directory is created. The `-enable-*` flags of this mode require
`APP_MODE=development` and a disposable database.

The phases that led here are recorded in
[Go backend migration](../docs/architecture/go-migration.md).
