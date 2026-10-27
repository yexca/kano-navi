# ADR-0002: Count-Based Source Synchronization for Saved Workflows

- Status: Accepted; amended by [ADR-0003](ADR-0003-per-source-sync-schedules.md), which stores `sourceSync` on source schedules and removes saved workflows
- Date: 2026-10-26

## Context

Saved workflows used to carry `sourceLookbackDays` (X 7, YouTube 14). Most
of the time those days were not what bounded a run. The X incremental path
pages the FxTwitter timeline newest first until it reaches a stored status,
and is capped by `X_DISCOVERY_LIMIT`. YouTube lists newest-first uploads
(the RSS feed or the uploads playlist) and only filtered them by age. The
days decided what happened in two places only, and both were the
expensive paths:

- With `X_API_BEARER_TOKEN`, every run searched the whole X day window
  through full-archive search, which bills per post read.
- With `YOUTUBE_API_KEY`, every run searched the YouTube day window through
  `search.list`, which costs 100 quota units a page, where `playlistItems`
  costs 1.

There was also a gap. An incremental X run that hit its cap still moved
the cursor to the newest post. Statuses between the old cursor and the
oldest one read were never discovered by later runs.

## Decision

A saved workflow stores `sourceSync` per source in the additive
`workflows.source_sync_json` column. The setting is frozen on each run in
`workflow_runs.source_sync_json`.

- `{"mode": "incremental", "max": 50}` is the default. A run reads newest
  first until it reaches a stored record, and reads at most `max` (1–200)
  records per X account or per YouTube channel.
- `{"mode": "count", "count": N}` reads the newest `N` (1–200) records every
  run, whether or not they are stored. X serves it from the FxTwitter
  timeline count window. YouTube serves it from the uploads listing, and
  without a key the RSS feed's 15 entries are its reach.

Existing workflows whose column is still `{}` read as incremental with the
default cap. `source_lookback_json` is kept and no longer read for saved
workflows; operator `recent` windows still freeze their days there.

Credentials no longer turn a scheduled run into a date search:

- **X**: workflows always use the FxTwitter incremental path. The token is
  kept for operator `before` and `range` windows. We chose this over
  full-archive search with `since_id` because the timeline path already has
  the pending detail queue, schedule refreshes, request budgets and
  host protections. A second incremental cursor model for the API would
  duplicate all of that, and every run would still pay for the reads.
- **YouTube**: with a key, the recent step pages the uploads playlist
  (`playlistItems`, 1 unit and 50 entries a page). It stops at the first
  page holding a stored video in incremental mode and continues to the
  count in count mode. Reservations are still refreshed from the
  feed/streams page and the detail queue as before.

**Caps leave no gaps.** When an X account already has a cursor and the
incremental head cannot reach a stored status within the cap, page budget
or page failures, the run records a continuation in
`sync_state.metadata_json.timelineGap` as `{cursor, floor, older}`. `floor` is the
cursor the run started from, and `cursor` is the FxTwitter continuation
(empty restarts at the newest page). Later runs read the gap after the
head, within the same cap, until a status at or below `floor` appears that
no status above `floor` follows on the page (an old pinned post can lead the
newest page). A newer gap is read first and keeps the earlier one, with its
continuation, as `older`, so a burst of capped runs does not undo what was
read of it; past four gaps the oldest merge by keeping the older floor. A
stale continuation restarts at the newest page. A timeline that ends before reaching the floor closes the
gap with `x_timeline_gap`, so the operator knows to backfill with a range
window. The bootstrap age limit (`X_BOOTSTRAP_DAYS`) applies only while an
account has no cursor.

## Consequences

- Scheduled runs read about one timeline page or one playlist page when
  nothing is new, whatever the credentials. Quota use no longer grows with
  the run frequency times a day window.
- Operator `recent`, `before` and `range` windows keep their date meaning,
  and an operator YouTube `count` window reuses the uploads listing.
- A YouTube count above 15 without a key is partial and reports
  `youtube_count_limited`.
- A YouTube incremental run keeps no gap record. Its cap counts only uploads
  not stored yet, and a run that leaves new uploads above the stored cursor
  keeps the cursor, so later runs take them while the listing (the feed's 15
  entries, or four playlist pages) still reaches them. Uploads arrive far
  more slowly than X posts, and the uploads playlist answers 50 at a time.

## Alternatives Considered

- Keeping days and adding count only for operators would leave the quota
  cost of credentialed workflows in place.
- X API `since_id` incremental sync is discussed above. It remains possible
  later behind the same `sourceSync` setting.
