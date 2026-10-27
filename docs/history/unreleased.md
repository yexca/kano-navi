# Unreleased

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- Each source runs on its own timer instead of through saved workflows
  (ADR-0003). X (default every 30 minutes), YouTube (60) and Bilibili (180,
  never below `BILIBILI_MIN_INTERVAL_MINUTES`) each submit a fixed chain
  through the same single-flight queue: `x, media, schedule, translate`,
  `youtube, media, translate` and `bilibili, media, translate`, followed by
  `reconcile`. The scheduler starts whichever due timer or timed workflow was
  planned earliest. A fresh installation keeps the timers off. YouTube pushes
  now queue the `youtube` chain, so pushed videos are translated too.
- **Upgrade:** on the first start, timed workflows that contain a source step
  hand their timer to that source's schedule (the smallest interval among
  them, raised to the Bilibili floor, and the workflow's `sourceSync`), and
  their own timers are turned off. Their steps and history stay. Timed
  workflows without a source step keep their timers. This runs once.
- X and YouTube timers read by count instead of by days. Each source is
  `incremental` (the default: newest first until a stored record, at most
  `max`, 50 by default) or `count` (the newest N every run), each 1–200.
  Manual runs of saved workflows and YouTube pushes read the same way. The
  workflows' lookback days are no longer read.
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

- The Sources page has a section per source with its timer, interval, read
  mode (X and YouTube), next run and last run. The YouTube section also
  holds push notifications and the live-status probe. The new
  `PUT /api/admin/sources/{source}/schedule` saves a timer, and
  `GET /api/admin/sources` lists them as `schedules`.
- Saved workflows are for manual runs and custom combinations. The editor no
  longer sets lookback days or read modes, and its timer is turned off, with
  an explanation, for a workflow with a source step. The workflow API rejects
  `sourceSync` and `sourceLookbackDays`, and refuses a timer on a workflow
  with a source step, naming the source timer to use.
- Run history labels timer runs by source ("X timer"), and the overview's
  next timed run includes the source timers.
- YouTube fetches offer a `count` window read from the uploads listing.
  Without a key it reaches only the RSS feed's 15 entries and reports
  `youtube_count_limited` above that.

</details>
