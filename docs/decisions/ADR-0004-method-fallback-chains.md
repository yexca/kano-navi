# ADR-0004: Method Fallback Chains for Timed Source Reads

- Status: Accepted
- Date: 2026-10-30
- Amends: [ADR-0002](ADR-0002-count-based-source-sync.md) (supersedes its
  decision to keep the X API out of incremental X reads)

## Context

Manual fetches name one listing method and never fall back (see
[Manual fetches](../architecture/workflows.md#manual-fetches-and-historical-backfill)).
Timed reads had a hard-coded order instead:

- X read the FxTwitter timeline and fell back to the logged-out profile
  page, which lists only the newest few status IDs. When both failed, the
  account was not read. The official API served only manual fetches,
  because ADR-0002 judged a second incremental cursor model for it not worth
  the cost.
- YouTube read the Data API's uploads playlist when a key was set, then the
  RSS feed, then the uploads tab. With a key, every run also spent quota on
  details, the live-status probe and the Shorts playlist, and the operator
  could not turn that off without removing the key.

FxTwitter is a third-party service. When it is down for days, X posts stop
arriving, and the schedules they announce are missed. The X API can read the
same posts, but X bills every post a read returns, so it must stay a bounded
fallback rather than the default.

## Decision

### Chains

Each X and YouTube source schedule stores an ordered list of its listing
methods, each enabled or not, in the additive `source_schedules.chain_json`
column. A read tries the enabled methods from the top and falls to the next
when the first page fails, when the answer lists none of the account's or
channel's own records, when the method is skipped (a missing credential, the
X API gate or its daily cap), or on quota and rate limiting. A later page's
failure does not fall back: the run keeps the earlier pages and is partial,
as before.

| Source   | Methods (default order)                        | Default                        |
| -------- | ---------------------------------------------- | ------------------------------ |
| X        | `timeline` (FxTwitter), `profile`, `api`       | `api` off                      |
| YouTube  | `api` (uploads playlist), `rss`, `videos_page` | all on (`api` runs with a key) |
| Bilibili | `list`                                         | fixed, not reorderable         |

The default is the former order, so an upgrade reads exactly as before.

Each run freezes its chain in `workflow_runs.source_chain_json`, as
`source_sync_json` freezes the sync setting, so retries and recoveries read
the same way. Timer runs, Run now, YouTube pushes, one-off step runs and the
live-status probe (which reads YouTube's chain) freeze their schedule's chain.
Full syncs (`kano-go sync`, the console's Sync everything, MCP) and runs
stored without a chain use the default.

The chain covers listing methods only. Detail requests (vxtwitter, YouTube
watch pages) and fixed helper reads (the YouTube streams page, the feed read
for Shorts verdicts when the Data API lists) stay outside it.

### X API in the chain

The X `api` method reads `GET /2/users/{id}/tweets`
([user posts timeline](https://docs.x.com/x-api/users/get-posts)) with
`exclude=retweets,replies` and `since_id` set to the account's cursor. We
chose it over the two search endpoints:

- It lists exactly what the FxTwitter timeline lists: the account's own
  posts, newest first, without reposts or replies. Search needs a query that
  approximates it.
- Recent search covers only seven days, so it cannot fill a gap left by an
  FxTwitter outage longer than a week; `since_id` older than that fails.
- Full-archive search needs full-archive access, which the chain should not
  require. The user timeline reaches the account's newest 3,200 posts, far
  more than a gap holds.
- `since_id` and `until_id` bound a read by status ID, which closes a gap
  exactly (below).

The numeric user ID is looked up once per process
(`GET /2/users/by/username/{username}`).

**Gate.** The chain uses `api` only after the account's timeline failed N
consecutive runs (default 3, 0–100). The current run's failure counts, so the
Nth failing run already reads through the API; any run the timeline serves
resets the count. A chain without the timeline has nothing to count, so the
gate does not apply.

**Daily cap.** Posts the API returns per Japan day are capped (default 100,
1–10,000). The two accounts post a few times a day, so a `since_id` read that
finds nothing new returns nothing, and an ordinary day of fallback reads
returns a few dozen posts at most. 100 leaves room to fill a gap after an
outage while bounding a day's bill to a known number of posts, about 3,000 a
month at worst. A read never asks for more posts than remain; with fewer
than five left (the endpoint's smallest page), the chain skips `api` with
`x_api_daily_cap` and the run is partial. Manual fetches through the X API
ignore the gate and the cap, but the posts they read count toward the day.

### Gaps stay method-independent

A timeline gap's continuation is an FxTwitter cursor. When another method
serves an account, it drops the continuations and keeps every gap's floor,
so the timeline reads each gap again from the newest page. The `api` method
closes a gap exactly: it reads between the floor (`since_id`) and the oldest
stored status above it (`until_id`). A read that the cap stops leaves the
gap; the next read resumes below the oldest status stored.

### Parity

A post the API returns is stored exactly as the timeline and detail paths
store it: a long post's whole text (`note_tweet`), links expanded, its own
media links removed and entities decoded; its own images in post order (a
photo's URL, or a video's or GIF's thumbnail); the quoted status's
projection; the counters; and `raw_json.mediaURLs`. Otherwise a later detail
refresh would change the post's extraction fingerprint and extract it again.
The manual X API method maps posts the same way.

### YouTube without the Data API

A YouTube chain with `api` disabled makes no Data API call at all for that
run: no listing, no `videos.list` details, no per-format or Shorts playlists,
and the live-status probe asks watch pages only. Watch pages take over as
they do without a key.

### Results

The step result names the method that served each X account (`discovery`,
as `<account>: <method>`) or the channel (`listing`), and lists every method
tried in order with its code (`tried`). X results add `apiPosts` and
`apiRead` when the API was used.

## Consequences

- An FxTwitter outage costs at most N runs of X posts per account; after
  that the X API reads them, within a bounded daily bill.
- The operator can stop YouTube quota use without removing the key, and can
  reorder methods when one source misbehaves.
- The X API method's text mapping follows the documented shapes of the X
  API, FxTwitter and vxtwitter. If a service changes how it expands links or
  long text, a post read through one path and refreshed through another may
  be extracted again once.
- A count sync setting for X still reads the newest posts from the FxTwitter
  timeline and does not fall back.

## Alternatives Considered

- Falling back to the X API on the first timeline failure. A single
  FxTwitter hiccup would then bill API reads; the gate keeps the API for
  outages.
- A monthly cap. A day bounds the damage of a misconfiguration sooner, and
  the operator sees the day's usage beside the setting.
- Keeping the API's next-page tokens as gap continuations. `since_id` and
  `until_id` need no stored continuation and stay valid across methods.
