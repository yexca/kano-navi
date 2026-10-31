# Unreleased

> [!IMPORTANT]
> **Upgrade notes.** `YTDLP_BINARY` is optional. Set it to a fixed yt-dlp
> executable path to enable the method; leaving it empty preserves the
> existing chain behavior.
> Startup adds the `source_schedules.chain_json` and
> `workflow_runs.source_chain_json` columns; nothing is migrated.
>
> - Each X and YouTube timer reads through a method chain. Until one is saved
>   on the Sources page, X tries the FxTwitter timeline, then the profile page,
>   and leaves the X API off; YouTube tries the Data API (with a key), the RSS
>   feed, the configured yt-dlp method when enabled, then the uploads tab.
> - To let X fall back to the official API during FxTwitter outages, enable
>   `api` in the X chain. It needs `X_API_BEARER_TOKEN` and runs only after
>   three consecutive timeline failures of an account, for at most 100 posts
>   a Japan day; both are settings in the X API panel of the Sources page.
> - `GET /api/admin/sources` adds `chain` to each schedule, and
>   `PUT /api/admin/sources/{source}/schedule` accepts it.
> - Manual X API fetches read quoted statuses and skip reposts, and their
>   posts count toward the X API daily usage.
> - `POST /api/admin/runs` with `kind: "fetch"` requires a `method`
>   (`timeline` or `api` for X, `api`, `rss` or `ytdlp` for YouTube) and accepts
>   `options`. The `recent` mode and YouTube date windows (`range`, `before`
>   and YouTube `recent`) are removed; X date ranges and backfills need the X
>   API method, and the whole YouTube uploads playlist needs the YouTube API
>   method.
> - `GET /api/admin/sources` reports each source's `methods` in place of
>   `historyAvailable` and `windows`.
> - Runs stored with a fetch window from an earlier version keep reading in
>   the run history; running one again fails its fetch step with "this fetch
>   uses a retired selection; start a new fetch".
> - A manual fetch whose method fails fails the run, and the steps after it do
>   not run.
> - Read limits are console settings: `X_MAX_STATUS_REQUESTS`,
>   `X_REFRESH_KNOWN`, `X_BOOTSTRAP_DAYS`, `X_DISCOVERY_LIMIT`,
>   `YOUTUBE_MAX_DETAIL_REQUESTS`, `SOURCE_HISTORY_MAX_PAGES`,
>   `SYNC_TIMEOUT_MS`, `SYNC_REQUEST_DELAY_MS`, `TRANSLATION_SCAN_LIMIT` and
>   `TRANSLATION_LOOKBACK_DAYS` are stored once on the first start that sees
>   them and then ignored, so remove them from `.env` after that start.
>   `YOUTUBE_BOOTSTRAP_VIDEOS` had no effect and is no longer read.
>   `GET`/`PUT /api/admin/sync-settings` read and change them.

<details>
<summary><strong>Operator console</strong></summary>

- The Sources page has X and YouTube read limits (status requests, known-post
  refreshes, first-run age, full-sync cap, video pages, manual fetch pages,
  request timeout and spacing), and the LLM page has the translation limits.
  Saved values apply from the next run.

- The manual fetch on the Sources page picks a method first, then what it
  reads, the count or dates, and the steps that follow: the FxTwitter timeline
  (newest posts, or every page to the end of the timeline), the X API (newest
  posts, a date range, or a backfill before the oldest post), the YouTube Data
  API (newest uploads, or the whole uploads playlist), the YouTube RSS feed
  (newest 15) or yt-dlp (bounded metadata listing). Methods the server cannot
  run show the variable they need.
- A fetch never falls back to another method: a method that fails fails the
  run with its code.
- Options run schedule extraction (X) or translation after the fetch, or
  refresh stored YouTube videos the listing names again.
- The newest fetch of a source shows under the form: what the method listed,
  how many records were new, already stored or failed, the pages it read,
  whether it reached the end (or should be repeated to continue), and the
  first new records.
- The Sources page has a tab per source. Under it, the selected source's
  timer switch and method chain sit on the left, its settings beside them and
  its manual fetch below the settings; on a phone they stack in that order. In
  the chain, switch methods on or off and move them up or down; changes save
  at once. Methods the server
  cannot run show the variable they need, a source a `SKIP_*` setting turns
  off shows as locked, and Bilibili shows its one fixed method.
- The settings of X add an X API panel: the gate and daily cap, where the
  chain has the method, today's usage and each account's timeline failure
  streak. YouTube's settings group push, the live-status probe and the
  follow-up checks of finished videos (1 hour, 1 day, 3 days and 7 days) as
  its progressive confirmation.
- The Schedule page has a calendar view beside its list: a month grid whose
  entries are chips styled by review state, where a chip opens the entry and
  a day shows its entries below the grid. The list groups entries under a
  heading per Japan date with its count and a button that adds an entry on
  that day, with Upcoming, Past and All dates tabs beside the date range.
  The filters are a search field and All, Not confirmed, Suspected and
  Deleted tabs. Clicking a title opens the entry, unconfirmed entries show a
  labelled Confirm button, and the entry dialog shows when the entry last
  changed.
- The Videos page lists videos as compact rows with a search field and All,
  Upcoming, Bilibili and Missing translation tabs; uploads merged into
  another video appear on that video's row. Clicking a title opens the
  correction dialog, which previews the chosen thumbnail and keeps Save in
  its footer.
- `GET /api/admin/videos` accepts `search`, `merge` and `translation`, and
  `GET /api/admin/events` accepts `provenance=unconfirmed` and
  `deleted=only`.
- Run results name the method that served each X account or the YouTube
  channel and every method tried with its code.
- A run whose media step left images queued explains how many remain, and the
  step's counts list them as Remaining with the bound that stopped it.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- YouTube fetches list the uploads playlist (one quota unit per 50 uploads)
  or the RSS feed instead of searching dates with `search.list` (100 quota
  units a page).
- A YouTube Data API fetch checks the details of the videos it lists that are
  not stored yet (all of them with refresh) through `videos.list`, under the
  recent step's rules: length, stream or premiere verdict, broadcast times
  and status, reservation events and localized titles. The detail queue is
  checked in batches of 50, at most five calls per run, so a 200-video count
  window is classified in one run. A failed call records its controlled
  `youtube_api_` code and ends the lookup; the IDs it would have covered fall
  back to watch pages. Older uploads beyond the regular listing get them too:
  a whole-playlist fetch reads the channel's live and long-form playlists to
  their end (up to 1,000 entries each, once per run) to tell old streams from
  premieres. It spends about two quota units per 50 new uploads, plus one per
  50 entries of each format playlist when an ended broadcast needs them; a
  failed details call leaves the fetch partial and keeps what it listed.
- A watch page that asks the server to sign in (YouTube's bot check) records
  `youtube_watch_login_required` once and stops further watch page requests
  for that run, in recent sync and the live-status probe alike.
- An X API fetch can read each account's newest posts, and leaves stored posts
  as they are, like a timeline fetch, so their extraction fingerprints stay.
- Timed reads follow each source's method chain and fall to the next method
  when the first page fails, when it lists none of the account's own records
  or on quota and rate limits.
- The X chain can read new posts through the X API's user timeline, newer
  than the stored cursor, and close timeline gaps exactly. Posts read this
  way are stored as the timeline and detail answers store them: long text,
  expanded links, images, quotes and counters.
- Turning the Data API off in the YouTube chain stops every Data API call of
  timed reads, the live-status probe included; watch pages take over.
- The media step downloads the whole image queue in batches, up to 500 files
  or five minutes per run. A 429 answer ends the step early. The step records
  how many never-checked images remain and which bound stopped it, and finishes
  partial while images remain. `MEDIA_DOWNLOAD_LIMIT` is removed from
  `.env.example` and the configuration page.

</details>

<details>
<summary><strong>Public pages</strong></summary>

- The language menu has an interface language and a separate content
  language for video and schedule titles, posts and LINE BLOG translations.
  Content follows the interface by default, so existing visitors see no
  change until they pick one.
- Every translation switch is one translate icon with **AI**, lit while the
  translation shows and off while the original does, in place of the
  Original label and the origin line with Show original. Posts carry it after
  their last word, and the latest video's heading carries it too.
- The Overview console has an off-by-default switch for a localized mainland
  China site suggestion when the reverse proxy reports that country.
- A new LINE BLOG page brings back Kano's 2015–2019 archive with 99 Japanese
  articles, their photos, pictographs and archived YouTube videos, the
  original-era layouts, monthly archive, the blog's own 前の記事/次の記事
  pager, and each article's archived comments with names, dates, text and
  ratings but without the commenters' links. Phones get the blog's own
  smartphone template: a sideways menu bar, a one-line profile with a popup,
  an article list, round page buttons and a monthly pull-down. A menu-bar
  picker
  keeps one era's header art and profile photo in place of the automatic one.
  English and Simplified Chinese readers can show or hide the AI paragraph
  translations for every article or one article, and the About page credits
  the fan recovery project.
- Today's weekly schedule row uses a berry date disc and accent rule on
  phones and tablets, while the site header shows the name with an
  `Unofficial` mark on wide screens.
- The board's sync status shows the latest collection run's outcome and time,
  and lists the last successful read of X, YouTube and bilibili separately,
  each marked out of date after a day.
- Past posts can be read row by row in a single-column view, with the device
  remembering the choice; the month dialogs are larger on wide screens.
- Posts on X always lists the eight newest posts of each account, so the
  account filter fills the wall for a quiet account too.
- A quote of one of the board's own posts shows that post's translation in
  the content language, with its own **AI** switch at the end of the quote's
  header line.
- The header names the blog page LINE BLOG, and its About link reads 关于 in
  Chinese and について in Japanese. The About page drops its tagline and the
  Full journey link, folds the LINE BLOG note into the overview, and lists
  the models as Claude Opus 5.5, GPT-6.1-Sol and GPT-5.6-Sol.
- Switching weeks in the weekly schedule keeps the current week dimmed until
  the next one is ready and then changes once, instead of passing through an
  empty grid and a loading bar.
- The weekly schedule shows its schedule-image strip only for a week that
  has a schedule image, without a "No schedule image for this week" note.
- The weekly schedule, Posts on X and the month dialogs drop their one-line
  notes under the title, and the video archive names bilibili beside YouTube.
- In the month view, a click anywhere in a day's cell opens its details, and
  a button hides them so the grid takes the full width. On phones, tapping a
  day with dots shows its entries instead of opening an entry's link, and the
  Month view button keeps its label.
- Images on the board, the Journey page and the LINE BLOG show the same zoom
  cursor, and the image viewer keeps its close button in the top-right corner
  of the screen and closes on a click anywhere outside the image, its arrows
  and its details.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- `.env.example` and the configuration reference drop `X_HANDLE`,
  `X_SCHEDULE_REFRESH_LIMIT`, `MEDIA_DOWNLOAD_TIMEOUT_MS` and
  `MEDIA_MAX_BYTES`, which the server does not read; existing `.env` entries
  for them have no effect and can be deleted.
- Release images are published for `linux/arm64` as well as `linux/amd64`
  under the same tags, so ARM hosts such as a Raspberry Pi 4/5 or an Apple
  silicon Mac pull a native image. The Dockerfiles cross-compile the Go
  backend for the target platform.
- `npm run lineblog:package -- --from <archive>` writes the article index and
  media catalog into `src/lineblog/` and media into `data/lineblog/`, with
  validation before writing. The page loads only the articles it shows.
- `npm run lineblog:comments -- --from <captures>` writes each article's
  comments from local Wayback captures of its page into
  `src/lineblog/comments/`, keeping the capture with the most comments and
  dropping commenter links; an article view loads only its own comments.
- LINE BLOG and fixed history images get on-demand smaller copies through
  `srcset`; hash-addressed originals and variants are cached as immutable
  after byte verification. The build emits Brotli and gzip copies of static
  files, and the server serves the best accepted encoding.
- The board is its own lazy-loaded chunk, while `/` preloads it alongside the
  entry; direct visits to `/history`, `/lineblog` and `/about` avoid loading
  board code.

</details>
