# Backend

The backend is the Go module in `backend/`. `kano-go serve` runs the public
API, the operator console, workflows and timers, schedule extraction, MCP and
the built frontend from one process, against the SQLite snapshot and the
`data/` tree. Releases, the published image, local development and CI all run
it; no Node runtime is shipped. Node is the frontend build toolchain only.

Public HTTP and MCP contracts are in [Public API](public-api.md); operator
routes are in the [Admin API](admin-api.md).

## Commands

```text
kano-go serve        Serve the whole site (API, console, workflows, MCP, frontend)
kano-go seed         Create the database if missing and add missing static records
kano-go sync         Run one full synchronization and exit
kano-go migrate-llm  Seal OPENAI_API_KEY as the key of the openai-default provider
kano-go healthcheck  Probe the local service and exit
```

Every command reads `.env` from the working directory; variables already in
the environment win. `-database` overrides `data/database/kano.sqlite`, and
`serve` also accepts `-static` (default `dist`) and `-listen` (default `PORT`,
`7657`). `seed -overwrite` restores the shipped profile, timeline and resource
wording; it never touches source snapshots, events, media or providers.
`migrate-llm -force` replaces an already stored key.

From the repository root:

```bash
make build   # frontend into dist/
make start   # builds backend/bin/kano-go and runs `kano-go serve`
make seed    # or: make sync
```

The npm scripts `start`, `seed`, `sync` and `migrate:llm` run the same commands
through `scripts/run-app.mjs`. In the image, `docker compose run --rm kano sync`
(or `seed`, `migrate-llm`) runs a command against the mounted `/app/data`.

`kano-go sync` uses the same executor and SQLite single-flight reservation as
the service. If the service is already synchronizing, the command reports a
busy queue instead of starting a second run.

## Development

`make dev` builds the backend, starts it on `127.0.0.1:KANO_API_PORT` (default
`7658`) and starts Vite on `PORT`. Vite serves the frontend with hot reload and
proxies `/api`, `/media` and `/mcp` to the backend with the browser's Host
header, so authentication, media and MCP stay on one origin. `make preview`
does the same with the built bundle. Restart after changing Go source. The site
defaults to port `7657`; development and preview refuse to switch ports when it
is occupied.

## Startup Order

1. `ADMIN_PASSWORD` (production default, at least 12 non-padding characters)
   and a non-blank `LLM_SECRETS_KEY` are validated before SQLite is opened.
2. A missing database is created from the embedded schema
   (`internal/store/sqlite/bootstrap.sql`) and public static seeds. Source
   snapshots, Featured data and providers start empty.
3. An existing snapshot is upgraded in one transaction
   (`internal/store/sqlite/schema_upgrade.go`): missing tables, columns and
   indexes are added from the embedded schema, followed by the backfills
   (schedule periods, post account handles, focus video IDs, provider
   capabilities and retries, the route-target copy, default catalog models,
   and the merge of per-stage routes into the single model queue). An `events`
   table without event sources is rebuilt in place: every row keeps its ID,
   gains its Japan calendar date and becomes its own source evidence, and
   events curated from X are locked. Nothing else is dropped or cleared. A
   database without the core tables is refused unchanged.
4. Missing static seed rows are inserted; existing rows and settings are kept.
5. The selected avatar and banner are reconciled with the files on disk
   (`active_sha256`, `active_mime_type`), bumping the dashboard revision only
   when something changed.
6. Interrupted workflow runs are recovered, and a stored Bilibili interval
   below `BILIBILI_MIN_INTERVAL_MINUTES` is raised to it. No timers are seeded;
   [source schedules](workflows.md#source-schedules) hold them.
7. The timer starts unless `WORKFLOW_SCHEDULER_ENABLED=0`. It submits due
   source schedules, earliest first.

## Package Ownership

| Area                                           | Package                                                         |
| ---------------------------------------------- | --------------------------------------------------------------- |
| Entry point and commands                       | `cmd/kano-go`                                                   |
| Public reads, feeds, media, health             | `internal/httpapi`, `internal/feeds`, `internal/publicview`     |
| Session, login, origin guard                   | `internal/adminauth`                                            |
| Admin route table, overview, events, lists     | `internal/adminapi` + `internal/store/sqlite`                   |
| Runs, source schedules, source fetch           | `internal/adminapi`, `internal/jobs`, `internal/workflow`       |
| Historical source windows                      | `internal/source/history`, `internal/workflow/history_steps.go` |
| Extraction, providers, models, routes          | `internal/llmadmin`                                             |
| Profile media, schedule images, post reprocess | `internal/contentadmin`                                         |
| Schedule extraction step                       | `internal/schedule`                                             |
| `/mcp`                                         | `internal/mcpapi`                                               |
| Built frontend and SPA fallback                | `internal/httpapi/static.go`                                    |

`cmd/kano-go` validates credentials, opens the runtime database and starts the
listener; `internal/httpapi` builds the handler around an injected store.
`internal/adminapi` owns the single `/api/admin` route table described in the
[Admin API](admin-api.md). Feature packages (`internal/llmadmin`,
`internal/contentadmin` and the YouTube `websub` package) implement
`httpx.AdminModule` by returning their routes; the router authenticates the
session, applies the origin guard and refuses writes to a read-only database
before a module handler runs.

Responses stay stable and sanitized: `raw_json`, stack traces, credentials and
absolute local paths never reach the browser. Ready runtime media is served
only through the opaque-ID `/media/:id` route; the recovered LINE BLOG files
in `data/lineblog/` are served by archive name through `/media/lineblog/:name`
(see [Media cache](media-cache.md#line-blog-archive-media)).

## Authentication and Origin

The server defaults to `APP_MODE=production`, validates the admin password and
requires a non-blank `LLM_SECRETS_KEY` in every mode before opening or seeding
SQLite. An explicit `APP_MODE=development` bypasses authentication for local
work. Production uses in-memory HttpOnly, SameSite cookie sessions; restarting
the process invalidates all sessions. The API exposes provider-key
availability, never plaintext or ciphertext.

Mutating requests reject cross-site Fetch Metadata. Browser
`Sec-Fetch-Site: same-origin` requests remain valid when a reverse proxy
rewrites Host or terminates HTTPS; other requests with an `Origin` must match
the observed local origin.

## Jobs and Workflows

The console's job list is a projection of durable workflow runs. Every run is
ad hoc and names its kind of work with a reserved ID: `@sync`, `@scan`,
`@fetch` (manual fetches and one-off step runs), `@translate`, `@live` and
`@source-<source>` (a source's chain, from its timer or Run now). All of them
share the SQLite single-flight reservation, so a second start answers `409`
with `busy` and the active run.

Jobs survive a restart and are recovered. Every job reports per-step progress.
A failed X or YouTube step is recorded and the remaining steps still run; the
job finishes as partial, or failed when no step succeeded. A manual fetch is
the exception: its method's failure fails the job. Steps disabled
through `SKIP_X`, `SKIP_YOUTUBE`, `SKIP_MEDIA` or `SKIP_LLM` are recorded as
skipped. Each step's counters are merged into the job's `sync_runs.counts_json`
row. The step catalog, queue, timers and manual fetches are described in
[Workflows](workflows.md).

## Read-Only Mode

Run with flags only and no command, the binary opens an existing snapshot in
SQLite `mode=ro` on `:7658` and serves the public reads without migrations,
seeds or writes. `backend/Dockerfile` packages this mode, and
`scripts/go-docker-smoke.mjs` (part of `make go-check`) mounts a synthetic
`data/` tree read-only and verifies that no database sidecar, WAL, journal or
directory is created. Its `-enable-*` and `-initialize` flags are
development test switches: they require `APP_MODE=development` and a
disposable database.

## Text Transfer and Static Caching

Compression precedes the response handlers. Successful GET/HEAD public
dashboard, revision, health, calendar, post archive and video archive JSON,
the [calendar and Atom feeds](public-api.md#public-feeds), plus build
HTML/JavaScript/CSS, negotiate gzip through `Accept-Encoding`. The
[revision stream](public-api.md#revision-stream) is never compressed, since
gzip would hold its events back. Bodies below 1 KiB remain uncompressed. The
listener compresses on the fly with gzip only; clients that advertise only
Brotli or deflate receive the identity representation of those responses.
`Vary: Accept-Encoding` also accompanies eligible identity and conditional
responses. Existing weak ETags validate the uncompressed representation
across encodings; 304 and HEAD have no compressed body. Range responses,
non-success responses, already encoded bodies and `no-transform` responses
are not compressed on the fly.

The build precompresses its own text. `npm run build` (and the image's
`vite build`) writes a Brotli `.br` and a gzip `.gz` sibling at the strongest
settings next to every HTML, JavaScript, CSS, JSON and SVG file in `dist/`
(outside `dist/.vite/`) of at least 1 KiB, when the sibling is smaller
(`scripts/vite-precompress.ts`). The static site sends the first sibling the
request's `Accept-Encoding` admits, `br` before `gzip`, with
`Content-Encoding`, `Vary: Accept-Encoding`, the original's `Content-Type`
and `Cache-Control`, and the sibling's length. Last-Modified stays the
original's, so conditional requests answer alike for every encoding; HEAD
and 304 carry no body, and a range request on an encoded response addresses
the sibling's bytes. A sibling older than its original is stale and ignored.
`index.html` responses that carry rewritten share tags (a page's own tags,
the public origin, or a 404) never use the stored sibling and fall back to
on-the-fly gzip like any text without a sibling. Siblings have no URL of
their own: a request for `….br` or `….gz` of such a file is a missing page.

Admin/authentication responses, MCP (including streams and reflected protocol
inputs), runtime media and images are excluded. Public JSON compression is
credential-independent and contains no secrets or reflected free-text request
input. Do not extend this allowlist to secret-bearing/reflected responses or
apply unconditional compression at a reverse proxy. Admin and MCP additionally
send `Cache-Control: no-store, no-transform` so intermediaries also have an
explicit transformation prohibition.

Content-hashed JS/CSS in `dist/assets` listed in Vite's
`dist/.vite/manifest.json` receive `public, max-age=31536000, immutable`. So
does a fixed history file under `/assets/history/` (and its `w` copies) when
the request's single `v` is the SHA-256 of the file's bytes, which the server
checks ([Immutable Fixed Files](media-cache.md#immutable-fixed-files));
without `v`, or
with any other `v`, it revalidates. HTML (including SPA fallback routes),
icons and other files receive `public, max-age=0, must-revalidate`. A path that is neither a
build file nor one of the frontend's pages (`/`, `/history`, `/lineblog`,
`/about`, `/admin`, with or without a trailing slash) still receives `index.html`, which
shows the frontend's not-found view, but with status 404. `robots.txt` comes
from `public/` and disallows `/admin`, `/api/` and `/mcp` but allows the two
public feeds, `/api/calendar.ics` and `/api/feed.xml`, whose subscription
fetchers (Google Calendar's among them) honour it. `/index.html` is the file
itself, which the frontend shows as the board. A missing/invalid manifest
falls back to revalidation. Dynamic media retains its independent version/hash
validation and caching. The server keeps no dashboard response cache, because
the projection compares event and video times with the exact clock; the
dashboard, calendar and archive reads are revalidated by the browser through
their [ETags](public-api.md#public-snapshot-contract), and the revision read
and the revision stream are `no-store`.

## Listener Timeouts

The listener's read and write timeouts (15 seconds each, `-read-timeout` and
`-write-timeout`) bound every response except the
[revision stream](public-api.md#revision-stream). Shutdown ends the open
revision streams first, then waits for the remaining requests up to
`-shutdown-timeout` (10 seconds).

## Share Tags

`index.html` carries static Open Graph and Twitter card tags (site name,
description, the bundled banner) and `rel="alternate"` links to both feeds.
Those tags describe the board. When the server serves `index.html` for
`/history`, `/lineblog` or `/about`, it replaces the page's `<title>`, `description`,
`og:title`, `og:description` and `og:url` (and Twitter title and description
tags, if present) with that page's English title, description and path; the
route table `spaPages` in `backend/internal/httpapi/static.go` holds them
next to the list of known pages. `/`, `/admin` and missing pages keep the
file's own tags. Link previews need absolute URLs, which a static file cannot
know, so the server then rewrites the page's root-relative `content="/..."`
values to the optional `PUBLIC_ORIGIN` (HTML-escaped, although configuration
already accepts only a DNS name or IP address as its host). Without it the
tags stay relative. Nothing else in the build is rewritten.

## Related Docs

- [Public API](public-api.md)
- [Admin API](admin-api.md)
- [Data model](data-model.md)
- [Workflows](workflows.md)
- [Deployment security](../operations/security.md)
- [Secure development](../development/security.md)
