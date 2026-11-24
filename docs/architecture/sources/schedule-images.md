# Schedule Images

How schedule images pass the source-text gate and image-model or manual approval.

Recent X synchronization and manual X fetches (timeline and API pages) share
one candidate rule: a post with an image of its own whose text contains one
of the fixed source keywords (`schedule`, `スケジュール`, `配信予定`,
`週間予定`, `今週の予定`, case-insensitive; the configurable extraction
keywords play no part) registers its first image as the schedule asset
`schedule-x-<post ID>`, with its source URL and account. The asset, its
`schedule-image` media link and a `pending` / `unreviewed` review commit
with the post, and with the page's continuation checkpoint for a manual fetch.
Registration does not run extraction or approve an image, and it writes no
period.

An operator can also make any of an X post's own images its schedule asset
from the console's post dialog, with a required reason and optional period.
The pick labels the asset `schedule` and sets `manual_image_at`; a different
image than the asset showed returns its model verdict to `pending`. Later
synchronization keeps a picked asset's image, review and period.

A schedule asset's period is `period_start`/`period_end` with its
`period_basis`; the schema upgrade that adds these columns fills them from a
stored `week_start` as a seven-day period. The image check does not read
dates, so an asset without a stored period takes the whole Monday-to-Sunday
weeks around the live (not deleted) events extracted from its source post,
with basis `events` (`SchedulePeriodSQL`,
`backend/internal/store/sqlite/schedule_period.go`). Readers compute it on
every request, so it follows later extractions and event edits. An asset
whose post has no events is unknown and labelled so in the console. Operators set or correct a period with a required reason
(`manual_period_start`, `manual_period_end`, `manual_period_reason`)
independently of image approval, and the manual period wins. The dashboard
and calendar list an approved asset only while its period overlaps the
requested dates.

A schedule image (`assets.kind = schedule`) is public only after two gates:

1. The board-notice gate (`scheduleBoardPattern`,
   `backend/internal/schedule/store.go`) checks that the source post's
   own wording (its stored `raw_json.search_text` when present, otherwise
   its text) reads as a weekly or multi-day board notice. The stored post `label` is not part of that input, because the
   looser post classifier labels any `配信予定` post as `SCHEDULE`.
2. The automatic scan sends each unreviewed candidate's ready cached image,
   without the post text, to the image extraction queue
   (`llm_route_targets.route = 'verify'`), whose targets need the `image` tag
   and which never falls back to the text extraction queue. The model records
   `schedule`, `not_schedule`, or `uncertain` plus the visible schedule text in
   `schedule_asset_reviews`. A candidate whose source post does not pass
   gate 1 is recorded as `skipped` / `source_not_board` without a model
   call. A pending cache, or no queued, keyed Image model, is also `skipped`
   and retried. On an image that already has a verdict, a skipped, running
   or failed check is recorded as the image's last check and the verdict, with
   the image's public visibility, stands; a skipped or failed check puts the
   image in the Review queue until a check settles it. Assets that share one
   image URL are checked once and the verdict is copied to the others; the
   one sent is the most recently checked asset whose post passes gate 1, so
   a sibling whose post fails it never stands in for the image. An asset without an image URL, such as a `weekly-schedule`
   alias row, has nothing to download: it is recorded once as `skipped` /
   `no_image` and left out of later scans, forced ones included, until a
   writer gives it a URL. It is never public and never counts as needing
   review; the console shows it as having no image, and an operator may
   still label it `not_schedule`.

`/api/dashboard` returns a schedule asset only when an operator labelled it
`schedule`, or when it has no manual label, still passes gate 1, and the model
returned `schedule`. A manual `not_schedule` always hides it. A model verdict
that becomes or stops being `schedule` advances the dashboard revision with
the review. Labels require a
reason and are set on the `/admin` schedule calendar, in its list of
images or in the Review queue. A verdict belongs to the image URL it judged:
when a post's asset receives a different URL, its `asset_version` increases,
its model verdict returns to `pending`, and an operator's label on the old
image becomes the previous decision, shown beside the new image in the Review
queue until the operator labels it. A `schedule` label is cleared, so it never
approves the new image (the model's verdict on the new image may); a
`not_schedule` label keeps hiding it. The period correction stays.

Image review budgets apply after cache checks and manual-label filtering across
all stored candidates, rather than only the newest page. Retry order follows
the least recent check/review attempt. Reviews persist an input fingerprint
of the extractor version, image hash and source post URL. A `schedule`,
`not_schedule` or `uncertain` verdict for the current fingerprint stands
whichever check-queue target gave it, so adding, removing, reordering or
replacing targets and models sends no image again, and an empty queue keeps
the verdict. An image is checked again when its image or source changes, when its last
check failed, and in a forced pass.
