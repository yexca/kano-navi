# Testing and CI

The shipped backend is Go. `make go-test` runs its contracts and vet without
Docker. `make go-check` also validates formatting, CGO-free operation,
Linux/Windows builds and disposable read-only/development containers. Source
tests, including the historical-window API adapter, use fake transports and
synthetic credentials; no check synchronizes live sources. See
[Go backend](../architecture/go-standalone.md).

`make test-server` runs the TypeScript reference implementation's tests, and
the comparison scripts (`dashboard-parity`, `side-by-side-http`) hold the Go
projections to it. Change behavior in Go first, then keep the reference and
these comparisons passing.

The Makefile is the canonical validation entry point. GitHub Actions invokes
its targets so local and hosted checks protect the same contracts. Use the
smallest target that covers the change; aggregate checks are for broad changes,
CI/tooling work, and pre-commit validation.

## Validation Targets

| Change or task                                        | Target                | Requirements                   |
| ----------------------------------------------------- | --------------------- | ------------------------------ |
| Documentation links                                   | make docs-check       | Node.js                        |
| Formatting, docs, scanner behavior and source privacy | make ci-style         | Locked dependencies            |
| Go backend only                                       | make go-test          | Go                             |
| SQLite, API, sync, media or extraction                | make ci-backend       | Locked dependencies and Go     |
| TypeScript source, scripts, tests, and config         | make typecheck        | Locked dependencies            |
| Public or admin UI                                    | make ci-frontend      | Locked dependencies            |
| Seed and HTTP snapshot integration                    | make smoke            | Node.js and Go                 |
| Production image                                      | make docker-build     | Docker with Linux containers   |
| Production entry point, pages and guards              | make production-smoke | The locally built DOCKER_IMAGE |
| All checks without Docker or installation             | make check            | Locked dependencies and Go     |
| Full locally portable Actions validation              | make ci-local         | Locked dependencies and Docker |
| Install then full validation                          | make ci               | Node/npm, Go and Docker        |
| Manual privacy review before handoff or commit        | make sensitive-check  | Node and Git                   |

make install uses npm ci. Narrow targets and ci-local do not reinstall.
make ci installs first, then runs ci-local. Both full targets enforce image
build before production smoke even under make -j. Override NPM, NODE, DOCKER,
DOCKER_BUILD, DOCKER_BUILD_ARGS, or DOCKER_IMAGE for the corresponding toolchain.
DOCKER is an executable path/name for the smoke helper, not a shell command.
Existing check-docs, check-sensitive, and privacy-check aliases remain supported.
npm run check remains the previous non-Docker formatting/test/privacy/docs/build
sequence; use make check to include API smoke as well.

## Cross-platform Formatting

Prettier accepts the file's existing LF/CRLF style through endOfLine=auto,
so a Windows checkout does not require rewriting otherwise unchanged source.
Local tool directories (.claude and .codegraph) are excluded from formatting;
this does not widen the privacy scanner's exclusions.

## Test Scope

Tests should guard an observable contract, state transition, security boundary,
or previous regression. Choose the lowest sufficient layer. Server tests live
beside their source as server/*.test.ts and scanner tests live under scripts/.
Do not add tests for prose or a purely visual change without a documented
interaction or responsive contract. Do not mirror internal implementation.

`server/logic-regression.test.ts` covers anonymous HTTP/MCP white lists,
cancellation-status bypasses, protocol-specific request bodies, fair backlog
processing, image-review failure aggregation, and snapshot time transitions.
`server/http-fetch.test.ts` uses delayed/never-ending synthetic streams for body
timeout and byte-limit checks; source-job tests verify queue recovery. Profile
redownload tests use a disposable SQLite fixture, restore existing selected
files, and check actual response bytes, URL versions, and ETags.
Upgrade fixtures also keep different selected/candidate bytes and MIME types,
exercise both the legacy schema and previously incorrect active metadata, repeat
startup, verify revision advances from 17 to 18 on repair and remains 18 on the
next startup (including MIME-only repair), and follow the existing HTTP revision
hint to reload the repaired avatar URL. They preserve candidate download and
manual selection state, settings, and unrelated snapshots, and reject missing,
escaping/symlinked, unsupported, and oversized files. Snapshot-to-spotlight regressions
cover explicit stream ends and the three-hour fallback before, at, and after
their boundary using public HTTP snapshots. They cover equivalent YouTube watch,
short, live, shorts, and share links in both directions, ordinary exact URL
matching, and unrelated videos/lookalike hosts that must retain their own grace
period.

`server/schedule-regression.test.ts` protects same-day event coexistence,
re-extraction through changing event counts, legacy identities and collisions, manual locks and
tombstones, transaction rollback, X/YouTube merging, confirmed stream
cancellation, and mixed-offset ordering in API/admin/board/spotlight consumers.
`server/sync.test.ts` covers budget-limited and failed X details, durable pending
work beyond profile/bootstrap coverage, account isolation, and date-window
separation. It also guards against persistent 404 retry starvation, verifies
503 recovery, and checks fair rotation with new arrivals under one- and
two-request budgets. YouTube overlap fixtures recover from incomplete HTTP
200 pages to reservations under the detail budget, preserve existing snapshots
after invalid refreshes, complete ordinary-video checks, and verify that selected
windows leave normal incremental cursors/check metadata intact. These fixtures use
synthetic records, in-memory/temporary SQLite and mocked
platform/model responses.

Use manually authored synthetic records, example.invalid URLs, fixed neutral
timestamps, and temporary files. Never derive fixtures from operator databases,
provider responses, credentials, local paths, or logs. Do not call live X,
YouTube, image CDN, or model APIs. Dependency installation and image builds may
access package registries; that is separate from platform-data synchronization.

For UI changes, run the build and review the affected pages at desktop and
mobile widths, including loading, empty, error, stale, and dialog states.
For API/schema changes, run the backend tests and smoke. The smoke helper
builds the Go backend into a temporary directory, runs `kano-go seed` twice
against a temporary SQLite file, starts `kano-go serve` on a loopback port,
checks fresh snapshot reads, revision polling, unready media and
unauthenticated admin access, then removes the fixture. The backend runs from
that temporary directory with an explicit synthetic environment, so it never
opens the operator database or reads .env. Initialization may create
the standard empty cache directories; it does not download media.

`server/http-performance.test.ts` checks wire-level compression negotiation,
decoded content, weak ETags, conditional/HEAD/range responses, static cache
boundaries and exclusions for authentication/admin, MCP and media. Upgrade
tests verify indexed source lookups and unchanged synthetic records/configuration.
`server/dashboard-requests.test.ts` loads the actual hook through Vite without
the runtime plugin or environment files and uses fake time and controlled
responses to exercise recovery, stalled JSON deadlines, deduplication, races,
visibility and unmount. Its lightweight React renderer is test-only; browser
review covers the production React DOM bundle. No timing thresholds enter CI.

For repeatable latency/transfer measurements, run
`node scripts/benchmark-api.ts measurement` after building. It uses two fixed
in-memory synthetic SQLite snapshots and separate server/client processes,
includes historical schedule images/source posts, and reports median/P95,
identity/gzip wire bytes and headers at concurrency 1 and 4. It does not load
`.env`, enable timers or perform source synchronization. Run measurements without
concurrent builds/tests. `--browser small` serves a disposable UI fixture with
stdin-only failure/delay controls. See [Performance measurements](performance.md)
for the recorded before/after conditions, results and limitations.

Production smoke creates a disposable container from the built image, uses
synthetic production authentication, disables workflow timers, and publishes a
random loopback-only port. Its anonymous data volume is removed in cleanup.
It validates the image's `kano-go serve` entry point, fresh SQLite, public API,
SPA page routes, and the protected admin API. It does not mount host data or pass host secrets.
This HTTP check does not claim to verify browser interaction.

## GitHub Actions

The CI workflow runs on pull requests, pushes to main, and reusable workflow
calls from Release:

```text
Style -> Backend, Go backend, Frontend, API smoke, Docker image -> Validate
```

Style runs make ci-style. Backend runs make public-read-gate: the TypeScript
reference and Go contract tests, the TypeScript/Go comparisons and the Go
gate. Go backend runs make go-check and the container rehearsal. Frontend runs
make ci-frontend. API smoke runs make smoke against the Go backend. Docker
image runs make docker-build, which compiles the Go backend and the frontend
into the release image, with Buildx cache arguments and then make
production-smoke against that image.
Every runner has a timeout, locked installation, and read-only repository
permissions. Checkout does not persist credentials. Caches speed installation
and image layers but never skip validation targets. Actions are pinned to
reviewed commit SHAs; Node 24.19.0 and the Go version in backend-go/go.mod
match the Docker build toolchain.

`make ci-backend` checks all TypeScript files before the reference tests, then
runs the Go tests and vet.
`make ci-frontend` runs the same type check before Vite builds the browser bundle.
Node executes server and test `.ts` files directly; runtime execution alone does
not check types. No source files are excluded with `@ts-nocheck`.

Validate always evaluates every job result and fails on failure,
cancellation, or a skipped job. There is no path-based job selection; even
documentation-only changes run the complete sequence. The existing Validate
and Docker image check names are preserved. Release calls this workflow rather
than maintaining another list of check commands, then grants write permissions
only to publication jobs. Only normal CI runs cancel superseded runs; release
publication is serialized per tag.

## Related Docs

- [Local development](local-dev.md)
- [Design](design.md)
- [Secure development](security.md)
- [Commit and release](commit-and-release.md)

## Concurrent and Bounded-read Regressions

`server/repair-regression.test.ts` covers deferred image responses during manual
accept/reject, image replacement/restoration/content changes, credential-bearing
parser and unexpected errors with captured log assertions, complete media-list
reconciliation and rollback, partial/missing image caches, applicable periods,
manual correction and additive migration. A 10,000-record synthetic history
checks bounded snapshots, full calendar pagination, counts and public projection.
`server/sync.test.ts` also reopens temporary SQLite during one-request X/account
and YouTube/detail rotation, including persistent 404 and recovery.
`server/polling.test.ts` uses controlled timers to check retry cadence, its cap,
deadlines, overlapping-work prevention, stale responses and disposal.

For the real hooks and responsive UI, run `node scripts/ui-regression.ts` and
open its printed loopback URL. The fixture uses in-memory synthetic state,
injected inert jobs, no runtime entry point or .env, and no external requests.
The browser regression button advances a controlled clock through initial
failure, one 503, sustained failures/recovery, 401, stalled requests, superseded
responses and unmount for both hooks. It also checks hidden-page polling and
calendar pagination/navigation. Review `/` and `/admin` at desktop and mobile widths, including
calendar navigation and the period correction fields. Stop the fixture after
verification; it does not write an operator snapshot.

The calendar scenario advances past the minute refresh after loading 101 events,
verifies unchanged revisions preserve both pages, replaces both pages on a revision
change, retains them after a failed second-page refresh and retries a revision
change between pages. A separate race scenario rejects a delayed previous-week
response after navigation. The historical video regression checks the first
10,000-event/10,000-video snapshot and its actual SQLite query plans, ensuring
associations use indexes rather than per-video event scans. Live-focus fixtures
cover qualification before truncation, cross-week streams, chronological selection,
explicit ends and the three-hour boundary.
