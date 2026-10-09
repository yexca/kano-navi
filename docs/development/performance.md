# Synthetic API and Page Performance

## Scope and Conditions

Measured on 2026-10-09, with Node 24.19.0 on the same Windows workstation and
loopback HTTP listener. These are local synthetic snapshot results, not
production latency, capacity or SLA measurements. Machine load, garbage
collection and run order affect individual milliseconds and tails.

`scripts/benchmark-api.ts` constructs deterministic in-memory SQLite fixtures:

| Fixture |  Posts | Events | Media rows | Reviewed schedule images |
| ------- | -----: | -----: | ---------: | -----------------------: |
| small   |    100 |     20 |         40 |                        2 |
| large   | 20,000 |  4,000 |     10,000 |                      150 |

The large fixture includes historical weekly images and their source posts,
image verdicts, historical schedules, 20 posts in the requested feed window,
and pending media metadata. The dates/IDs/text/URLs are synthetic and identical
before and after. Pending media requires no download or runtime file access;
these results do not measure the cost of verifying 10,000 ready files. The
database starts without seed/provider/workflow configuration. The admin image
read uses explicit development mode; production authentication is tested
separately. There are no source/model calls, workflow timers or operator data.

Each fixture has a separate server process and a separate HTTP client process.
Each endpoint/encoding has five warmup requests, 50 serial samples, and 60
samples in 15 batches of four concurrent requests. Median is the middle ordered
sample, P95 is the nearest-rank 95th percentile. Latency includes receiving the
body and decoding it on the client, excluding fixture startup. Sizes count
response body bytes, excluding headers. Both passes use the same requests:

- `/api/dashboard?days=3`
- `/api/dashboard/revision?since=0`
- `/api/admin/schedule-assets?limit=200`

The baseline was collected before changing application code. The final pass
was collected after builds/tests finished; no build ran concurrently with either
recorded pass. An additional exploratory pass was discarded. The script prints
JSON to stdout and does not write a database or benchmark log into the repository.
Build first with `make ci-frontend`, then run
`node scripts/benchmark-api.ts measurement`. Compare matched fixture, route,
encoding and concurrency rows across revisions; do not turn these times into
absolute CI thresholds.

## Query Plan and API Results

The source-wording query is still:

```sql
SELECT COALESCE(json_extract(raw_json, '$.search_text'), text)
FROM posts WHERE url = ? ORDER BY published_at DESC LIMIT 1;
```

Before: `SCAN posts`, `USE TEMP B-TREE FOR ORDER BY`.
After: `SEARCH posts USING INDEX posts_url_published_at_idx (url=?)`, with no
temporary sort. The additive startup index is `posts(url, published_at DESC)`.
The upgrade test compares stored records/configuration before/after reopening,
then checks the latest quoted source wording, manual rejection/approval, locks
and tombstones. Source text gates and image-review semantics are unchanged.

Times below are **median / P95 in milliseconds**. `Accept` is the request's
`Accept-Encoding`; before optimization even `gzip` requests returned identity.

| Fixture | API (under `/api/`)   | Accept   | Concurrency |         Before ms |        After ms | Body bytes before → after |
| ------- | --------------------- | -------- | ----------: | ----------------: | --------------: | ------------------------: |
| small   | dashboard             | identity |           1 |       1.95 / 2.77 |     1.15 / 1.56 |           21,469 → 21,469 |
| small   | dashboard             | identity |           4 |       4.68 / 8.51 |     2.80 / 5.91 |           21,469 → 21,469 |
| small   | dashboard/revision    | identity |           1 |       0.43 / 0.58 |     0.28 / 0.36 |                   30 → 30 |
| small   | dashboard/revision    | identity |           4 |       0.84 / 1.20 |     0.60 / 0.84 |                   30 → 30 |
| small   | admin/schedule-assets | identity |           1 |       0.72 / 0.86 |     0.44 / 0.61 |             1,470 → 1,470 |
| small   | admin/schedule-assets | identity |           4 |       1.48 / 2.31 |     1.00 / 1.37 |             1,470 → 1,470 |
| small   | dashboard             | gzip     |           1 |       1.78 / 2.00 |     1.22 / 1.58 |            21,469 → 1,451 |
| small   | dashboard             | gzip     |           4 |      4.23 / 12.28 |     3.84 / 8.77 |            21,469 → 1,450 |
| small   | dashboard/revision    | gzip     |           1 |       0.37 / 0.52 |     0.24 / 0.43 |                   30 → 30 |
| small   | dashboard/revision    | gzip     |           4 |       0.63 / 1.03 |     0.38 / 0.60 |                   30 → 30 |
| small   | admin/schedule-assets | gzip     |           1 |       0.60 / 0.71 |     0.39 / 0.54 |             1,470 → 1,470 |
| small   | admin/schedule-assets | gzip     |           4 |       1.41 / 2.16 |     0.76 / 1.18 |             1,470 → 1,470 |
| large   | dashboard             | identity |           1 |   164.10 / 175.31 |   46.45 / 51.22 |     1,883,283 → 1,883,283 |
| large   | dashboard             | identity |           4 |   487.41 / 817.12 | 135.75 / 194.08 |     1,883,283 → 1,883,283 |
| large   | dashboard/revision    | identity |           1 |       0.34 / 0.48 |     0.36 / 0.57 |                   30 → 30 |
| large   | dashboard/revision    | identity |           4 |       0.53 / 0.74 |     0.69 / 1.05 |                   30 → 30 |
| large   | admin/schedule-assets | identity |           1 |   134.06 / 294.61 |     4.17 / 5.68 |         110,122 → 110,122 |
| large   | admin/schedule-assets | identity |           4 | 572.52 / 1,049.02 |   11.81 / 51.62 |         110,122 → 110,122 |
| large   | dashboard             | gzip     |           1 |   258.94 / 305.18 |   53.43 / 69.76 |        1,883,283 → 46,718 |
| large   | dashboard             | gzip     |           4 | 679.06 / 1,512.53 | 196.12 / 276.87 |        1,883,283 → 46,718 |
| large   | dashboard/revision    | gzip     |           1 |       0.38 / 0.74 |     0.32 / 0.62 |                   30 → 30 |
| large   | dashboard/revision    | gzip     |           4 |       0.55 / 0.87 |     0.54 / 0.91 |                   30 → 30 |
| large   | admin/schedule-assets | gzip     |           1 |   153.49 / 250.52 |     3.95 / 6.05 |         110,122 → 110,122 |
| large   | admin/schedule-assets | gzip     |           4 |   383.58 / 542.18 |   11.36 / 41.88 |         110,122 → 110,122 |

The dashboard identity median improves about 72% in the large fixture; the
admin image median improves about 97%. Public gzip transfer drops about 97.5%.
Small gzip sizes vary by a byte because `meta.generatedAt` changes each read.

| Response            | Before Content-Encoding / Vary | After Content-Encoding / Vary (gzip request) | Cache-Control after                 |
| ------------------- | ------------------------------ | -------------------------------------------- | ----------------------------------- |
| Dashboard           | absent / absent                | gzip / Accept-Encoding                       | no-store                            |
| Revision (30 bytes) | absent / absent                | absent / Accept-Encoding                     | no-store                            |
| Admin images        | absent / absent                | absent / absent                              | no-store, no-transform              |
| Hashed JS/CSS       | absent / absent                | gzip / Accept-Encoding                       | public, max-age=31536000, immutable |

Identity requests have no Content-Encoding. Eligible responses still vary by
Accept-Encoding. Admin stays uncompressed even for its 110 KB body because it
belongs to the credential/operator boundary. MCP additionally retains
`Vary: Authorization` and sends `no-store, no-transform`. HTTP regression tests
also cover Brotli, encoding quality/zero values, no advertised encoding,
decoded equality, ETag/304/Last-Modified, HEAD, ranges, media and fixed assets.

## Build and Actual Homepage Resources

The locked React/React DOM versions remain 19.2.8 in both builds. Vite's build
summary estimates homepage gzip at 203.98 KB before and 125.26 KB after.
The table below uses exact file bytes and Node's default gzip for both passes;
its totals can differ slightly from Vite's estimates.

| Homepage resource | Before raw / gzip bytes | After raw / gzip bytes |
| ----------------- | ----------------------: | ---------------------: |
| JavaScript entry  |       671,529 / 202,861 |      387,477 / 124,132 |
| CSS entry         |        123,205 / 22,268 |        53,073 / 11,056 |

Before, homepage HTML references `index-CdpNiP8f.js` and `index-CMSrP0-B.css`.
Direct HTTP requests with `Accept-Encoding: gzip` actually transferred all
671,529 and 123,205 identity bytes and returned `public, max-age=0`.
After, the browser's server-side request inventory confirms homepage loads only
`index-9PDZHo1y.js`, `index-CBr5l6-t.css` and the existing local avatar/banner,
plus its local dashboard/revision reads. No admin/history/about chunks load on
the homepage. Direct gzip transfer is now 124,132 and 11,056 bytes respectively,
with Accept-Encoding variation and immutable caching. Hashed filenames identify
this measurement's build and change with subsequent source/build changes.

Other pages request their own modules, copy and CSS; the principal raw JS sizes
are about 109.75 KB (admin), 101.24 KB (history) and 5.45 KB (about). Their copy
chunks are about 51.40, 14.00 and 6.96 KB. Small shared icon chunks are requested
only when needed. Across all pages there is about 677.25 KB raw JS after
splitting, versus 671.53 KB before: the improvement concerns initial page work,
not reducing the total code needed to visit every page. Shared settings still
carry the homepage code; fully splitting the homepage from static-page visitors
is a possible later improvement.

## Browser and Regression Review

The production bundle was served by `--browser small` against a disposable
in-memory snapshot. Stdin controls only simulate snapshot failures/delays,
chunk failures/delays and revisions; there is no fixture-control HTTP route.
Reviewed all four pages at 1440×1000 and 390×844, including direct visits and
refresh. History's local archive remained intact. Theme/language survive page
loads; lazy statuses and reload-on-error use the current locale/theme.

The review exercised initial API failure with automatic recovery, a later manual
refresh failure preserving 20 known posts, lazy page loading, failed chunks and
successful retry. The server's dashboard request count stayed unchanged while
visiting history/about, including about's chunk failure and retry. Those pages
requested only local build/static files. Workflow timers and tasks remained
disabled/unrun. Hook regressions additionally exercise fake-time backoff,
single-flight deduplication, stale revision/JSON completions, hidden/visible
coalescing and unmount cancellation.

Validation for this change uses `make ci-backend`, `make ci-frontend`,
`make smoke`, `make docker-build`, `make production-smoke`,
`make sensitive-check` and `make docs-check`. Backend includes all 220 tests,
including existing sanitized HTTP/MCP, manual precedence, Featured, mixed ISO
offset and local time-transition regressions. Production smoke uses its fresh
anonymous volume and synthetic authentication. Browser review uses the in-app
Chromium browser; other engines, real deployment/proxy traffic, real ready-media
loads, WAN throughput and production synchronization were not measured.

## Remaining Work and Evidence

- The measurements above predate the bounded snapshot/calendar repair merged
  afterwards. That repair limits homepage previews and focus candidates, resolves
  only displayed media and adds indexed video/event associations; full calendar
  history is available through bounded pages. The measured 1.88 MB response and
  135.75 ms concurrent median describe the earlier revision, rather than the current
  response contract. Remeasure matching fixtures before applying those timings to
  the combined implementation. First-query cost is tested without a response cache.
- Pending media avoids ready-file checking. Profile actual ready-media syscall
  and hash/path-validation cost separately before considering a media-state
  verification redesign; retain its containment/version protections.
- Compression saves transfer while adding CPU. Large gzip median is 53.43 ms
  versus 46.45 ms identity, and four-way gzip is 196.12 ms. Measure representative
  deployment bandwidth/CPU before choosing other compression levels or
  precompressed build assets. Authentication and reflected inputs must remain
  excluded (see the [compression middleware](https://github.com/expressjs/compression)).
- The homepage still loads React/Radix/UI and the shared base translations.
  Static pages also receive that shared entry, and admin still loads its views
  together. Further route/shared-library or admin-view splitting needs separate
  waterfall measurements; this round preserves their data-loading architecture.

[Testing](testing.md) · [Backend transfer contract](../architecture/backend.md#text-transfer-and-static-caching)
