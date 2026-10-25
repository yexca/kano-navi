# Testing and CI

The shipped backend is Go. `make go-test` runs its contracts and vet without
Docker. `make ci-backend` runs golangci-lint with `scripts/golangci-lint.yml`,
vet, and the contracts again under the race detector (`make go-race`, which
needs cgo and a C compiler). `make go-check` also validates formatting, CGO-free operation,
Linux/Windows builds and disposable read-only/development containers. Source
tests, including the historical-window API adapter, use fake transports and
synthetic credentials; no check synchronizes live sources. See
[Go backend](../architecture/go-standalone.md).

`make test-frontend` runs the frontend tests in `tests/`: the dashboard
request hook, the month archive reads, polling, and the history catalog and
packaging.

The Makefile is the canonical validation entry point. GitHub Actions invokes
its targets so local and hosted checks protect the same contracts. Use the
smallest target that covers the change; aggregate checks are for broad changes,
CI/tooling work, and pre-commit validation.

## Validation Targets

| Change or task                                          | Target                | Requirements                             |
| ------------------------------------------------------- | --------------------- | ---------------------------------------- |
| Documentation links                                     | make docs-check       | Node.js                                  |
| Formatting, docs, workflows, scanner and source privacy | make ci-style         | Locked dependencies and Go               |
| SQLite, API, sync, media or extraction                  | make ci-backend       | Go and a C compiler                      |
| TypeScript frontend, scripts, tests, and config         | make typecheck        | Locked dependencies                      |
| Public or admin UI                                      | make ci-frontend      | Locked dependencies                      |
| Seed and HTTP snapshot integration                      | make smoke            | Node.js and Go                           |
| Production image                                        | make docker-build     | Docker with Linux containers             |
| Production entry point, pages and guards                | make production-smoke | The locally built DOCKER_IMAGE           |
| All checks without Docker or installation               | make check            | Locked dependencies, Go and a C compiler |
| Full locally portable Actions validation                | make ci-local         | Locked dependencies and Docker           |
| Install then full validation                            | make ci               | Node/npm, Go and Docker                  |
| Manual privacy review before handoff or commit          | make sensitive-check  | Node and Git                             |
| Dependency advisories and npm registry signatures       | make audit            | Locked installation, Go and network      |

make install uses npm ci. Narrow targets and ci-local do not reinstall.
make ci installs first, verifies the registry signatures of the fresh
installation with make npm-audit-signatures, then runs ci-local. Both full targets enforce image
build before production smoke even under make -j. Override NPM, NODE, DOCKER,
DOCKER_BUILD, DOCKER_BUILD_ARGS, or DOCKER_IMAGE for the corresponding toolchain.
GOLANGCI_LINT_VERSION, ACTIONLINT_VERSION and GOVULNCHECK_VERSION pin the Go
tools that `go run` builds; Dependabot does not update them.
DOCKER is an executable path/name for the smoke helper, not a shell command.
Existing check-docs, check-sensitive, and privacy-check aliases remain supported.
npm run check is the Node-only formatting/test/privacy/docs/build sequence; use
make check to include the Go tests and API smoke as well.

## Cross-platform Formatting

Prettier accepts the file's existing LF/CRLF style through endOfLine=auto,
so a Windows checkout does not require rewriting otherwise unchanged source.
Local tool directories (.claude and .codegraph) are excluded from formatting;
this does not widen the privacy scanner's exclusions.

## Test Scope

Tests should guard an observable contract, state transition, security boundary,
or previous regression. Choose the lowest sufficient layer. Go tests live
beside their source as `*_test.go`, frontend tests live in `tests/`, and
scanner tests live under `scripts/`. Do not add tests for prose or a purely
visual change without a documented interaction or responsive contract. Do not
mirror internal implementation.

Backend coverage, by package under `backend-go/internal/`:

- `store/sqlite`: dashboard and calendar projections and their bounds over a
  10,000-record synthetic history, stream identity, manual locks and
  tombstones, read-only opening, startup upgrades of older snapshots (including
  the legacy events rebuild and selected profile repair), source writes that
  keep cursors on rollback, and history pages that commit with their checkpoint.
  Retry tests cover restart/rediscovery backoff, missing-file recovery, retained
  ready files and 304 resets; freshness tests exclude historical and cursor-only
  writes and retain timeline head-page times across resumed pages. Admin post
  tests cover ID search before pagination and inclusive Japan-date boundaries.
- `httpapi`: public reads, media containment and validators, compression and
  static caching, the admin session and origin guard, event mutations, and the
  operator console routes for workflows, jobs and source fetch windows.
- `workflow` and `jobs`: single-flight reservation, leases, retry, recovery,
  cancellation, timers, and the source, media and history steps.
- `source/x`, `source/youtube`, `source/history`, `mediadownload`: adapters
  against fake transports, covering budgets, pending work, account isolation,
  oversized and redirected responses, controlled error codes and resumed pages.
- `schedule` and `llmadmin`: extraction and normalization against recorded
  reference vectors (`schedule/testdata/*.json`), protocol-specific provider
  requests, routing and failover, provider validation, and secret handling.
- `mcpapi`, `adminauth`, `contentadmin`, `secretstore`: the public and
  control tool boundaries, login and rate limits, profile media and schedule
  image review, and the stored secret format.

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

`tests/dashboard-requests.test.ts` loads the actual hook through Vite without
environment files and uses fake time and controlled responses to exercise
recovery, stalled JSON deadlines, deduplication, races, visibility and
unmount. Its lightweight React renderer is test-only; browser review covers
the production React DOM bundle. `tests/polling.test.ts` uses controlled
timers to check retry cadence, its cap, deadlines, overlapping-work
prevention, stale responses and disposal. `tests/history-media.test.ts` checks
the history catalog, the shipped image bytes and packaging validation. No
timing thresholds enter CI.

To review the real hooks and responsive UI against a backend, start an
isolated instance: run `kano-go serve` from a temporary directory with
`APP_MODE=development`, a synthetic `LLM_SECRETS_KEY`,
`WORKFLOW_SCHEDULER_ENABLED=0` and a temporary `-database`, and point Vite at
it with `KANO_API_PORT`. Do not exercise a development build against the
operator's `data/` directory. The synthetic 10,000-record benchmark is
`go test ./internal/store/sqlite -run '^$' -bench BenchmarkDashboardSynthetic10k`
in `backend-go/`; see [Performance measurements](performance.md) for the
recorded history.

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
Style -> Backend, Go gate, Frontend, API smoke, Docker image -> Validate
```

Style runs make ci-style, which also checks the workflow files and their shell
snippets with actionlint (shellcheck is preinstalled on the runner). Backend
runs make ci-backend: golangci-lint, vet and the Go contract tests under the
race detector. Go gate runs make go-check: formatting, CGO-free and cross-platform
builds and the read-only container smoke. Frontend runs make
npm-audit-signatures on its fresh installation, then make ci-frontend: the
type check, the build and the frontend tests. API smoke runs make smoke
against the Go backend. Docker
image runs make docker-build, which compiles the Go backend and the frontend
into the release image, with Buildx cache arguments and then make
production-smoke against that image.
Every runner has a timeout, locked installation, and read-only repository
permissions. Checkout does not persist credentials. Caches speed installation
and image layers but never skip validation targets. Actions are pinned to
reviewed commit SHAs. Every Node job uses the local
`.github/actions/setup-node` action, which installs the version in `.nvmrc`;
that version and the Go version in backend-go/go.mod match the Docker build
toolchain.

`make ci-frontend` type-checks the frontend, scripts and tests before Vite
builds the browser bundle, then runs the frontend tests. Node executes script
and test `.ts` files directly; runtime execution alone does not check types.
No source files are excluded with `@ts-nocheck`.

Validate always evaluates every job result and fails on failure,
cancellation, or a skipped job. There is no path-based job selection; even
documentation-only changes run the complete sequence. The existing Validate
and Docker image check names are preserved. Release calls this workflow rather
than maintaining another list of check commands, then grants write permissions
only to publication jobs. Only normal CI runs cancel superseded runs; release
publication is serialized per tag.

The Dependency audit workflow runs daily at 03:17 UTC and on manual dispatch.
Its npm job runs make npm-audit-production and then make npm-audit, which read
only package-lock.json, then installs the locked tree and runs make
npm-audit-signatures. Its Go job runs make go-audit:
govulncheck, pinned by GOVULNCHECK_VERSION, reports vulnerabilities that the
backend actually reaches in its modules and in the standard library of the
go.mod toolchain that the release image also uses. A new advisory fails the
run without changing the repository. These targets query live advisory
databases, so they stay outside check and ci and never gate a pull request.
Both jobs run only in the upstream repository, so forks do not inherit the
schedule.

Dependabot (`.github/dependabot.yml`) proposes weekly grouped updates for
GitHub Actions, Go modules and npm packages after a seven-day cooldown. The
Node and Go toolchains and the Docker base images are upgraded by hand; see
[Dependency updates](security.md#dependency-updates).

Issue forms in `.github/ISSUE_TEMPLATE/` collect bug reports and feature
requests with synthetic data, and route suspected vulnerabilities to private
security reporting as the [security policy](../../SECURITY.md) describes.

## Related Docs

- [Local development](local-dev.md)
- [Design](design.md)
- [Secure development](security.md)
- [Commit and release](commit-and-release.md)
