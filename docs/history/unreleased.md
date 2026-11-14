# Unreleased

> [!IMPORTANT]
> An X detail answer that shows a stored own post to be the account's
> repost of another status (one an archive import stored, say) soft-deletes
> that post: it leaves the board, the post archive, MCP and the console, and
> extraction, translation and refresh skip it, while its row, media links,
> schedule-image candidate, extraction history and translations stay in the
> database. The automatic, unlocked events only it supported are retired and
> a suspected cancellation it raised is withdrawn; manual and locked events
> and confirmed cancellations stay. The repost's ID is then never stored as
> a post again.

<details>
<summary><strong>Upgrade notes</strong></summary>

- The optional `ADMIN_TOTP_SECRET` adds a one-time code (TOTP) to
  production admin login; empty keeps password-only login. `GET /session`
  adds `requiresCode` and `POST /session` accepts `{password, code}`.
  `kano-go totp-secret` prints a value. A malformed value refuses startup.
- Admin adds `POST /posts/refresh` with `{ids}` (1–200 post IDs), which
  queues a refresh from the source for each own X post in one transaction
  and answers `{queued}`; unknown IDs, other sources and promoted statuses
  are skipped.
- `POST /posts/{id}/refresh` answers `400 invalid_input` for a promoted
  related-account status, which the own accounts' synchronization never
  reads.
- Startup adds `posts.deleted_at` (a soft-deleted post) with its index, and
  `x_reposts.retweet_id` and its index, retains every distinct
  repost's own ID in `x_repost_ids`, backfills that table from existing
  repost links, and marks every pooled
  X status an `x_reposts` row names as reposted (`reposted_at` from the
  earliest sighting), without a request.
- A pending X status whose detail answer is the account's repost leaves the
  queue with the new code `x_status_repost` in `pendingDropped`, without a
  failure or retry.
- Every own-account repost a listing or detail answer carries enters the X
  status pool under the `reposted` reason, whatever its author. Only a
  related account's reposted status is translated in the pool; another
  author's is stored untranslated, and an operator translation request for
  it queues nothing, until an operator promotes it.
- `PUT /x-statuses/{id}/decision` accepts `promote` for any reposted status
  of another author, and answers `400 invalid_input` for an own account's
  status; keyword rules still promote related accounts' statuses alone.
- Public dashboard, post archive and MCP posts add `repostedBy`: the own X
  accounts that reposted a promoted post, first sighting first, and `[]` for
  every other post.
- Public dashboard, post archive and MCP post media and quote media add
  `kind`: `photo`, `video` or `gif`.
- Startup adds `media_links.kind` and, once, marks the video and GIF links of
  stored X posts whose `raw_json` types their images, without a request.
  Other links read as photos until their post is refreshed.
- Public API adds `GET /api/journey`, `GET /api/works` and
  `GET /api/works/upload-list`: the Journey and Works pages' data with every
  language and same-host image URLs, from the build and without a revision.
  `GET /api/works/uploads` is unchanged.
- Public API adds the LINE BLOG archive reads `GET /api/lineblog`,
  `GET /api/lineblog/posts/{id}` and `GET /api/lineblog/posts/{id}/comments`:
  the article list and months, one article with its blocks, comment count
  and neighbours,
  and its archived comments, locale-neutral and with same-host media, served
  from data embedded in the release.
- Every LINE BLOG archive video is H.264 with AAC audio: thirteen VP9 and
  AV1 copies are re-encoded under their names, so the media catalog's sizes
  and hashes change. Update `data/lineblog/` from the recovery archive (pull
  it, or copy its `media/` again) before deploying; it no longer splits any
  video.

</details>

<details>
<summary><strong>Public pages</strong></summary>

- Past posts and Past videos keep one header height as you change months or
  scroll through years: the month controls and the filters stay on one row
  wherever the window is wide enough for both. Past posts names its platforms
  above the title instead of every account, and both dialogs show their total
  only on the filter's buttons.
- Changing months in Month view and Past posts no longer collapses the
  dialog while the month loads: the calendar keeps its dates in place and
  Past posts shows outlines of its cards, on phones as well. A month without
  entries says so beside the calendar (below it on phones) rather than above
  it.
- Month view jumps to any month: select the month name to pick a year and
  month, as in Past posts.
- After a jump to an earlier month in Past videos, Show newer videos loads
  the next newer videos above the list, keeping what you were looking at in
  place, instead of returning to the newest.
- A post's or quoted post's video or GIF shows its still with a play mark or
  a GIF tag, on the board, in the post archive and in the image viewer, and
  opens the post on X instead of the viewer.
- Every video in the LINE BLOG archive plays in Safari and on iPhone, not
  only those YouTube had kept as H.264.
- In Safari and on iPhone, the weekly schedule folds past entries to two
  lines of title and cuts other entries' titles at three lines, as in other
  browsers, instead of showing every title whole.
- In the LINE BLOG menu bar's list of official sites, the site under the
  pointer or keyboard focus shows its text in the highlight's colour, as the
  design picker does.
- A reposted post the operator picks shows on the wall and in Past posts as
  its author's post, marked Reposted by and the account of Kano's that
  reposted it, under All only.
- About opens with a one-line introduction and lists the apps and sites that
  show the board: 小鹿包们的宝物 for Android, iOS and HarmonyOS, Kano Navi
  for iOS (in development) and the mainland China server site. Credits and
  the technologies, AI models included, each take one section, and the
  project links, version and author share one row.
- Notes in the footer opens one notice, the same on every page, in a
  dialog instead of unfolding the page's own note in the footer; About
  shows the same notice.
- Past posts' previous and next month links skip months whose posts are all
  hidden, and its All filter counts every post of the month, picked reposts
  included.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- The login page asks for a one-time code beside the password when the
  deployment sets one, which password managers such as 1Password fill in.
- Every own X post in Posts has Refresh from source in its actions, with a
  Refresh queued badge, whether or not it quotes a status; the quote card
  keeps only its hide and open actions.
- Posts selects own X posts on a page and queues their refresh from the
  source in one request; Review's extraction lists offer the same bulk
  refresh beside Process again.
- External posts lists every status an own account reposted under Reposted,
  with the reposting accounts, and promotes or demotes any of them by hand
  with a reason, as a lock. A promoted repost is translated as a post.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- A vxtwitter detail answer that carries `retweet` and explicitly matches
  the requested outer status ID is recorded as the
  account's repost of the original status and never stored as a post, on
  the pending queue, known-post refreshes and operator refreshes alike. Such
  an answer whose reposted status has no usable ID fails as an invalid
  answer and keeps the stored post.
- X API answers record each repost's own status ID, so a stored post under
  it is soft-deleted in the same way. Repeated reposts of one original retain
  every own ID, including when one API page carries several, so later
  synchronization and imports cannot restore them as posts.
- Each X image records whether it is a photo or a video's or GIF's
  thumbnail, from the detail answer, the timeline and the X API alike. Video
  and GIF files are never downloaded.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- `npm run lineblog:export` writes the server's copy of the LINE BLOG
  archive from the tracked page data, and the frontend tests fail when the
  committed copy differs, allowing Git checkout line endings.
- HTML revalidates with an ETag over its rendered content, including page
  tags and runtime CDN/public-origin configuration. A build timestamp alone
  cannot produce a 304 that keeps an earlier configuration's addresses.
- LINE BLOG packaging refuses a video that is not H.264 with AAC audio.
- Profile images load when the data directory is reached through a symbolic
  link, as macOS reaches its temporary directories; a symbolic link in place
  of the avatar directory is still refused.
- `CDN_PUBLIC_BASE` optionally serves the build's assets and cached media
  from a CDN. It is empty by default, changes no stored data, and must be
  passed to the frontend build as well; see [CDN](../operations/cdn.md).

</details>
