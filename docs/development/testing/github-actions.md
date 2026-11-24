# GitHub Actions

The Actions workflows, the CI plan and the gate.

Validation runs in two tiers that share one set of job definitions:

| Tier   | Workflow | Runs on                                                | Selects                                 | Decides                            |
| ------ | -------- | ------------------------------------------------------ | --------------------------------------- | ---------------------------------- |
| `pr`   | `PR`     | Every pull request                                     | Jobs for the changed paths              | Whether the pull request may merge |
| `full` | `Full`   | Every push to `main`, and any branch or tag on request | Every validation job, no advisory audit | Whether `main` is healthy          |

`.github/workflows/validate.yml` holds the jobs and has no trigger of its own.
`pr.yml` and `full.yml` call it with an explicit `tier`, so a tier never
depends on the event or branch that started a run. The Release workflow checks
the source and requires a successful `Full` run whose `head_sha` is the exact
tagged commit; it does not call `validate.yml` again.

```text
Style
Plan -> Backend, Go builds, Go container, Frontend, API smoke, Docker image,
        Go audit, npm audit
Plan, Style, and all planned jobs -> Gate
```

Plan selects the remaining jobs, so they start without waiting for Style.
Style runs beside them and always runs make ci-style, which also checks the
workflow files and their shell snippets with actionlint (shellcheck is
preinstalled on the runner) and tests the CI selection and gate policy with
make test-ci-plan. Backend runs a static matrix entry for golangci-lint and vet,
plus four race matrix entries that cover the Go contract tests. Go builds runs
make go-build-check, the contracts without cgo and the Linux/Windows builds,
and Go container runs make go-container-check, the service image and its
read-only container smoke; Style already checks Go formatting. Backend, Go
builds and Go container each restore a Go compilation cache keyed by the Go
sources and save a new one when the sources change. Frontend runs make npm-audit-signatures on its fresh installation, then
make ci-frontend: the type check, the build and the frontend tests. API smoke
runs make smoke against the Go backend. Docker image runs make docker-build,
which compiles the Go backend and the frontend into the release image, with
Buildx cache arguments and then make production-smoke against that image.
Go audit runs make go-audit, and npm audit runs make npm-audit-production and
then make npm-audit.

`scripts/ci-plan.ts` owns the plan (make ci-plan) and the Gate's verdict (make
ci-results); its tests guard against accidental validation skips. In the `pr`
tier it compares the PR base with the checked-out merge result, including both
sides of renames and deleted paths:

| PR changes                                                                    | Planned jobs beside Plan, Style, and the Gate                       |
| ----------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| Only documentation, issue forms, `.env.example` or Compose files              | None                                                                |
| Go backend source, tests or SQL                                               | Backend, Go builds, Go container, API smoke, Docker image           |
| `backend/go.mod` or `backend/go.sum`                                          | Backend, Go builds, Go container, API smoke, Docker image, Go audit |
| The static seed                                                               | Backend, Go builds, Go container, Frontend, API smoke, Docker image |
| `backend/Dockerfile`                                                          | Go container                                                        |
| Frontend source, `public/` or frontend configuration                          | Frontend, Docker image                                              |
| `package.json` or `package-lock.json`                                         | Frontend, Docker image, npm audit                                   |
| Frontend tests                                                                | Frontend                                                            |
| `docker/` or `LICENSE`                                                        | Docker image                                                        |
| A validation script                                                           | The jobs that run or type-check it, listed in `scriptJobs`          |
| Makefile, `.nvmrc`, workflows, composite actions, the planner, or other paths | All validation jobs, no advisory audit                              |

Mixed changes use the union of planned jobs, and a missing or empty PR diff
selects all jobs, the audits included. The frontend build type-checks every
TypeScript script, so each one selects Frontend; `scriptJobs` names the other
jobs behind each script. A script missing from that table selects all
validation jobs, and the planner's tests fail until it is listed. The advisory
audits follow the dependency manifests alone, so a new advisory for an
unchanged dependency never fails an unrelated pull request. The `full` tier
ignores changed paths and always plans every validation job, so whatever a
pull request leaves out is validated on `main`. It plans no audit: the daily
Dependency audit covers `main`, so a new advisory never holds back a release.
A release reuses that result for its tagged commit.

Gate always evaluates the plan against the job results, even after failures.
It rejects failed, cancelled, missing, or unexpectedly skipped results and a
missing or invalid plan; only a job the plan disables may be skipped. A check
is named after the calling job and the validation job, so the ruleset for
`main` requires the single check `PR / Gate`; adding, splitting or renaming a
job never changes what the ruleset names. `Full / Gate` reports the same
verdict for a commit on `main`; the Release workflow reports its source and
same-commit result from `Verify release source and validation` without a
second validation Gate. Release grants write permissions only to publication
jobs. A new push cancels the superseded run of the same pull request or
branch, while Full keeps each main commit's result; release publication is
serialized per tag.

Every runner has a timeout, locked installation, and read-only repository
permissions. Checkout does not persist credentials. Caches speed installation
and image layers but never skip validation targets. Actions are pinned to
reviewed commit SHAs. Every Node job uses the local
`.github/actions/setup-node` action, which installs the version in `.nvmrc`;
that version and the Go version in backend/go.mod match the Docker build
toolchain.

`make ci-frontend` type-checks the frontend, scripts and tests before Vite
builds the browser bundle, then runs the frontend tests. Node executes script
and test `.ts` files directly; runtime execution alone does not check types.
No source files are excluded with `@ts-nocheck`.

The Dependency audit workflow runs daily at 03:17 UTC and on manual dispatch.
Its npm job runs make npm-audit-production and then make npm-audit, which read
only package-lock.json, then installs the locked tree and runs make
npm-audit-signatures. Its Go job runs make go-audit:
govulncheck, pinned by GOVULNCHECK_VERSION, reports vulnerabilities that the
backend actually reaches in its modules and in the standard library of the
go.mod toolchain that the release image also uses. A new advisory fails the
run without changing the repository. These targets query live advisory
databases, so they stay outside check and ci; in CI they gate only a pull
request that changes a dependency manifest, through Go audit and npm audit.
Both jobs run only in the upstream repository, so forks do not inherit the
schedule.

Dependabot (`.github/dependabot.yml`) proposes weekly grouped updates for
GitHub Actions, Go modules and npm packages after a seven-day cooldown. The
Node and Go toolchains and the Docker base images are upgraded by hand; see
[Dependency updates](../security.md#dependency-updates).

Issue forms in `.github/ISSUE_TEMPLATE/` collect bug reports and feature
requests with synthetic data, and route suspected vulnerabilities to private
security reporting as the [security policy](../../../.github/SECURITY.md) describes.
The Works correction form, which the Works page's feedback link opens,
collects one work's wrong or missing information with a source; a
correction lands in the reference collection and reaches the page with the
next packaging ([Works collection](../works-archive.md#packaging)).
