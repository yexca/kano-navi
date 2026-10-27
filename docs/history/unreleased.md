# Unreleased

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- Saved workflows read X and YouTube by count instead of by days. Each source
  is `incremental` (the default: newest first until a stored record, at most
  `max`, 50 by default) or `count` (the newest N every run), each 1–200.
  Existing workflows become incremental with the default cap; their lookback
  days are no longer read.
- Credentials no longer turn a scheduled run into a date search. X workflows
  always use the FxTwitter incremental path, keeping `X_API_BEARER_TOKEN` for
  `before` and `range` windows. With `YOUTUBE_API_KEY`, the recent step pages
  the uploads playlist (one quota unit per 50 uploads) instead of
  `search.list`.
- An incremental X run that hits its cap keeps a continuation and reads the
  rest on later runs instead of skipping the posts between its old cursor and
  the oldest one it read. A timeline that ends first reports
  `x_timeline_gap`. `X_BOOTSTRAP_DAYS` now applies only while an account has
  no cursor.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- The workflow editor sets each source's incremental cap or count in place
  of lookback days. `sourceSync` replaces `sourceLookbackDays` in the
  workflow API, which now rejects the old field.
- YouTube fetches offer a `count` window read from the uploads listing.
  Without a key it reaches only the RSS feed's 15 entries and reports
  `youtube_count_limited` above that.

</details>
