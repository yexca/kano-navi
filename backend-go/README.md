# Kano Navi Go backend

This module is the backend. `kano-go serve` runs the whole site: public API,
operator console, workflows and timers, historical source windows, schedule
extraction, MCP and the built frontend. `kano-go seed`, `sync`, `migrate-llm`
and `healthcheck` are its maintenance commands.

From the repository root, `make build && make start` runs it against `data/`,
`dist/` and `.env`; `make dev` runs it behind Vite; `make docker-build` builds
the release image from the root `Dockerfile`. See
[Backend](../docs/architecture/backend.md) for commands, startup order and
package ownership.

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
`CGO_ENABLED=0`. It declares Go `1.26.10`; CI reads that version from
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
holds fixed reference vectors that pin stored event identities and
fingerprints.

The synthetic store benchmark uses 10,000 posts, events and videos:

```text
go test ./internal/store/sqlite -run '^$' -bench BenchmarkDashboardSynthetic10k -benchmem
```

## Read-only mode

Run with flags only and no command, the binary serves the public reads from an
existing snapshot without writes; `backend-go/Dockerfile` packages that mode
for the container smoke in `make go-check`. See
[Read-only mode](../docs/architecture/backend.md#read-only-mode).
