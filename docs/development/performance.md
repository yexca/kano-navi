# Synthetic API and Page Performance

Performance work measures synthetic local fixtures and protects bounded
contracts with tests. No timing threshold enters CI: machine load, garbage
collection and run order move individual milliseconds and tails, so timings
are compared, not asserted.

## Backend Benchmark

The Go store keeps a synthetic 10,000-record snapshot benchmark. Run it from
`backend-go/`:

```bash
go test ./internal/store/sqlite -run '^$' -bench BenchmarkDashboardSynthetic10k
```

The fixture in
`backend-go/internal/store/sqlite/dashboard_benchmark_test.go` writes 10,000
synthetic posts, events and videos into a temporary snapshot and times the
three-day dashboard projection. It uses no seed, provider or workflow
configuration, no source or model calls and no operator data, and writes
nothing into the repository.

## Contracts the Measurements Check

- `TestDashboardSynthetic10kCollectionsStayBounded`, on the same fixture,
  keeps the dashboard payload bounded (at most 100 posts, 130 events and 61
  videos) while its post and event counts still report the full totals. Full
  calendar and post history is available through bounded pages; see
  [Bounded public archives](../architecture/public-api.md#bounded-public-archives).
- The latest source wording of a schedule image is read through the
  `posts_url_published_at_idx` index on `posts(url, published_at DESC)`,
  without a temporary sort. Startup adds the index to older snapshots, and
  the schema upgrade tests check that it exists.
- Public dashboard, revision and health JSON and the build's text files are
  gzip-compressed; admin, authentication, MCP and runtime media are not.
  Hashed build files are immutable and everything else revalidates. HTTP
  tests cover encoding negotiation, validators, 304, HEAD and ranges. The
  rules are in the
  [backend transfer contract](../architecture/backend.md#text-transfer-and-static-caching).
- The homepage loads only its entry chunk and stylesheet, the local avatar
  and banner, and its local dashboard and revision reads. `/admin`,
  `/history` and `/about` and their translations are lazy chunks requested
  only by their own pages (`src/main.tsx`).

## Measuring a Change

Run the benchmark on the base and the changed revision on the same machine,
with no build running concurrently, and compare matched results. For page
weight, run `make ci-frontend` and compare the chunk sizes Vite reports for
the homepage entry and each lazy page. For request behavior, serve the
production build against a disposable development backend (see
[Testing](testing.md#test-scope)) and review all four pages at desktop and
mobile widths, including direct visits, refresh, a failed snapshot read with
automatic recovery and a failed lazy chunk with retry. Visiting `/history` or
`/about` must not issue dashboard reads.

Report results as local synthetic measurements, not production latency,
capacity or SLA figures, and do not record them in this document.

[Testing](testing.md) · [Backend transfer contract](../architecture/backend.md#text-transfer-and-static-caching)
