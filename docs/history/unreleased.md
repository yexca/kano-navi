# Unreleased

> [!IMPORTANT]
> **Upgrade notes.** No environment variables were added or removed.
> Startup adds the `source_schedules.chain_json` and
> `workflow_runs.source_chain_json` columns; nothing is migrated.
>
> - Each X and YouTube timer reads through a method chain. Until one is saved
>   on the Sources page, X tries the FxTwitter timeline, then the profile page,
>   and leaves the X API off; YouTube tries the Data API (with a key), the RSS
>   feed, then the uploads tab.
> - To let X fall back to the official API during FxTwitter outages, enable
>   `api` in the X chain. It needs `X_API_BEARER_TOKEN` and runs only after
>   three consecutive timeline failures of an account, for at most 100 posts
>   a Japan day; both are settings in the X API panel of the Sources page.
> - `GET /api/admin/sources` adds `chain` to each schedule, and
>   `PUT /api/admin/sources/{source}/schedule` accepts it.
> - Manual X API fetches read quoted statuses and skip reposts, and their
>   posts count toward the X API daily usage.
> - `POST /api/admin/runs` with `kind: "fetch"` requires a `method`
>   (`timeline` or `api` for X, `api` or `rss` for YouTube) and accepts
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

<details>
<summary><strong>Operator console</strong></summary>

- The manual fetch on the Sources page picks a method first, then what it
  reads, the count or dates, and the steps that follow: the FxTwitter timeline
  (newest posts, or every page to the end of the timeline), the X API (newest
  posts, a date range, or a backfill before the oldest post), the YouTube Data
  API (newest uploads, or the whole uploads playlist) or the YouTube RSS feed
  (newest 15). Methods the server cannot run show the variable they need.
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

- A new LINE BLOG page brings back Kano's 2015–2019 archive with 99 Japanese
  articles, their photos, pictographs and archived YouTube videos, the
  original-era layouts, monthly archive and article links. A menu-bar picker
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
- Switching weeks in the weekly schedule keeps the current week dimmed until
  the next one is ready and then changes once, instead of passing through an
  empty grid and a loading bar; the schedule-image slot keeps its size on wide
  screens.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- Release images are published for `linux/arm64` as well as `linux/amd64`
  under the same tags, so ARM hosts such as a Raspberry Pi 4/5 or an Apple
  silicon Mac pull a native image. The Dockerfiles cross-compile the Go
  backend for the target platform.
- `npm run lineblog:package -- --from <archive>` writes the article index and
  media catalog into `src/lineblog/` and media into `data/lineblog/`, with
  validation before writing. The page loads only the articles it shows.
- LINE BLOG and fixed history images get on-demand smaller copies through
  `srcset`; hash-addressed originals and variants are cached as immutable
  after byte verification. The build emits Brotli and gzip copies of static
  files, and the server serves the best accepted encoding.
- The board is its own lazy-loaded chunk, while `/` preloads it alongside the
  entry; direct visits to `/history`, `/lineblog` and `/about` avoid loading
  board code.

</details>
