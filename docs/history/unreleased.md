# Unreleased

<details>
<summary><strong>Operator console</strong></summary>

- Posts can be filtered by an inclusive Japan-date range, and text search
  also matches the stored post ID across all pages.
- Models has a separate Translation queue for the models that translate X
  posts and YouTube titles into English and Simplified Chinese.
- Each X post on the Posts page shows whether its English and Chinese
  translations are complete and where each comes from (manual, platform or
  AI). A translation filter, a count of untranslated posts and a Translate
  missing button queue an incremental pass, and each post's editor saves or
  clears a manual translation per language or asks the AI to translate it
  again. New routes: `GET /api/admin/translations`,
  `POST /api/admin/translations/request` and
  `PUT`/`DELETE /api/admin/translations/{owner}/{id}/{locale}`.
  `PUT /api/admin/llm/queue` takes `route`.
- Each YouTube video on the Videos page has the same translation chips and
  editor, and the page counts and queues untranslated titles.
- The Videos page shows each video's locally cached thumbnail, and vertical
  covers stay inside the 16:9 frame.
- The Schedule page shows merged duplicates with the entry they are hidden
  behind, filters them, and can undo a merge so the entry stays separate.
- Sources has a Live-status probe panel to turn the probe on or off and set
  its interval (default 15 minutes, 5 to 360) and to show when it runs next;
  its runs appear in the run history as "Live-status probe".
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
- X date-range fetches work without `X_API_BEARER_TOKEN`, and a new "Latest
  posts" fetch reads each account's newest 1 to 200 posts (20, 50, 100 or 200
  in one click on the Posts page). `GET /api/admin/sources` adds `windows`,
  the fetch modes each source can run now, and the badge says when windows
  go through the timeline.
- Each video on the Videos page has a Correct dialog: override its title and
  kind, pin any cached version of its thumbnail, see the titles and
  thumbnails the platform replaced, and ask for its page to be read again
  first in the next YouTube step. The board, the video archive and MCP show
  the corrections, while sync keeps the platform values underneath. Cards
  mark corrected videos, their history
  and a pending refresh. New routes: `GET /api/admin/videos/{id}/history`,
  `PUT /api/admin/videos/{id}` and `POST /api/admin/videos/{id}/refresh`.
- The Videos page has a Bilibili panel: the account's read state (signed in
  or anonymous, last read, captcha backoff, last error) and a field that adds
  one upload by BV ID or bilibili.com link. A Platform filter separates
  YouTube and Bilibili videos, Bilibili cards are labelled, and the Correct
  dialog reads a Bilibili upload again at once. New route:
  `POST /api/admin/videos/bilibili`; `GET /api/admin/sources` adds `bilibili`
  and `GET /api/admin/videos` accepts `source`.
- A Bilibili upload merged into its YouTube video shows the video it belongs
  to, and the YouTube card links the upload; both can undo the merge, which
  keeps the upload separate for good. The Correct dialog merges an upload by
  hand, from its possible YouTube copies or any YouTube link, or merges a BV
  ID into a YouTube video. A Bilibili matches list above the videos shows the
  uploads the rule merges at its next run and those it leaves to the
  operator. New routes: `GET /api/admin/videos/merges`,
  `GET /api/admin/videos/{id}/merges`, `POST /api/admin/videos/{id}/merge`
  and `POST /api/admin/videos/{id}/unmerge`; videos add their merge state
  and `alternates`.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- A new `translate` workflow step translates X posts and YouTube titles with
  the translation queue. Manual translations outrank platform translations,
  which outrank the AI's, and a changed post or title is translated again.
  Bilibili titles are not machine-translated. `TRANSLATION_SCAN_LIMIT` and
  `TRANSLATION_LOOKBACK_DAYS` bound it, and `SKIP_LLM` skips it.
- Translation uses native JSON output and recovers incomplete model replies
  with a larger output budget or one corrective request. Unchanged song titles
  remain in the returned text, and protected runs record safe per-attempt
  diagnostics for operators.
- Thumbnail downloads persist retry backoff from 30 minutes to at most a day,
  retry missing files daily, fall back to a smaller YouTube size when the
  larger one was never generated, and pick up a replaced cover for upcoming
  and recent videos. Failed revalidation retains the ready file and its
  retry deadline.
- YouTube sync also checks recent RSS entries, so waiting rooms and premieres
  missing from the streams page get their start time. Premieres become
  releases, and finished streams and premieres keep YouTube's reported end.
- A new `reconcile` step, appended to every run that writes events, merges
  duplicate entries of one stream: a weekly schedule's date-only entry into
  the timed stream of that day, and an X notice into the YouTube reservation
  that starts within 30 minutes. Ambiguous days, manual entries and
  cancellations are left alone, and merges survive re-extraction.
- X notices with an approximate start time also merge into the only compatible
  YouTube stream starting within 30 minutes on the same date, while keeping
  their original time precision and the existing operator protections.
- A lightweight live-status probe rechecks only the watch pages of up to five
  reservations that are starting or started and streams without a known end,
  through the job queue. It follows each stream's start: one check shortly
  before it, every 3 minutes from the start until the stream is live (for up
  to 45 minutes), then the operator interval, so an on-time start reaches the
  board within minutes. It records the actual start, the end, the
  reservation state and the live status, and updates the board only when
  something changed.
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
- Without `X_API_BEARER_TOKEN`, operator X fetch windows page the FxTwitter
  timeline 100 posts at a time and store posts straight from it: a recent
  window reads back to its first day instead of stopping at the last stored
  post, and range and count windows resume from their own `x-timeline-window`
  checkpoint. Posts already stored, the incremental cursor and the pending
  queue are left unchanged, and timed workflows stay incremental.
- YouTube sync reads a finished video's page again 1 hour and 1, 3 and 7
  days after it airs (from its end, or else its start or publication), so a
  premiere's replaced thumbnail or a later title change reaches the board;
  each read also rechecks the cached thumbnail. Videos store their length.
- A title or thumbnail the platform replaces is kept as history, and a
  thumbnail file replaced at the same URL stays in the cache as an earlier
  version that `/media/<id>?v=<hash>` still serves.
- A new `bilibili` step stores the configured account's newest uploads from
  Bilibili's public web API, at most once every three hours by default. When
  Bilibili asks for a captcha it waits 6 hours, doubling up to a day, instead
  of retrying, and keeps every stored upload. Covers are cached from
  `*.hdslb.com`. The uploads stay in the console, so the board does not list
  a cover twice.
- The `reconcile` step also merges each Bilibili upload into the YouTube
  video of the same work: the only one published within six hours whose
  length is within three seconds and whose song title overlaps. Several
  matches, or a YouTube video whose length is not stored yet, leave the
  upload to the operator, and an operator's merge or undo is never changed.
  It runs after the `bilibili` step too, and the merged upload's title
  becomes the video's Chinese translation.

</details>

<details>
<summary><strong>Public pages</strong></summary>

- The profile card shows separate successful-read times for X and YouTube
  and warns when a source is at least 24 hours old, without fetching it.
- Journey adds the first solo album, “ばんび～の”, its original cover and
  the 2011 milestone. The 2022 split EP entry records the MV and cancelled CD
  editions instead of treating their planned date as an actual release.
- English and Chinese readers see X posts and video titles in their
  language when a translation exists. An AI translation is labelled as one,
  and a post's or the featured video's Show original button switches back
  to the source wording; spotlight tickets and video rows mark a translated
  title and show the original on hover.
  Japanese readers always see the source.
- In English and Japanese, the schedule no longer shows Chinese reservation
  text such as `YouTube Live / 预约`.
- A YouTube schedule entry that still carries its video's title shows that
  video's translation, marked like a video row, in the week guide, the month
  grid and the spotlight; an entry with its own wording keeps the source.
  The board, calendar and MCP dashboard events add `translations` and
  `sourceLanguage`. The YouTube Live and YouTube Premiere details read in the
  reader's language.
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
- YouTube videos on the board and in the video archive list their merged
  Bilibili uploads in `alternates` (`source` and `url`), and Chinese readers
  see a merged video's Bilibili title in place of an AI translation (a
  manual or YouTube translation still comes first). The board does not show
  the links yet.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- A new installation names its two starting workflows "Full refresh" and
  "Sources only" instead of Chinese names. Existing workflows keep their
  names; rename them on the Runs & workflows page.
- New settings `BILIBILI_UID`, `BILIBILI_MIN_INTERVAL_MINUTES`,
  `BILIBILI_SESSDATA` (an optional Bilibili login cookie, kept only in the
  environment) and `SKIP_BILIBILI`. New installations include the `bilibili`
  step in both starting workflows; existing workflows need it added on the
  Runs & workflows page.

</details>
