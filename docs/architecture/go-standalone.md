# Go standalone backend

Phase twelve lets the Go binary own the whole site. `kano-go -standalone`
serves the public API, the operator console, workflows and timers, MCP and the
built frontend from one process, against the same SQLite snapshot and `data/`
tree the TypeScript service uses. The TypeScript service and image remain in
the repository as the rollback path; the published image has not been switched.

Without `-standalone` the binary is still the read-only sidecar described in
[Go backend migration](go-migration.md), with its development-only flags.

## Running

```bash
make build      # frontend into dist/
make go-start   # builds backend-go/bin/kano-go and runs it with -standalone
```

The process reads `.env` from the working directory (existing environment
variables win), `data/database/kano.sqlite`, and `dist/`. It listens on `PORT`
(default `7657`). `-database`, `-static` and `-listen` override those paths.
`make go-docker-build` builds `go.Dockerfile`: a scratch image containing the
binary, `dist/`, `public/` and a CA bundle, with `/app/data` as its volume and
`kano-go -healthcheck` as the container health check. It accepts the same
environment as the TypeScript image, so the existing Compose file works with
`KANO_IMAGE` pointed at it.

Do not run the Go and TypeScript services against the same snapshot at the
same time. Both write SQLite and both would start timed workflows.

## Startup order

1. `ADMIN_PASSWORD` (production default, at least 12 non-padding characters)
   and a non-blank `LLM_SECRETS_KEY` are validated before SQLite is opened.
2. A missing database is created from the embedded schema and public static
   seeds. Source snapshots, Featured data and providers start empty.
3. An existing snapshot from an earlier release is upgraded additively: missing
   tables, columns and indexes are added from the embedded schema, followed by
   the documented backfills (schedule periods, post account handles, focus
   video IDs, provider capabilities and retries, the one-time route-target copy,
   default catalog models, and the message/board route defaults). Nothing is
   dropped, rebuilt or cleared. A database without the core tables, or one that
   predates `events.starts_on`, is refused unchanged; start the TypeScript
   service once to rebuild it.
4. Missing static seed rows are inserted; existing rows and settings are kept.
5. The selected avatar and banner are reconciled with the files on disk
   (`active_sha256`, `active_mime_type`), bumping the dashboard revision only
   when something changed.
6. Interrupted workflow runs are recovered, then the timer starts unless
   `WORKFLOW_SCHEDULER_ENABLED=0`.

## Route ownership

| Area                                           | Package                                                                  |
| ---------------------------------------------- | ------------------------------------------------------------------------ |
| Public reads, media, health                    | `internal/httpapi`, `publicview`                                         |
| Session, login, origin guard                   | `internal/adminauth`                                                     |
| Events, posts, videos, run reads               | `internal/httpapi` + `store/sqlite`                                      |
| Workflows, jobs, source fetch                  | `internal/httpapi/admin_compat.go`, `internal/jobs`, `internal/workflow` |
| Config, providers, models, routes              | `internal/llmadmin`                                                      |
| Profile media, schedule images, post reprocess | `internal/contentadmin`                                                  |
| Schedule extraction step                       | `internal/schedule`                                                      |
| `/mcp`                                         | `internal/mcpapi`                                                        |
| Built frontend and SPA fallback                | `internal/httpapi/static.go`                                             |

Admin feature packages implement `httpx.AdminModule`. The router authenticates
the session and applies the origin guard before a module sees a request.

## Jobs and workflows

The console's job list is a projection of durable workflow runs. A saved
workflow runs under its own ID. A full sync, an automatic scan and a
single-source fetch run from unsaved definitions with the reserved IDs
`@sync`, `@scan` and `@fetch`; they never appear among saved workflows. All of
them share the SQLite single-flight reservation, so a second start answers
`409` with `already_running` and the active job.

Differences from the TypeScript job manager that operators can observe:

- Jobs survive a restart and are recovered; the list is no longer limited to
  the current process lifetime.
- Sync and scan jobs also report per-step progress.
- A failed X or YouTube step is recorded and the remaining steps still run; the
  job finishes as partial, or failed when no step succeeded.
- Steps disabled through `SKIP_X`, `SKIP_YOUTUBE`, `SKIP_MEDIA` or `SKIP_LLM`
  are recorded as skipped.

Each step's counters are merged into the job's `sync_runs.counts_json` row.

## Not yet in Go

- Historical source windows (`before` and `range`) through the X and YouTube
  APIs. `GET /api/admin/sources/fetch` reports `historyAvailable: false` and
  those modes are rejected with a validation message.
- The rebuilding migration for databases that predate event sources.
- The `seed`, `sync` and `migrate:llm` command-line entry points. Seeding
  happens at startup; synchronization is started from the console, MCP or a
  timer.
- Brotli. Public JSON and build text are gzip-compressed with the same
  path rules as the Express listener.
- Vite development still attaches the TypeScript API. Use `make go-start`
  against a build to exercise the Go backend.

## Rollback

Stop the Go process and start the TypeScript service on the same `data/`
directory. The additive `workflow_*` tables are ignored by TypeScript, and
provider secrets use the same encryption format in both directions. Saved
workflows keep their definitions; their last status may read as an unknown
value until the next run.
