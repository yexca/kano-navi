# Go public-read cutover runbook

## Current state and ownership

> Historical record. The migration is complete: releases run the Go backend
> described in [Go backend](../architecture/go-standalone.md). This page
> documents the earlier read-only canary rehearsal. Its scripts and the
> TypeScript service were removed afterwards.

At the time of this rehearsal, production traffic remained on the TypeScript
service at `:7657`. Go is a
read-only side-by-side/canary process and is not in the production request
path. It opens a maintained SQLite snapshot with SQLite read-only mode and
reads runtime media without downloading or replacing files.

The future public-read ownership boundary is:

- `/api/health`
- `/api/dashboard/revision`
- `/api/dashboard`
- `/api/calendar`
- `/media`

TypeScript continues to own the admin frontend, authentication in production,
all admin mutation, MCP control, source synchronization, workflow scheduling,
LLM routing and extraction, and media discovery, download, upload and cleanup.
Go has a separate in-memory admin session only for side-by-side GET/read
validation; it is not connected to the frontend and no write or operator
mutation route is part of this phase.

## Admin side-by-side boundary

The Go admin contract mirrors the existing TypeScript paths for session,
login, logout, posts/LLM, videos, events and sync runs. Go also provides
read-only activity/overview aggregates for diagnostics. List queries use
bounded `page`/`pageSize` values (events retain the existing event page shape)
and bind every filter value. Event projections retain manual lock, tombstone,
cancellation and provenance fields. LLM errors are returned as stable
categories; run diagnostics omit raw JSON, stacks, absolute paths, secrets and
provider material.

The side-by-side session stores never share a cookie token or SQLite secret.
Before production admin ownership changes, select the owner, verify the
cookie and failure contract against TypeScript with synthetic fixtures, and
prepare a rollback that disables the Go admin listener and returns `/api/admin`
to TypeScript without changing the snapshot.

## Validation gate

Run `make public-read-gate` from a clean, dependency-installed checkout. The
ordered gate runs `make install`, TypeScript server checks, the complete
dashboard JSON parity comparison, side-by-side HTTP checks, Go native and
container checks, the sensitive scan, and documentation link checks. Both
comparison scripts exit with `SKIPPED` status 2 when local `node_modules` is
absent; this is useful for a dependency-free local Go check but cannot pass a
CI job because the backend job installs the lockfile first.

The side-by-side check uses only synthetic SQLite rows and synthetic image
bytes. It compares status, sanitized JSON or complete media bytes, and the
public content/cache/validator headers for health, revision, dashboard,
calendar, ready media, selected profile media, pending media, stable 404s and
method errors. It normalizes only `health.now` and
`dashboard.meta.generatedAt`. The request and rollback portions hash the
synthetic database, SQLite sidecars and fixture media before and after reads.

For repeatable resource measurements, run:

```text
node scripts/public-read-benchmark.mjs --output <path outside the repository>
```

The benchmark creates 10,000 synthetic posts, events and videos, reports the
first request, median and P95 at concurrency 1 and 4, decoded response sizes,
and best-effort RSS observations. It records measurements without imposing a
machine-specific CI threshold. The command and its output should be retained
with the deployment review.

The dependency-free Go store benchmark from this validation run completed two
iterations at 521.7–551.9 ms/op, 88.13 MB/op and 1,883,952–1,883,958
allocations/op on the Windows amd64 host. The cross-service HTTP benchmark is
available through the command above and requires the locked Node dependencies;
it remains a required CI measurement when the public-read gate is run after
`make install`.

## Deployment rehearsal

`make go-check` builds the independent scratch Go image and runs
`scripts/go-docker-smoke.mjs`. The Go validation job then runs
`make public-read-deployment-smoke`, which builds the existing TypeScript
production image, starts both images on different ephemeral host
ports against copies of the same synthetic snapshot, checks health, dashboard,
calendar, ready media and profile media, compares public responses, and
restarts both containers. The Go mount is read-only in CI; the rehearsal
includes committed WAL/SHM sidecars so SQLite does not create them during a
read. It verifies that the SQLite snapshot, WAL/SHM/journal entries and media
tree retain their hashes and that no unexpected files are created. The
default production Compose file is unchanged; this is a test-only image
exercise.

## Planned routing and dependency controls

When a cutover is approved, route the five public-read families through a
reverse proxy or have the TypeScript front door explicitly proxy them to the
Go listener. Any TypeScript-to-Go outbound proxy must be configured with an
explicit loopback target address and port, a connection and response deadline,
a maximum response size, no redirects, fixed public error mappings, and a
documented fallback. It must never fall back automatically to a remote CDN.
Admin, MCP control, synchronization, workflows, LLM calls and download paths
must remain local to TypeScript.

Before enabling traffic, run the parity and side-by-side checks over the
maintained snapshot, review the benchmark output and request-log privacy,
confirm the deployment-specific container startup, and rehearse rollback.

## Rollback

1. Stop the Go canary or disable the proxy/proxy target.
2. Keep the TypeScript service serving `:7657`.
3. Check `/api/health`, `/api/dashboard`, `/api/calendar` and representative
   ready, profile and unready media reads.
4. Confirm the SQLite snapshot and runtime media hashes are unchanged.

There is no schema rollback in this phase. A failed Go read keeps the known
TypeScript snapshot and does not trigger a CDN fallback. Production traffic is
not switched by the phase-six validation or deployment rehearsal.
