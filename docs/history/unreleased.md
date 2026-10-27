# Unreleased

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- Each source runs on its own timer, and saved workflows are removed
  (ADR-0003). X (default every 30 minutes), YouTube (60) and Bilibili (180,
  never below `BILIBILI_MIN_INTERVAL_MINUTES`) each submit a fixed chain
  through the same single-flight queue: `x, media, schedule, translate`,
  `youtube, media, translate` and `bilibili, media, translate`, followed by
  `reconcile`. The scheduler starts whichever due timer was planned
  earliest. A fresh installation keeps the timers off. YouTube pushes
  now queue the `youtube` chain, so pushed videos are translated too.
- **Upgrade:** saved workflows and their timers are removed; enable the
  source timers on the Sources page. Nothing is migrated. Older runs of
  saved workflows stay in the run history as retired workflows, and the
  `workflows` table stays in the database, unread.
- X and YouTube timers read by count instead of by days. Each source is
  `incremental` (the default: newest first until a stored record, at most
  `max`, 50 by default) or `count` (the newest N every run), each 1–200.
  Run now, one-off step runs and YouTube pushes read the same way.
- Credentials no longer turn a scheduled run into a date search. X timers
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
  mode (X and YouTube), **Run now**, next run and last run. Run now runs
  the source's chain once and leaves the next timed run as planned. The
  YouTube section also holds push notifications and the live-status probe.
  The new `PUT /api/admin/sources/{source}/schedule` saves a timer,
  `POST /api/admin/sources/{source}/run` starts Run now, and
  `GET /api/admin/sources` lists the timers as `schedules`.
- **Incompatible admin API change:** the workflow list and editor are gone.
  `/api/admin/workflows` is removed, `POST /api/admin/runs` answers
  `400 invalid_input` to a `workflowId`, runs no longer carry
  `workflowName`, and the overview's `workflows` becomes `timers`
  (source timers only). The Runs page keeps the run history and offers a
  one-off step run instead: pick steps from `GET /api/admin/runs/steps`
  and run them once with `{"kind": "workflow", "steps": [...]}`. A failed
  run of any kind can be retried when a step failed temporarily.
- Run history labels source runs by source ("X timer", "X · run now") and
  older saved-workflow runs as retired workflows.
- YouTube fetches offer a `count` window read from the uploads listing.
  Without a key it reaches only the RSS feed's 15 entries and reports
  `youtube_count_limited` above that.
- Fetching lives on the Sources page only. The Posts and Videos pages open
  straight on their lists, with a Fetch button in the header that leads
  there, and adding a Bilibili upload by BV ID moves to the Bilibili section
  of the Sources page.

</details>
