# Unreleased

<details>
<summary><strong>Upgrade notes</strong></summary>

- `GET /api/posts/months` adds `accounts` (`x`, `bilibili`) and
  `accountAvatars`.
- New command `kano-go x-archive` (`import`, `check`, `apply`) imports X
  account exports and reviews the posts an earlier import added; see
  Database operations, X Account Exports. A first import of an account
  outside `X_HANDLES` adds the app setting `x_archive_accounts`.
- Startup adds the empty column `posts.upstream_deleted_at`; no existing post
  is marked. A read-only instance refuses a snapshot without it, so start the
  writable service on the snapshot once to upgrade it.
- The admin API's posts report `upstreamDeletedAt`.
- An event `view.state` may be `ongoing`, in the dashboard, calendar, MCP
  and every other read that carries the view.
- Startup adds the empty tables `alert_incidents` and `alert_deliveries`.
  Operator alerts are on by default and send nothing until a channel is
  added in `/admin` → Alerts; `ALERTS_ENABLED=0` turns them off. The
  outbound proxy pool gains the `notify` area, off until routed.
- The admin overview reports `alerts.open`; new routes under
  `/api/admin/alerts`.
- Startup adds `event_sources.unlisted_at` and the `schedule_asset_reviews`
  columns `previous_manual_status`, `previous_manual_reason`,
  `previous_manual_checked_at`, `check_status`, `check_reason` and
  `check_at`, all empty, to existing snapshots.
- Startup adds the post indexes `posts_account_instant_idx` and
  `posts_bilibili_account_idx` to existing snapshots. A read-only instance
  serves an older snapshot without them, only more slowly.
- A post's public `label` (dashboard, post archive, MCP) is English text:
  `DAILY`, `SCHEDULE` or `ANNOUNCEMENT`. Startup rewrites the stored
  `DAILY / 近况` and `SCHEDULE / 日程` labels once. A configured extraction
  keyword that matched a post only through `近况` or `日程` in its old label
  no longer matches it.

</details>

<details>
<summary><strong>Public pages</strong></summary>

- Posts get a page of their own, `/posts`, in the site menu: every stored
  post in one timeline by month and day, with a card per account and a map
  of every year's months and totals to jump through, in an address that
  names the month and account. The board's **Past posts** opens it in place of the
  month dialog.
- The post archive offers MKLNtic, the unit account Kano shares, as an
  account of its own; its posts stay off the board's wall and latest post.
- A post deleted on X leaves the wall, Past posts, the API and MCP, and its
  schedule image leaves the board and the calendar, as do the automatic
  events it is the only source of. They return when X lists it again.
- A video confirmed on air is the board's next stream and stage ticket even
  when a waiting room without a start time was published earlier; such a
  waiting room comes after every scheduled video.
- A Featured Short reports that it may be the stage's latest video in its
  view, as the stage already treats it.
- MCP `schedule_list` reports the same event state as the board: an ended
  stream is not live in the grace after its start, a waiting room the
  probe saw stays upcoming, and a YouTube premiere on air is live.
- The board shows only snapshots with projection version 1 and takes the
  stage, the on-sale list, the video shelf and every event and video state
  from it, without a local fallback. A snapshot stored by a board from
  before v0.7.0, or one served with another projection version, is not
  shown; the board reads a fresh one.
- An offline event between its start and end is in progress: the week
  keeps its card whole with an **In progress** chip, and the stage shows it
  as happening now, without a countdown or On air, until its end or a
  stream about to start.
- A stream confirmed on air before its scheduled start is live on the board
  and the stage.
- A date-only stream or membership is no longer shown live from midnight to
  3:00 the next day, and the board refreshes when that day ends.
- A premiere past its start with neither an end nor a confirmed broadcast
  leaves the stage as an aired video.
- An event with a cancelled status stays cancelled when a model also
  suspects its cancellation.
- A sale with only an opening date shows its opening on that date rather
  than a closing.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- A new **Alerts** page sends the operator a message through ntfy,
  Telegram or a signed webhook when runs keep failing, a source goes
  unread, the Bilibili login expires or stored secrets cannot be decrypted,
  and another when it recovers. A heartbeat pings an external monitor such
  as Uptime Kuma, so a stopped server is noticed too. Destinations must be
  public addresses, and development mode never sends. Acknowledging an
  open alert takes it off the nav badge until the problem returns.
- The nav groups pages by task: Today, Content, Pipeline and System.
  Queue becomes the Runs page's **Up next** tab, and the Works covers and
  recent uploads move from Site to a **Works** page in System. Older
  `#queue` links open Up next.
- Panels, fields and switches drop their explanatory text and question-mark
  help; the docs describe how each feature works.
- Posts marks a post deleted on X with a **Deleted on X** badge.
- The model provider dialog drops its retries field, which never took
  effect; calls follow the attempts per model sync setting.
- The Review queue lists entries their posts no longer list, with Keep entry
  and Delete entry, and the overview counts them.
- A schedule image card shows its last check and, after an image change, the
  label on the previous image.
- Admin API: events carry review kind `unlisted`; the overview's review
  counts add `unlistedEvents`; schedule images add `needsReview`,
  `previousDecision` and `lastCheck`.
- Admin API: the event an edit, confirmation, merge, unmerge or creation
  returns, and the evidence read's event, carry their `extractionId` as the
  events list does instead of `null`.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- X account exports fill in Kano's own posts with their wording, links,
  quotes and photos, and hide the replies and other people's posts an
  earlier import filed as hers; a slow local check sorts the hidden posts
  and shows her own thread replies again.
- A run whose lease expired after a source failure it tolerated resumes with
  its remaining steps and finishes partial instead of failing.
- An X `count` sync setting and a timeline `count` fetch read the newest posts
  from the head every run instead of continuing from older pages; an `all`
  fetch still continues from its checkpoint.
- An X `count` sync read that no account answers fails with the retryable
  `x_source_failed`, so it is tried again and can be retried like an
  incremental read.
- Retrying a run whose source step failed also re-runs the completed media,
  schedule, translation and reconcile steps after it, since the retried read
  can store new posts with media.
- Finishing or cancelling a run no longer advances the dashboard revision; the
  writes that change public data advance it, including schedule extraction
  that changes a post's events and approved schedule images, so open boards
  reload only for a change.
- An X account whose listing served records its last success even when it
  found nothing new, so a quiet timeline or X API read keeps the source
  fresh.
- A failed refresh of a stored X post no longer fails a quiet run: it is a
  note that leaves the run partial, and the post is not queued.
- An X status stored under another account's handle no longer fails the
  account's run. The post keeps its stored row and is skipped with
  `x_post_owner_conflict`; the rest of the run is written.
- A stored X post whose status answers 404 or 410 twice is marked deleted
  on X instead of staying public; its data, media and events are kept, and
  a later answer that carries it clears the mark.
- An event whose post's evidence moves to another event, such as an entry
  that gains or drops a YouTube link, is retired when it has no other source
  instead of staying on the board without one. Evidence attached to an
  operator's edited or deleted entry stays with it, so a deleted activity no
  longer returns as the video's event.
- A post's entry that links a YouTube video only attaches to the event the
  YouTube sync wrote for that video, adding its stated platforms and label;
  the video's title, times and type stay YouTube's.
- A post read again as no schedule keeps its events and sends them to the
  Review queue instead of leaving them unnoticed.
- A schedule image check that is skipped (an image still caching, an empty
  image queue), running or failed keeps the image's verdict and its place on
  the board, and records the check apart.
- A schedule image that changes under an operator's label keeps that label
  as the previous decision and waits for the operator; a `schedule` label no
  longer approves the new image, a `not_schedule` one keeps hiding it, and
  the period correction stays.
- An ended YouTube broadcast stays ended when a later check reports it
  upcoming or names no state.
- An upcoming yt-dlp entry that names only its release date becomes a
  date-only reservation instead of starting at its publication time.
- A corrupt file at a media content address is replaced by the verified
  download, and a file the media step cannot write records a retryable
  failure instead of stopping the step.
- An X timeline gap keeps the ceiling its opening read reached, so a post
  pinned or fetched inside the gap no longer lets an X API read close it
  before the posts between them are read.
- Schedule images that share one image are checked through a post that
  passes the board-notice gate, so a sibling whose post fails it no longer
  keeps the image from ever being checked.
- A YouTube thumbnail whose revalidation was rejected once (an invalid image,
  an oversized body) is checked again after its video's next watch-page read,
  such as a refresh request, instead of never.
- A post whose input returns to one read before applies that stored result
  to its events again, so its events follow its current input.
- Entries of one post that name the same activity on different platforms
  are one event carrying every platform, not only the last line's.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- `/posts` loads only the board's post wording and its post and month
  stylesheets instead of the whole board copy and `board.css`.
- The board snapshot reads each account's newest posts, the Bilibili
  accounts and the schedule images of posts deleted on X through indexes
  instead of walking every stored post on each read, and looks up the events
  of its active and cancelled videos by identity instead of walking the
  events for every video.
- `/api/dashboard` answers a request at an unchanged revision, a 304
  included, from a prepared response that lasts until the next projection
  transition, a trusted broadcast state going stale or the next minute;
  requests that miss together compute it once. `meta.generatedAt` is the
  time that response was computed.
- The agent guide is a short root `AGENTS.md` with area guides for
  `backend/`, `src/`, `scripts/` and `docs/`, plus `merge-to-main` and
  `release` skills in `.claude/skills/`.
- The large documents (sources, data model, admin API, frontend, public
  API, workflows, media cache, testing, configuration and LINE BLOG archive
  maintenance) are short indexes of topic pages, and `make docs-check` also
  validates heading anchors.
- A pull request that changes `backend/go.mod`, `backend/go.sum`,
  `package.json` or `package-lock.json` also runs the matching Go or npm
  advisory audit, and `PR / Gate` requires it to pass.

</details>
