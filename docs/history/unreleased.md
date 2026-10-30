# Unreleased

> [!IMPORTANT]
> **Upgrade notes.** No environment variables were added or removed.
> Startup adds the `source_schedules.chain_json` and
> `workflow_runs.source_chain_json` columns; nothing is migrated.
>
> - Each X and YouTube timer now reads through a method chain. Until you save
>   one on the Sources page, timers read as before: X tries the FxTwitter
>   timeline, then the profile page, and leaves the X API off; YouTube tries
>   the Data API (with a key), the RSS feed, then the uploads tab.
> - To let X fall back to the official API during FxTwitter outages, enable
>   `api` in the X chain. It needs `X_API_BEARER_TOKEN` and runs only after
>   three consecutive timeline failures of an account, for at most 100 posts
>   a Japan day; both are settings in the X API panel of the Sources page.
> - `GET /api/admin/sources` adds `chain` to each schedule, and
>   `PUT /api/admin/sources/{source}/schedule` accepts it.
> - Manual X API fetches now read quoted statuses and skip reposts, and their
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
> - A manual fetch whose method fails now fails the run, and the steps after
>   it do not run.

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
- The Sources page has a rail with each source's timer switch and method
  chain, the selected source's settings beside it and its manual fetch below
  them; on a phone they stack in that order. In the rail, switch methods on or
  off and move them up or down; changes save at once. Methods the server
  cannot run show the variable they need, a source a `SKIP_*` setting turns
  off shows as locked, and Bilibili shows its one fixed method.
- The settings of X add an X API panel: the gate and daily cap, where the
  chain has the method, today's usage and each account's timeline failure
  streak. YouTube's settings group push, the live-status probe and the
  follow-up checks of finished videos (1 hour, 1 day, 3 days and 7 days) as
  its progressive confirmation.
- Run results name the method that served each X account or the YouTube
  channel and every method tried with its code.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- YouTube fetches list the uploads playlist (one quota unit per 50 uploads)
  or the RSS feed instead of searching dates with `search.list` (100 quota
  units a page).
- A YouTube Data API fetch checks the details of the videos it lists that are
  not stored yet (all of them with refresh) through `videos.list`, under the
  recent step's rules: length, stream or premiere verdict, broadcast times
  and status, reservation events and localized titles. Older uploads beyond
  the regular listing get them too: a whole-playlist fetch reads the
  channel's live and long-form playlists to their end (up to 1,000 entries
  each, once per run) to tell old streams from premieres. It spends about two
  quota units per 50 new uploads, plus one per 50 entries of each of those
  playlists when an ended broadcast needs them; a failed details call leaves
  the fetch partial and keeps what it listed.
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

</details>
