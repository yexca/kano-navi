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
- Source timers no longer run on a fixed period. Each planned run moves by
  a random offset of up to ±10% of its interval, kept within the interval
  bounds; Bilibili only runs later (up to +10%), never sooner than
  `BILIBILI_MIN_INTERVAL_MINUTES`.
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
- Schedule extraction backs off posts that keep failing instead of retrying
  them on every X run: 1, 6 and 24 hours after the first three consecutive
  failures of the same input, and no automatic retry after the fourth. New
  content, a changed model queue or an operator reprocess starts it over, and
  a queued reprocess always runs. Held posts make no model call, do not use
  the scan limit and are counted as `backoff` or `retryStopped`. Startup adds
  `failure_count`, `failure_fingerprint` and `next_attempt_at` to
  `post_llm_states`.
- Incremental X runs store a status straight from the timeline page they
  already read and keep vxtwitter detail requests for statuses listed
  without content, pending statuses no longer listed, known-post refreshes
  and profile fallbacks. `X_MAX_STATUS_REQUESTS` now bounds only those
  requests, so a backlog drains in fewer runs; the step result counts
  `timelinePosts`. Timeline photos are stored without FxTwitter's
  `?name=orig`, and detail answers keep a video's or GIF's thumbnail instead
  of its file, so both paths store the same post.
- Pending X statuses stop being requested forever: two consecutive 404/410
  (or other-account) answers drop one with `x_status_gone`, and other
  failures wait 1, 6 and 24 hours before the fourth drops it with
  `x_retry_limit`. Failure counts live in
  `sync_state.metadata_json.pendingRetries`; the step result reports
  `pendingHeld` and `pendingDropped`.
- With `YOUTUBE_API_KEY`, the live-status probe asks one `videos.list` call
  about all of a run's due videos before reading any watch page, and reads a
  video's page only when the API fails, leaves it out or cannot classify it.
  Each probe run costs one quota unit; the run result counts `apiRequests`.
  Without a key the probe reads watch pages as before.

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
- Posts whose extraction keeps failing show when they are retried next, or
  that automatic retries stopped, next to their state on the Posts and Review
  pages. `GET /api/admin/posts` adds `llmRetryAt` and `llmRetryStopped`.

</details>
