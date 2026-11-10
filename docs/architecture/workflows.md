# Workflows and Synchronization

## Manual Fetches and Historical Backfill

The console's Sources page starts manual fetches of X, the related X
accounts, YouTube, the Bilibili uploads and dynamics, and the official shop;
it is the one place to start them. The Posts and Videos pages open on their lists and
link to it from their header. A fetch names one listing method first, then
what the method reads. `POST /api/admin/runs` with
`{ "kind": "fetch", "source": "x", "method": "timeline", "window": { "mode": "count", "count": 50 } }`
starts one; the [Admin API](admin-api.md#runs) describes the body and its
options.

| Source            | Method     | Needs                                    | Modes                              | Listing                                                  |
| ----------------- | ---------- | ---------------------------------------- | ---------------------------------- | -------------------------------------------------------- |
| X                 | `timeline` |                                          | `count` (1–200), `all`             | public FxTwitter timeline, 20 statuses a page            |
| X                 | `api`      | `X_API_BEARER_TOKEN`                     | `count` (1–200), `range`, `before` | full-archive search, 100 posts a page                    |
| X related         | `timeline` |                                          | `incremental`                      | one related account's FxTwitter timeline                 |
| YouTube           | `api`      | `YOUTUBE_API_KEY`                        | `count` (1–200), `all`             | uploads playlist, 50 entries and one quota unit a page   |
| YouTube           | `rss`      |                                          | `count` (1–15)                     | public feed, its newest 15 uploads in one request        |
| YouTube           | `ytdlp`    | `YTDLP_BINARY`                           | `count` (1–200), `all`             | configured yt-dlp upload listing, without media download |
| Bilibili uploads  | `space`    |                                          | `count` (1–200), `all`             | the account's upload listing, 20 entries a page          |
| Bilibili dynamics | `space`    | `BILIBILI_COOKIE` or `BILIBILI_SESSDATA` | `count` (1–200), `all`             | the account's dynamics listing, about 12 items a page    |
| Shop              | `catalog`  |                                          | `all`                              | the whole product catalog, at most 6 pages of 250        |

An X fetch reads every account of `X_HANDLES`, or the one its selection's
`account` names; a one-account fetch takes the whole X request budget and
leaves the rotation that spreads the budget across all-account fetches as
it is. An `x_related` fetch names one related account and is the
[`x_related` step](sources.md#related-accounts) for that account alone:
its timer's incremental read from the account's cursor, bounded by the
`x_related` schedule's `max`, through the FxTwitter timeline only. It fails
once, without a retry, with the account's failure code, or with
`x_related_account_unknown` when the account was removed while the fetch
waited in the queue.

- `count` reads the newest `count` records per X account, for the channel or
  for the Bilibili account, stored or not, whatever their age.
- The shop's `catalog` fetch is the `shop` step alone, with no option. It
  reads the sale periods that the timer's newest-50 window never listed and
  writes them as every [shop read](sources.md#official-shop) does; it waits
  out a 429's `Retry-After` like the timer, and keeps no checkpoint.
- `all` pages the listing to its end.
- `range` reads `startDate` to `endDate`, at most 365 days; both dates are
  inclusive in `Asia/Tokyo`, and the internal bounds are start-inclusive and
  end-exclusive.
- `before` reads `days` (1–365) before the oldest persisted post of each
  account, rather than before a UI page's limited list. Accounts without
  posts are skipped and reported while the others backfill; with no posts at
  all, fetch a date range first.

The method IDs are stable, since runs freeze them. `GET /api/admin/sources`
describes each source's methods: their modes, count bound, page size, the variable a method needs
(never its value) and whether it can run now.

A manual fetch never falls back. A method that cannot run now, because its
credential or adapter is missing, is refused with a validation message, and so
is a mode or count the method does not serve. A method that fails fails the
run with its controlled code: unlike a timed source step, the fetch step's
failure is not tolerated, so the steps after it do not run.

The options add steps or change what is written:

- `extract` (X, related-account and Bilibili dynamics methods) appends the `schedule` step. Schedule extraction picks
  its candidates among all stored X posts by its keyword and message rules,
  unattempted posts first (see
  [Schedule extraction](sources.md#schedule-extraction-pipeline)), so fetched
  posts that match join that queue whichever run extracts them: this run with
  `extract`, otherwise the X chain's next `schedule` step. Posts the rules do
  not select are not extracted either way.
- `translate` appends the `translate` step.
- `refresh` (YouTube methods) rewrites the videos the listing names that are
  already stored: their title (keeping a revision), publication time and URL.
  The listing leaves their reservation state (kind, upcoming flag, scheduled
  time) and stored thumbnail alone; the `api` method then checks their details
  as it does for new videos (see [YouTube methods](#youtube-methods)). Without
  it, stored videos are left as they are.

X fetches only fill gaps: a post that is already stored keeps the text, media
and schedule candidate it has, so extraction fingerprints do not change.
Bilibili fetches write every listed record as the timed reads do, since a
listing describes the whole upload or dynamic, so they take no `refresh`
option either.

Each fetch step reports `listed`, the records the selection took from the
listing, split into `new`, `stored` (already stored, refreshed or not) and
`failed` (listed without usable content); `pages`, the requests it made;
`reachedEnd`, false when the page budget or a failed later page stopped it
before the end of what it asked for, in which case repeating the fetch
continues it; and `items`, the first 20 new records as `{id, title, account}`
(an X post's first line of text, cut to 80 characters, or a video's title).
An X result also lists the same counts per account under `accounts`, with
`scheduleAssets` and `discovery` (`timeline` or `api`); an `api` result adds
`apiRead`, the posts its answers returned. A YouTube `api` result
adds `inspected`, the videos whose details it checked, and `apiRequests`, its
Data API calls (quota units). Every YouTube result also adds `members` and
`membersFrom` from the channel's members-only playlist, which a YouTube fetch
reads after its pages as the recent step does (see
[Video availability](sources.md#video-availability)). The Sources page shows
the newest fetch of the source under its form.

The run freezes the validated selection (method, mode, its count, days or
dates, and `refresh`) in `workflow_runs.fetch_windows_json`, so a recovered or
retried run never changes it. A run frozen without a method, or with a mode no
method serves, such as a `recent` window or a YouTube date window, reads in the
run history as it was stored; running it again fails its fetch step with
`this fetch uses a retired selection; start a new fetch`.

These requests run the chosen source and media stages, with the steps their
options add, through the single-flight queue; opening a page does not fetch
platforms. They never read or move the incremental cursors, the pending queue
or timeline gaps. Schedule-image candidates on fetched X pages are registered
with each page (see [Schedule images](sources.md#schedule-images)). Source
schedules, Run now, YouTube pushes and one-off step runs carry no window: they
read with the schedule's frozen sync setting (see
[Source schedules](#source-schedules)), whatever the credentials. A full sync
or scan carries neither and stays incremental.

### X timeline method

Timeline fetches page the public FxTwitter timeline
(`api.fxtwitter.com/2/profile/<handle>/statuses`) newest first. FxTwitter
lists 20 statuses a page whatever `count` a request names, so a `count` of
200 takes ten pages, more than the default
[page budget](#budgets-and-checkpoints); such a fetch stops with
`history_page_budget_reached` and continues when repeated. Each status is stored from the timeline page itself, without
vxtwitter detail requests or the incremental pending queue. A `count` fetch
stops after the account's newest `count` own statuses, and an `all` fetch at
the end of the timeline. Reposts are skipped and replies are not listed. A
status the timeline lists without usable content is counted as failed and
reported as `<id>: x_invalid_response`; a timeline that ends before a `count`
fetch's count reports `x_timeline_end`. Timeline pages use the X source
client's protections: a 10-second timeout, a 4 MiB response limit, rejected
redirects and controlled `x_*` error codes.

### X API method

`backend/internal/source/history` calls only
`https://api.x.com/2/tweets/search/all`
([X full-archive search](https://docs.x.com/x-api/posts/search/quickstart/full-archive-search),
which needs full-archive access). The environment-only credential travels in
the `Authorization: Bearer` header, never in URLs, SQLite or results. Each
request has the request timeout and pages wait the request spacing apart (see
[sync settings](../operations/configuration.md#sync-settings)), and redirects are rejected so the
credential header never follows one. Each response has a fixed 4 MiB byte
limit: the adapter checks `Content-Length` and counts streamed bytes,
cancelling an oversized body before parsing or writing its page. Posts are
mapped as the X chain's API method maps them, quoted statuses included (see
[X API in the chain](sources.md#x-api-in-the-chain)), and the posts a search
returns count toward the day's X API usage; a manual fetch ignores the
chain's gate and daily cap. Searches end at least ten seconds before the
present. Without a start time the endpoint
covers only 30 days, so a `count` search starts at the Snowflake epoch
(November 2010), where status IDs begin; it asks for the posts it still needs,
10 to 100 a page, and stops at the count. X bills every post a search reads.

Failures reach run history only as controlled codes:
`source_api_http_<status>`, `source_api_request_failed`,
`source_api_response_too_large`, `source_api_invalid_response`,
`source_api_invalid_records` and `source_api_repeated_page`, never upstream
bodies or credentials. A fetch that stops at its page budget reports
`history_page_budget_reached; repeat this fetch to continue`. Source access,
quota and search coverage depend on the platform; this feature does not
promise a complete export of its archive.

### YouTube methods

The API and RSS methods read through the YouTube source client, with its 10-second
timeout, 4 MiB response limit, rejected redirects and controlled codes. The
`api` method pages the channel's uploads playlist (`playlistItems.list` on the
`UU` playlist, the key only in the `X-Goog-Api-Key` header), newest first; an
entry that a new upload shifts onto the next page is listed once. The `rss`
method reads the public feed once, with the feed's retries after an upstream
5xx or connection failure (see [YouTube](sources.md#youtube)). Failures record
`youtube_api_*` or `youtube_feed_*` codes.

The `ytdlp` method requires `YTDLP_BINARY` and runs the configured executable
with `--flat-playlist --dump-single-json --skip-download`. It reads the newest
channel entries, maps the metadata yt-dlp provides onto the stored video fields,
and never downloads media. A `count` fetch is capped at 200 entries; an `all`
fetch uses the same bounded 200-entry request and reports more work when the
executable returns exactly 200 entries. It records `youtube_ytdlp_*` codes.
The executable follows the YouTube
[proxy route](outbound-proxy.md#yt-dlp).

Videos are written as the listing describes them: title, publication time,
largest thumbnail and an `unknown` kind, since a listing cannot tell a
broadcast from an upload. The `api` method then checks
the details of each page's videos that are not stored yet, and of the stored
ones too with `refresh`, through one `videos.list` call per 50 (see
[Data API](sources.md#data-api)). It follows the recent step's rules: the
classification, the per-format playlists for an ended broadcast without a
stored premiere verdict, localized titles, and
the recent step's upsert of videos and reservation events, so manual locks
and deletion tombstones hold. It reads no watch page, so an ended broadcast
the answer cannot classify is written as the API describes it (an
[unclassified answer](sources.md#data-api), which creates no event), and a
video the API leaves undecided keeps its listed row. The answers commit with
their page and its checkpoint. A failed details call records its
`youtube_api_*` code and leaves the step `partial`; the page's videos stay as
listed and the fetch goes on.

A `count` fetch reads the newest page of each format playlist, as the recent
step does, at most once per details call. An `all` fetch reaches broadcasts
far below those pages, so it pages both playlists to their end instead, at
most 20 pages (1,000 entries) each: once per run, the first time a page's
details name an ended broadcast that needs a verdict, keeping the two
membership sets in memory for the rest of the run. A video on the live list
is a stream and one on the long-form list a premiere; one seen on both, or on
neither, gets no verdict. A list that the cap or a failed page cuts short
records its code (`youtube_format_list_incomplete`, or the page's
`youtube_api_*` code) and names only what it read, so a broadcast it did not
reach gets no verdict. An `all` fetch therefore spends about two quota units
per 50 new uploads (a playlist page and a details call), plus one unit per 50
entries of each format playlist once per run when it holds an ended broadcast.

The `rss` method only lists: its 15 uploads are within the recent step's own
listing, which checks their details. Neither method reads the streams page,
watch pages or the detail queue, and neither writes incremental state. The
`ytdlp` method also writes only the listed video rows and does not advance the
timed source cursor.

### Bilibili methods

The `space` method of the uploads pages
`/x/series/recArchivesByKeywords` by page number, and that of the dynamics
pages `/x/polymer/web-dynamic/v1/feed/space` by the offset each page
answers, both newest first, through the Bilibili client the timers read with
(see [Bilibili](sources.md#bilibili)). Pages are 15 seconds apart, far wider
than the other sources' request spacing, because Bilibili answers quick runs
of requests with a captcha. The dynamics fetch needs the login cookie and
checks it and the WBI keys as the timed listing does when they are due; it
fails with `bilibili_login_required` without a cookie and
`bilibili_login_expired` while the latest check found it expired. Each page
keeps only the account's own records, as the timed reads keep them.

A fetch shares the timers' captcha backoff and is never forced: while the
backoff lasts it fails at once with `bilibili_blocked` and makes no request,
and the Sources page disables its form. A captcha met on the first page
fails the fetch with its `bilibili_risk_control_*` code and starts the
backoff; one met later keeps the committed pages, records the code and
stops. A successful page lifts the backoff and records the read's success.
A fetch ignores `BILIBILI_MIN_INTERVAL_MINUTES` and leaves the timers'
attempt clocks alone, so it never delays or advances a timed read. The
upload read's newest-upload cursor only moves forward.

### Budgets and checkpoints

The manual fetch pages setting bounds the pages of one fetch per X account,
for the channel or for a Bilibili listing (4 by default, 1–20), for every
paging method. X fetches
share the X status requests per run across accounts as a page budget, including failed
requests: each account gets an even share of what is left, and the first
account rotates across runs per mode (`sync_state` source `x-budget`, key
`window-<mode>`), so a small budget still reaches every account. An account
left without budget reports `X request budget exhausted`. A `before` fetch
skips an account with no stored posts. The step fails (`X fetch failed for
every account`) when no account's fetch answered, so skipped accounts never
turn a failure of every attempted account into a partial result.

Each successful page and its continuation checkpoint commit in one
transaction with the page's posts or videos and schedule candidates. The
checkpoint is a `sync_state` row keyed by account and a hash of the selection
(mode with its count or dates), under the method's own source:
`x-timeline-window` for timeline cursors, `x-window` for X API next tokens,
`youtube-window` for playlist page tokens, `bilibili-window` for upload page
numbers and `bilibili_dynamics-window` for dynamics offsets, each Bilibili
page in one transaction with the account state as well. The RSS method reads
one request and keeps no checkpoint. Repeating an interrupted fetch resumes its next page; a
completed one starts again at the newest page. When a resumed cursor or token
fails before any page of the fetch is committed, the fetch starts again from
the newest page, in the same run if the budget allows. A continuation the
source rejects is cleared from its checkpoint at once, so the next fetch
begins at the newest page even when this one had no page left: any failed
timeline cursor, an X API `next_token` answered with HTTP 400, a
playlist token answered with an HTTP error or rejected as malformed, and a
Bilibili page number or offset whose page failed with anything but a
captcha, the backoff or a login problem. Other
API answers, such as rate limits, quota and outages, keep the stored
continuation. `before` runs finish a pending interval (`pendingWindow`) before
moving farther back, using `x-backfill` search boundaries. A completed empty
interval also moves this boundary backward, preventing an endless retry over
the same empty month. Failures retain all records and the last successful
checkpoint.

### Why source schedules and read modes are separate

Each source has its own cadence because X, YouTube, Bilibili, related X accounts, and the shop have different freshness, quota, and access constraints. The single-flight queue remains shared so source writes are serialized and one process owns the timers. A schedule's incremental cap limits newly discovered records, while count mode intentionally re-reads a bounded newest window; timeline gaps preserve continuity when a capped or interrupted read cannot reach its cursor.

Listing method chains are configurable for X and YouTube because a public fallback can keep a source readable during an upstream outage, while credentials and quota remain optional. The chain only covers listing methods. Detail and helper requests keep their own bounded behavior so a failed later page does not discard records already read.

### Source schedules

Each source has its own timer
([source schedules](../architecture/workflows.md#source-schedules)), stored in
the additive `source_schedules` table and edited on the console's Sources
page. A fresh installation keeps all six disabled.

| Source              | Interval (default, bounds)                        | Fixed chain                                           |
| ------------------- | ------------------------------------------------- | ----------------------------------------------------- |
| `x`                 | 30 min, 15 min – 7 days                           | `x`, `media`, `schedule`, `translate`                 |
| `x_related`         | 180 min, 15 min – 7 days                          | `x_related`, `media`, `schedule`, `translate`         |
| `youtube`           | 60 min, 15 min – 7 days                           | `youtube`, `media`, `translate`                       |
| `bilibili`          | 180 min, `BILIBILI_MIN_INTERVAL_MINUTES` – 7 days | `bilibili`, `media`, `translate`                      |
| `bilibili_dynamics` | 180 min, `BILIBILI_MIN_INTERVAL_MINUTES` – 7 days | `bilibili_dynamics`, `media`, `schedule`, `translate` |
| `shop`              | 360 min, 180 min – 7 days                         | `shop`                                                |

X has two timers because its two reads serve different purposes: `x`
catches Kano's own posts quickly, while `x_related` reads the
[related accounts](sources.md#related-accounts), whose announcements land
days ahead of the event, at a slower cadence. Both reach FxTwitter, so the
single-flight queue keeps their runs apart, and a failure of one leaves the
other reading. The related accounts' chain carries `media`, `schedule` and
`translate` so a status the read promotes is processed in the same run.

Bilibili has two timers because its two reads serve different purposes:
the uploads are an archive, like YouTube's, while the dynamics announce
schedules, like X posts, and need the login cookie. Each timer has its own
interval, runs and Run now; both share the client, device identity and
captcha backoff, so a captcha met by either read holds both, and their
requests to the same address add up. The
[official shop](sources.md#official-shop) writes its sale periods itself, so
its chain has no media, extraction or translation step; its listing changes
a few times a month, hence the long default.

When a schedule is due, the server process submits its chain as the ad hoc
run `@source-<source>` (`reconcile` is appended as for every run with a
source step), labelled `source-schedule` in the run history. **Start now**
on the console overview's queue, and **Run now** in a source's Sources page
summary while its timer runs, start the timer
(`POST /api/admin/sources/{source}/schedule/start`): the schedule becomes due
at once, so the timer itself submits its chain, immediately when the queue
is free and otherwise after the current run and the schedules due before
it. While the timer is off or timers do not run in the process, Run now
(`POST /api/admin/sources/{source}/run`) submits the same chain once,
triggered by the operator, and leaves the schedule's next run where it was;
the history shows it as "X · run now". The follow-up
steps always run, even when the source wrote nothing: each works from its
own queue and makes no provider call while that queue is empty, so failed
extractions, pending image reviews, download retries and operator
translation requests keep moving while a source is quiet. YouTube pushes
submit the `youtube` chain as well (labelled `websub`). The operator's
single-video refresh keeps its own steps, and the
[live-status probe](#live-status-probe) has its own timer. X has no push, so its
timer is how new posts arrive; YouTube push and the probe catch most
YouTube changes and its timer is a fallback; neither Bilibili interval can
go below the environment floor, and the adapter's own interval and backoff
checks stay as the safety net.

The X, related-accounts and YouTube schedules carry a sync setting that
says how much a run reads
([source schedules](../architecture/workflows.md#source-schedules)). Each run
freezes it in `workflow_runs.source_sync_json`:

- `{ "mode": "incremental", "max": 50 }`, the default: read newest first until
  a stored record, at most `max` (1–200) records that are not stored yet, per
  X account, per related account or for the YouTube channel. An X run that
  stops short leaves a
  [timeline gap](sources.md#x) that later runs read; a YouTube run that
  leaves new uploads above its cursor keeps the cursor, and later runs take
  them ([YouTube](sources.md#youtube)).
- `{ "mode": "count", "count": N }`: read the newest `N` (1–200) every run,
  stored or not. X reads them as a `count` window from the timeline, which
  leaves the incremental cursor and pending queue alone. YouTube reads the
  uploads listing. The related accounts read incrementally only, so their
  schedule refuses a `count` setting.

Every other run with an X, related-accounts or YouTube step reads that
source with these
settings too, frozen on the run: a YouTube push, Run now, a
[one-off step run](#modular-steps-and-timers) and the operator's single-video refresh.
A [manual fetch](#manual-fetches-and-historical-backfill) reads through its
method instead. Full syncs (console, MCP, `kano-go sync`) carry no setting
and run incrementally with the full-sync post cap for X and its related
accounts and 50 uploads for YouTube.

#### Method chains

Each X and YouTube schedule also orders its listing methods into a fallback
chain ([method chains](../architecture/workflows.md#method-chains)), stored
in `source_schedules.chain_json` and edited in the timed reads of the
Sources page. A read tries the enabled methods from the top and falls to the
next when the first page fails, when the answer lists none of the account's
or channel's own records, when the chain skips the method, or on quota and
rate limits. A later page that fails keeps the earlier pages and leaves the
run partial without falling back.

| Source      | Methods, in default order                                                                                         | Default                                |
| ----------- | ----------------------------------------------------------------------------------------------------------------- | -------------------------------------- |
| `x`         | `timeline` (FxTwitter timeline), `profile` (logged-out `x.com/<handle>`, IDs only), `api` (X API)                 | `api` off                              |
| `youtube`   | `api` (Data API uploads playlist), `rss` (public feed), `ytdlp` (metadata), `videos_page` (uploads tab, IDs only) | api, rss and videos_page on; ytdlp off |
| `x_related` | `timeline` (FxTwitter timeline)                                                                                   | fixed                                  |
| `bilibili`  | `list` (the upload list)                                                                                          | fixed                                  |

The default keeps the existing order and leaves yt-dlp off until it is
configured, so the default chain applies when no stored chain is available. A method whose
credential the process lacks is skipped with `x_api_unavailable` or
`youtube_api_key_missing`. The chain covers listing methods only: detail
requests (vxtwitter, watch pages) and fixed helper reads (the streams page,
and the feed read for Shorts verdicts while the Data API lists) stay outside
it. [X](sources.md#x) and [YouTube](sources.md#youtube) describe the methods,
and [X API in the chain](sources.md#x-api-in-the-chain) its gate and daily
cap. An X `count` sync setting reads the timeline's count window whatever
the chain says.

Each run freezes its chain in `workflow_runs.source_chain_json`, so a retry
or recovery reads the same way: timer runs, Run now, YouTube pushes, one-off
step runs and the [live-status probe](#live-status-probe), which reads the
YouTube chain. Full syncs and runs stored without a chain read through the
default. The step result names the method that served each X account in
`discovery` (`<account>: <method>`, or `none`) and the channel in `listing`,
and lists every method tried, in order, in `tried`: `<method>` for the one
that served and `<method>: <code>` for the others, each prefixed with the
account for X.

Enabling a schedule or changing its interval plans the next run one interval
later, never immediately; a sync change alone keeps the plan. Claiming a run
the timer submitted plans the next one an interval after the claim. Only the
first claim plans: claiming it again after a retry or after recovery from an
expired lease (both counted in `recovery_count`) keeps the plan, and the
operator's one-off run of the chain does not move it either. Starting the
timer moves the plan to the moment it is pressed unless the schedule is
already due, and the claim plans the next run as for any timed run.

Each plan adds a random offset of whole seconds, uniform in ±10% of the
interval (a 30-minute X timer runs 27–33 minutes apart), so timed reads do
not reach FxTwitter, YouTube, `api.bilibili.com` or the shop on a fixed
cadence. An offset that would leave the interval bounds is reflected back, so
a plan is never shorter than 15 minutes (the shop's 180) nor longer than 7
days. A Bilibili timer's
offset is never negative (0 to +10%): its interval is never below
`BILIBILI_MIN_INTERVAL_MINUTES`, so a plan is never sooner than the floor and
the adapter's interval check does not skip a timed run. Run now, YouTube
pushes and the live-status probe have no offset. The draw is the store's
injectable `SetScheduleOffset`, which tests replace for deterministic plans.
`WORKFLOW_SCHEDULER_ENABLED=0` stops source schedules as well, and `SKIP_X`,
`SKIP_YOUTUBE`, `SKIP_BILIBILI` and `SKIP_SHOP` disable that source's
schedules (both X timers for `SKIP_X`, both Bilibili timers for
`SKIP_BILIBILI`), which the console reports as
`{ skipped: true, reason: "environment" }`.

#### The `workflows` table

Per-source timers are the only timers. The `workflows` table, with its
`source_sync_json` and `source_lookback_json` columns, remains in the schema
and is empty on a fresh installation; no timer is derived from it and current
runs do not read or write workflow definitions. A run whose `workflow_id` is
not a reserved `@` ID keeps that identifier in history without joining
`workflows`.

Every start of `kano-go serve` raises a stored interval of either Bilibili
timer below `BILIBILI_MIN_INTERVAL_MINUTES` to that floor. The
`bilibili_dynamics` and `x_related` timers are timers of their own: each
stays off until the operator turns it on, whatever the switch of the Bilibili
or X timer.

## Modular Steps and Timers

Synchronization is split into modular steps declared in
`backend/internal/jobs` (`StepOrder`), executed in this order: `x`,
`x_related`, `youtube`, `bilibili`, `bilibili_dynamics`, `shop`, `media`,
`schedule`, `translate`, `reconcile`. `kano-go sync` (`make sync`) runs every step
and records the ones disabled by `SKIP_*` as skipped.
A one-off step run (the Runs page, `POST /api/admin/runs` with
`{ "kind": "workflow", "steps": [...] }`) picks any subset from the catalog
(`GET /api/admin/runs/steps`) and runs it once in catalog order; nothing is
saved. Its run records `sync_runs.source = workflow` and exposes per-step progress (`pending`, `running`, `completed`,
`partial`, `failed`, `skipped`) through `/api/admin/runs`. When a selected
step is disabled by `SKIP_*`, its result records `{ skipped: true, reason:
"environment" }` instead of silently disappearing. Every run keeps its own
steps, sync settings and windows, so a failed run with a retryable failed
step can be retried without any saved definition.

`bilibili` and `bilibili_dynamics` pace themselves: `bilibili` reads the
account's upload list and `bilibili_dynamics`, with a login, its
dynamics, each at most once per `BILIBILI_MIN_INTERVAL_MINUTES` on its own
clock and never during the shared captcha backoff, so either is safe in a
manual run soon after its timer (see [Bilibili](sources.md#bilibili)).
`shop` makes no request while a 429's `Retry-After` lasts (see
[Official shop](sources.md#official-shop)). A read skipped without a request
records `{ skipped: true, reason }` and succeeds; a failed read makes its step
`failed`.

`reconcile` derives the YouTube events of stored videos and applies the
deterministic duplicate-merge rules in
[Duplicate merging](data-model.md#duplicate-merging) to the Japan dates from
30 days ago to 60 days ahead, then the
[video merging](data-model.md#video-merging) rules to every Bilibili upload.
Every run that includes `x`, `x_related`, `youtube`, `bilibili`, `shop` or `schedule` (sync, scan,
manual fetches, source schedules, Run now, step runs, WebSub pushes and MCP) appends it as its last
step; it can also be selected alone. Its
result counts `releasesPaired` (see
[Duplicate merging](data-model.md#bilibili-releases)), `considered`,
`merged`, `retargeted`, `released`, `kept` and
`ambiguous` events, `eventsTimed` and `eventsCreated` YouTube events, and
`videosConsidered`, `videosMerged`, `videosReleased`, `videosKept` and
`videosAmbiguous` uploads, and `tagged` surviving events whose label changed
to the label of a notice merged into them (see
[Event labels](data-model.md#event-labels)). It advances the dashboard
revision only when a merge decision, a derived YouTube event, an inherited
label or a merged upload's translation changed.

`x` reads the own accounts of `X_HANDLES` through the X method chain; their
quotes and reposts write the X status pool with each post. `x_related` reads
every related account's timeline into the pool (see
[Related accounts](sources.md#related-accounts)): without a related account
it records `{ skipped: true, reason: "no_accounts" }` and succeeds without a
request; a failed account leaves it `partial` with `@<handle>: <code>` in
`errors`, and when every account failed it fails with `x_related_failed`,
retryable like `x_source_failed`. A manual fetch of one related account
reads that account alone (see
[Manual fetches](#manual-fetches-and-historical-backfill)). Neither step's
outcome affects the other's.

`translate` first settles the forms of Bilibili uploads by rule (see
[Bilibili form](data-model.md#bilibili-form)), then machine-translates X
post text, the titles of public videos and the X
statuses own posts quote that lack a usable translation (see
[Translation](sources.md#translation)), then labels
unlabelled YouTube reservations from their title with the same queue (see
[Event labels](data-model.md#event-labels)), then asks the Bilibili uploads
no rule settled for their form and tag, then tags the videos no entry
labels, each with what is left of the same per-run limit (see
[Video form and tags](data-model.md#video-form-and-tags)); its result
counts them under `eventTags`, `bilibiliForms` (with `ruled`, the forms the
rules settled, and `uncertain` answers) and `videoTags`. A title that fails counts as
failed and the step goes on;
only a store failure or cancellation stops it. Its final failure does not
abort a run, so `reconcile` still runs after it. It reads
only the translation queue and records a disabled result while that queue
is empty. `POST /api/admin/translations/request` starts it alone as a
`translate` run after queueing operator requests.

The server process checks due source schedules every 30 seconds and submits
the one with the earliest planned run, so a frequent source cannot starve
another, through the same single-flight queue as manual and MCP jobs; a
schedule that finds the queue busy stays due for the next tick. Set
`WORKFLOW_SCHEDULER_ENABLED=0` to stop the source schedules and the
live-status probe.

### Live-status probe

The [live-status probe](sources.md#live-status-probe) asks YouTube about
active videos and Bilibili about the account's
[live room](sources.md#live-room). It has its own timer and its own operator
settings, stored in `app_settings`: for YouTube `youtube_live_probe_enabled`
(default on) and `youtube_live_probe_interval_minutes` (default 15, 5–360),
for Bilibili `bilibili_live_probe_enabled` (default on) and
`bilibili_live_probe_interval_minutes` (default 30, 10–360). They are edited
in `/admin` (Sources → Live-status probe).
Each active video is due by its phase against its reserved start (its actual
start once on air), counted from its last successful check (a watch page or
a Data API answer) or probe request, whichever is newer:

| Phase      | When                                          | Next check                           |
| ---------- | --------------------------------------------- | ------------------------------------ |
| `prestart` | within 15 minutes before the start            | once in that lead, then at the start |
| `starting` | from the start for 45 minutes, not yet live   | every 3 minutes                      |
| `live`     | the check reported the broadcast live         | every operator interval              |
| `overdue`  | 45 minutes after the start and still not live | every operator interval              |

A check by the regular YouTube step counts as well, so a reservation it just
read is not asked again. The same server process checks every 30 seconds;
when a video is due and at least two minutes have passed since the previous
probe run was queued, it queues a run of the single `live` step
(`kind: "live"`, ad hoc workflow `@live`, labelled `live-probe`) through the
same single-flight queue. The run asks only about the due videos, earliest
first: with `YOUTUBE_API_KEY` and the YouTube chain's `api` method on, in one
Data API call, then the watch pages of
those the API could not settle (see
[Live-status probe](sources.md#live-status-probe)). A busy queue or nothing
due waits for the next tick. Requests are recorded per video in `sync_state`
(source `youtube-live`), answered or not, so a video that keeps failing is
asked again on its phase's schedule rather than on every tick; entries older
than two days are dropped. A private or removed video, and any video an
operator stops, leaves the active set instead (see
[Stopped videos](sources.md#stopped-videos)). Runs are recorded in `sync_runs` and
`/api/admin/runs` with `active`, `candidates` (the due videos), `requested`
(watch pages), `apiRequests` (Data API calls), `inspected`, `live`,
`changed`, `unavailable` (when a video was stopped as private or removed) and
fixed error codes; a run that recorded an error and answered none of its
videos is `failed`, one with any recorded failure (an API call that failed but
whose pages answered, for example) `partial`.

The probe may run more often than a source schedule (15 minutes) because one
run asks about at most five videos and runs only while something is due.
With a key it spends one quota unit per run, at most 720 a day.
One stream that starts on time costs a check before the start, a few in the
starting window and four an hour on air at the default interval; a waiting
room that never opens costs at most 15 in its starting window and then one
per interval until 24 hours after its start. At most five pages per run and
runs two minutes apart bound the worst case, several reservations starting
together, at 150 pages an hour; a regular YouTube step every 15 minutes (feed,
streams page and up to 12 detail pages) is about 56 requests an hour. Longer
than six hours would let a stale result outlive most streams. `live` is not
a catalog step: step runs and full syncs never select it.
`WORKFLOW_SCHEDULER_ENABLED=0` stops the probe; `SKIP_YOUTUBE=1` stops its
YouTube part and `SKIP_BILIBILI=1` its Bilibili part.
Page loads, refreshes and revision requests never start it.

The Bilibili part follows every active stream and members-only entry that
starts within the next 15 minutes or the last 24 hours. An entry shown on
Bilibili (its platforms, with those its merged entries lend it, include
`bilibili`; see [Event platforms](data-model.md#event-platforms)) goes
through the same phases at the Bilibili interval. Every other one gets
sparse checks (phase `sparse`), about 5 and 20 minutes after its start and
then one per Bilibili interval while the room is live, so a broadcast on
Bilibili that only a YouTube reservation announced is seen as its
simulcast.
An entry whose broadcast the room already showed and ended, an ended entry
and a manually confirmed cancellation are left out. Without a due entry the
room is still asked about once per Bilibili interval after its last check
(phase `baseline`), so an unannounced stream is seen. A due Bilibili check
joins the same probe run as due YouTube videos and costs one request, never
more; it never runs during the live host's backoff. Its counts are under
`bilibili` in the run result (`targets`, `phase`, `requested`, `live`,
`changed`, `errors`). At the default interval the baseline costs 48
requests a day, one stream shown on Bilibili a few more around its start,
and any other scheduled stream about two.

## Run Execution

Every run is a durable `workflow_runs` row with its ordered
`workflow_run_steps` and one `workflow_step_attempts` row per try; the
console's job list and the `sync_runs` history are projections of them (see
[Backend](backend.md#jobs-and-workflows) for the reserved run IDs). A run
freezes what its steps read: `fetch_windows_json` for operator windows,
`source_sync_json` for schedule sync settings, `source_chain_json` for their
method chains and `source_lookback_json` for per-step days. Retry and recovery therefore repeat the same work without a
saved definition.

Statuses are fixed enums. Runs use `queued`, `running`, `succeeded`,
`partial`, `failed`, `cancelled` and `blocked`; steps use the same set with
`pending` in place of `queued`; attempts use `running` and the terminal
values. `trigger_source` and `execution_source` are `operator`, `scheduler`
or `recovery`. Idempotency keys are stored as SHA-256 hashes. Admin
projections omit raw payloads, stack traces, paths, credentials and provider
text.

Each step records the adapter that serves it in `adapter_capability`
(`x_recent`, `x_related`, `youtube_recent`, `bilibili_recent`, `bilibili_dynamics`,
`youtube_live`,
`media_download`, `schedule_extract`, `text_translate`, `media_variants` or
`event_reconcile`), which projections read before the constrained
`capability` column (`synthetic`, `x_recent` or `capability_not_migrated`).
A step without an adapter finishes `blocked` with `capability_not_migrated`
and never counts as successful work.

**Lease.** Claiming a queued run takes the SQLite write lock, sets it
`running` and writes the single `workflow_leases` row (scope `global`) with
an owner, a generation and an expiry two minutes ahead; a heartbeat renews
it every 40 seconds. Every step, attempt and run write, and every source
transaction, checks owner, generation and expiry in the same transaction,
so a worker that lost its lease cannot commit. A renewal that finds the
lease lost cancels the run's outbound requests; a renewal that fails to write,
such as on a busy database, is left to the next one, and the renewal after an
expiry finds the lease lost. A partial unique index allows at most one
`queued` or `running` run, so a second start answers `409` with `busy` and the
active run. A dispatched run has at most 20 minutes. A run stopped by that
limit, or by a lost lease, finishes `failed` with `timeout` or
`lease_expired` and its stopped step is retryable; only an operator's
cancellation or a shutdown finishes it `cancelled`.

**Retry.** A step error carries a category. `timeout`, `rate_limited` and
`dependency_unavailable` are retryable and get one more attempt in the same
run, five seconds later; `permanent`, `schema_failure`, `validation_failure`, `cancelled` and
`blocked` are not. The final failure of `x`, `youtube`, `bilibili` or
`translate` is recorded with its code and the run continues, finishing
`partial`, or `failed` when no step completed; a source step that carries a
[manual fetch](#manual-fetches-and-historical-backfill) is the exception. Any
other final failure ends the run and marks its pending steps `blocked`.
`POST /api/admin/runs/{id}/retry` requeues a `failed`, `partial` or
`blocked` run while no other run is active, when it has a retryable failed
step and no permanent failure: retryable failed and blocked steps return to
`pending`, and completed steps and earlier attempts stay.

**Recovery.** At startup and every 30 seconds the server recovers `running`
runs whose lease is missing or expired: their running attempts become
`failed` with `lease_expired` (retryable), their running steps return to
`pending`, and the run returns to `queued` with `execution_source =
recovery` and `recovery_count` incremented. A queued run is then dispatched.
`POST /api/admin/runs/{id}/cancel` records the request; a queued run is
cancelled at once, and a running one stops its network requests and finishes
`cancelled` with its pending steps. Finishing a run updates its `sync_runs`
row and the dashboard revision and releases the lease.

**Retention.** The server process deletes finished runs past their
retention window when it starts and then once a day. Succeeded and
cancelled runs are kept 30 days by default; failed, partial and blocked runs
90 days. Both windows are operator settings in `app_settings`
(`run_retention_succeeded_days`, `run_retention_failed_days`, 0–3650; 0 keeps
those runs forever), edited on `/admin` → Runs → History cleanup, and the
next pass applies a saved value. A run's age counts from `finished_at`
(`created_at` when it has none). A pass deletes a run with its
`workflow_run_steps`, `workflow_step_attempts` and `sync_runs` rows in one
transaction, at most 200 runs per transaction, then applies the same windows
to `sync_runs` rows that belong to no stored run (`success`; `failed` and
`partial`). It never deletes a queued, running or leased run, the newest
run of each `workflow_id` (the live-status probe paces itself and the
Sources page reads each schedule's last run from it) or the newest
successful `sync_runs` row (the board's freshness reads it), whatever their
age. A deleted run can no longer be retried. The pass is local database
work, not a run: it does not use the queue, and
`WORKFLOW_SCHEDULER_ENABLED=0` does not stop it.

Schedule-step outcomes combine post extraction and image verification. An
image-only failure cannot be reported as success: a scan with failures and no
successful/cached/uncertain work is `failed`; mixed outcomes are `partial`.
Workflow progress, persisted `sync_runs`, and standalone scans use the same
aggregation. Completed sync/workflow jobs expose their persisted run, including
its failure status, and always release the single-flight queue after settlement.

## Implementation Ownership

backend/internal/jobs owns the ordered step catalog and presents durable
workflow runs as operator jobs. internal/workflow maps steps to their adapters
(`SourceSteps`), executes runs with leases, retries and recovery, and owns the
single-flight queue, and the timer that starts due source schedules
(`SourceChains`) through it. `jobs.QueueManager` submits Run now and step
runs and freezes each source step's schedule sync and method chain on them.
A new module needs one catalog entry and one step adapter; the console reads
the catalog rather than maintaining another step list. See
[Backend](backend.md#jobs-and-workflows).

Adapters, candidate selection, model routes, modality, retries, and per-post
reprocess behavior are defined in [Sources and extraction](sources.md).
Register media candidates before bounded downloads; only successfully fetched
records are upserted. A failed step records its outcome and retains known state.

## Related Docs

- [Sources and extraction](sources.md)
- [Failure and freshness](../operations/reliability.md)
- [Configuration](../operations/configuration.md)
- [Admin operations](../operations/database.md#manual-maintenance)
