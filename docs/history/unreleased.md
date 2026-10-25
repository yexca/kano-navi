# Unreleased

<details>
<summary><strong>Operator console</strong></summary>

- The Videos page shows each video's locally cached thumbnail, and vertical
  covers stay inside the 16:9 frame.
- The Schedule page shows merged duplicates with the entry they are hidden
  behind, filters them, and can undo a merge so the entry stays separate.
- Sources has a Live-status probe panel to turn the probe on or off and set
  its interval (default 30 minutes, 5 to 360); its runs appear in the run
  history as "Live-status probe".
- Schedule status badges are translated, including the Chinese statuses that
  older manual entries still store.
- The Posts page is a paged table with separate columns for the account and
  time, the text, the image, the extraction state, the extraction route and
  the actions. The image column shows the first locally cached image and how
  many the post has, and clicking it enlarges the images inside the page,
  one at a time; Queue again and Open post are icons. A narrower page moves
  the account above the text, and a phone stacks each row.
- The Posts filter picks one of the configured X accounts instead of the
  legacy X / YouTube source choice. `GET /api/admin/posts` matches `account`
  without case or a leading `@` and adds `media` and `mediaStatus`.
- The Videos page shows the pinned featured video above the list, even when
  it is on another page or outside the filter, and returns to the automatic
  choice in one click. `GET /api/admin/videos` adds `featuredVideo`.
- A post queued for extraction no longer shows "Queued" twice.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- Thumbnail downloads retry transient failures after 30 minutes, fall back to
  a smaller YouTube size when the larger one was never generated, and pick up
  a replaced cover for upcoming and recent videos.
- YouTube sync also checks recent RSS entries, so waiting rooms and premieres
  missing from the streams page get their start time. Premieres become
  releases, and finished streams and premieres keep YouTube's reported end.
- A new `reconcile` step, appended to every run that writes events, merges
  duplicate entries of one stream: a weekly schedule's date-only entry into
  the timed stream of that day, and an X notice into the YouTube reservation
  that starts within 30 minutes. Ambiguous days, manual entries and
  cancellations are left alone, and merges survive re-extraction.
- A lightweight live-status probe rechecks only the watch pages of up to five
  reservations that are starting or started and streams without a known end,
  on its own interval through the job queue. It records the actual start,
  the end, the reservation state and the live status, and updates the board
  only when something changed.
- A stream YouTube reports as live is no longer stored as completed.
- Events store English status codes (`scheduled`, `live`, `ended`,
  `pending`) and English details (`YouTube Live`, `YouTube Premiere`) instead
  of Chinese text. Startup converts earlier automatic entries; manual and
  deleted entries keep their text. Extraction asks the model for English
  details and status words.
- X sync discovers posts from the public FxTwitter timeline and pages back to
  the last stored post, so several posts published between runs are no longer
  missed once they drop off the profile page. Reposts are skipped, and the
  logged-out profile page remains the fallback when the timeline fails.

</details>

<details>
<summary><strong>Public pages</strong></summary>

- In English and Japanese, the schedule no longer shows Chinese reservation
  text such as `YouTube Live / 预约`.
- A started premiere or release no longer shows as on air for three hours;
  only streams are presumed live without an end, and a presumed-live item
  gives the spotlight to a reservation starting within 30 minutes.
- The calendar, its day counts and page totals, the board, the spotlight and
  MCP `schedule_list` show a merged duplicate stream once.
- On air follows YouTube's confirmed live state when the probe has a fresh
  result: a long stream stays on air past three hours, a late waiting room no
  longer shows as on air, and an ended stream leaves the stage. Without a
  result, the start-time grace still applies.
- The week guide and month grid show On air whenever the stage does, instead
  of calling a stream that is still within its live window ended, and a late
  waiting room stays upcoming there.
- Posts on X now come before YouTube on the board.
- The schedule shows one week; the month grid moved to a Month view dialog
  with its own month navigation, where picking a date lists that day's
  entries and can show its week on the board.
- Past posts and Past videos dialogs browse every stored post and video month
  by month, with load-more paging, jumps to the nearest month with records,
  and the account filter for posts. They read the new `/api/posts` and
  `/api/videos` endpoints.
- The site is named What’s Kano Up To? in English, 鹿乃なにしてる？ in
  Japanese and 鹿乃在做什么 in Simplified Chinese, in the header, the page
  titles and the About page. Kano Navi stays the project name and appears
  beside the version on the About page. Between 601 and 680 px wide the
  header shows only the avatar.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- A new installation names its two starting workflows "Full refresh" and
  "Sources only" instead of Chinese names. Existing workflows keep their
  names; rename them on the Runs & workflows page.

</details>
