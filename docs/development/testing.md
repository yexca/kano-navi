# Testing and CI

The Makefile is the canonical validation entry point. GitHub Actions invokes
its targets so local and hosted checks protect the same contracts. Use the
smallest target that covers the change; aggregate checks are for broad changes,
CI/tooling work, and pre-commit validation.

## Validation Targets

| Change or task                                        | Target                | Requirements                   |
| ----------------------------------------------------- | --------------------- | ------------------------------ |
| Documentation links                                   | make docs-check       | Node.js                        |
| Formatting, docs, scanner behavior and source privacy | make ci-style         | Locked dependencies            |
| SQLite, API, sync, media or extraction                | make ci-backend       | Locked dependencies            |
| TypeScript source, scripts, tests, and config         | make typecheck        | Locked dependencies            |
| Public or admin UI                                    | make ci-frontend      | Locked dependencies            |
| Seed and HTTP snapshot integration                    | make smoke            | Locked dependencies            |
| Production image                                      | make docker-build     | Docker with Linux containers   |
| Production entry point, pages and guards              | make production-smoke | The locally built DOCKER_IMAGE |
| All checks without Docker or installation             | make check            | Locked dependencies            |
| Full locally portable Actions validation              | make ci-local         | Locked dependencies and Docker |
| Install then full validation                          | make ci               | Node/npm and Docker            |
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
For API/schema changes, run server tests and smoke. The smoke helper seeds a
temporary SQLite file twice, checks fresh snapshot reads, revision polling,
unready media and unauthenticated admin access, then removes the fixture.
It never opens the operator database or imports .env. Initialization may create
the standard empty cache directories; it does not download media.

`server/local-api.test.ts` checks the shared development and preview listener
with an injected in-memory snapshot and temporary frontend. It verifies page
routes, API reads, media and MCP boundaries, and same-origin admin login without
opening the runtime entry point or loading operator credentials.

Production smoke creates a disposable container from the built image, uses
synthetic production authentication, disables workflow timers, and publishes a
random loopback-only port. Its anonymous data volume is removed in cleanup.
It validates server/index.ts startup, fresh SQLite, public API, SPA page routes,
and the protected admin API. It does not mount host data or pass host secrets.
This HTTP check does not claim to verify browser interaction.

## GitHub Actions

The CI workflow runs on pull requests, pushes to main, and reusable workflow
calls from Release:

```text
Style -> Backend, Frontend, API smoke, Docker image -> Validate
```

Style runs make ci-style. Backend and Frontend run their corresponding narrow
Make targets. API smoke runs make smoke. Docker image runs make docker-build
with Buildx cache arguments and then make production-smoke against that image.
Every runner has a timeout, locked installation, and read-only repository
permissions. Checkout does not persist credentials. Caches speed installation
and image layers but never skip validation targets. Actions are pinned to
reviewed commit SHAs; Node 24.19.0 matches the Docker build toolchain.

`make ci-backend` checks all TypeScript files before the server tests.
`make ci-frontend` runs the same type check before Vite builds the browser bundle.
Node executes server and test `.ts` files directly; runtime execution alone does
not check types. No source files are excluded with `@ts-nocheck`.

Validate always evaluates all five job results and fails on failure,
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
