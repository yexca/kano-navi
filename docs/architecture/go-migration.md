# Go backend migration: phase two

## Goal and boundary

Phase two adds a read-only Go projection for the public dashboard while the
TypeScript service remains the production owner. The Go service now exposes:

- `GET /api/health`
- `GET /api/dashboard/revision`
- `GET /api/dashboard?days=3`

The dashboard projection covers the current public profile, summary, posts,
events, videos, focus, timeline, resources, schedule assets, profile media,
media readiness fields, synchronization metadata, and revision metadata. Its
field set is produced by `internal/publicview`, which removes source item IDs,
raw JSON, job and trigger fields, provider information, database paths, and
other operator-only data.

Admin, calendar, MCP, synchronization, workflow scheduling, schedule
extraction, LLM routing, media downloads, and the `/media` file routes remain
owned by TypeScript. The Go service does not make external requests and does
not receive frontend or production traffic.

## Running alongside TypeScript

The Go module lives under `backend-go/` and listens on `:7658`; TypeScript
continues to listen on `:7657`:

```text
go run ./backend-go/cmd/kano-go -database data/database/kano.sqlite
```

The database path is explicit. Go opens the existing snapshot with the
pure-Go `modernc.org/sqlite` driver and SQLite `mode=ro`, uses one connection,
and validates the complete dashboard schema at startup. It never creates a
file or directory, runs migrations, seeds rows, changes journal mode, writes
SQLite state, downloads media, or proxies URLs.

The repository currently declares `go 1.26.4`. The available Windows toolchain
was verified as `go1.26.4`, so this phase does not change the module version.
Go CI, Docker pinning, and a supported Windows/Linux toolchain matrix remain a
phase-three operational task because this repository does not yet run Go in
CI.

## Dashboard query boundary

`internal/store/sqlite` owns all dashboard SQL. Reads are bounded to match the
TypeScript board: at most 100 posts in the requested window plus one latest-post
fallback, at most 100 week/upcoming/live event candidates before de-duplication
plus one linked event per returned video, at most 30 recent and 30 upcoming
videos plus the latest fallback and Featured selection, and at most 100
timeline rows, resources, other assets, and schedule assets. Summary counts use separate aggregates, so the
post payload remains bounded while its count remains accurate. Event ordering
uses parsed ISO instants, with date-only events sorted at the end of their
Japan date without inventing `startsAt`.

The store checks cancellation status, manual locks, tombstones, and suspected
cancellations at the same projection boundary as TypeScript. `internal/httpapi`
only parses and clamps `days` to 1 through 30, calls the store, and writes
stable JSON errors. Matched public routes set `Vary: Accept-Encoding` for
success, method errors, and database errors, and dashboard responses retain
`Cache-Control: no-store`.

## Media and profile media

This phase maps public media state only. A ready asset is exposed as the same
opaque `/media/<id>?v=<sha256>` URL when its hash and contained cache path are
valid; pending, failed, missing, unsafe, or unavailable files expose `null`
public URLs while retaining the public status. `/assets/...` is returned only
when the contained static file exists. Remote CDN URLs are never copied into a
public URL field. Profile slots use `/media/profile/<slot>?v=<sha256>` only for
an active, hash-validated selected file. Go has not taken over media serving,
so no frontend switch is possible in this phase.

## Fixtures and parity checks

Go tests build disposable SQLite fixtures with synthetic rows. They cover empty
snapshots, days parsing and clamping, X posts, YouTube videos, automatic and
manually locked events, tombstones, suspected cancellations, date-only and
mixed-offset ordering, unready media, sensitive-field exclusion, bounded post
reads, schema/query failures, and response headers. No test reads the
maintainer database or contacts X, YouTube, a CDN, or an LLM. The fixture
assertions mirror the TypeScript public projection statically; a live
TypeScript-to-Go comparison remains a future check when both test harnesses can
share a fixture without installing dependencies or making network requests.

## Future cutover work

Before any production route changes, phase three must add Go CI and pinned
Docker/Windows startup checks, a shared fixture comparison against the
TypeScript dashboard, query and timeout measurements, read-only file and WAL
validation, observability and rollback procedures, and an explicit route
cutover plan. Calendar, admin, MCP, synchronization, media serving, workflow,
and LLM ownership must be reviewed separately.
