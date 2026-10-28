# Unreleased

<details>
<summary><strong>Public pages</strong></summary>

- The X posts section always has at least the eight newest posts, however
  old, so a quiet three-day window no longer leaves the wall half empty. The
  wall shows whole rows only: up to six posts or two rows, whichever is more,
  and fewer posts drop the short last row (five posts in two columns show
  four, five in three columns show three). The profile's post count still
  counts the three-day window, and the section subtitle reads "Latest posts"
  instead of naming the window.
- A post that quotes another X status shows it as a nested card below its
  own wording and images: the quoted author, time, wording (clamped on the
  board, whole in the full post and the archive) and small thumbnails that
  open the image viewer. The card links to the quoted post on x.com, and a
  deleted or withheld quote shows a "Quoted post unavailable" placeholder.
- A week without schedule entries no longer flickers to the loading
  placeholder on each background refresh; the placeholder appears only while
  a week is first loading.
- `/api/dashboard` and `/api/calendar` send a weak ETag with
  `Cache-Control: no-cache` instead of `no-store`, and the board revalidates
  them, so an unchanged board answers its minute refresh with a bodiless 304.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- Models & detection is split into two views. **Detection** holds the stage
  switches and the keyword list, edited as chips (Enter or a comma adds one,
  Backspace removes the last), and shows which model each stage calls first.
  **LLM** holds the schedule and translation queues side by side and the
  providers as a list beside the selected provider's models. Queue changes
  save at once; models are added from a searchable picker and reordered by
  dragging or the arrows, and each queue marks the model a stage uses first.
  The provider dialog holds only the connection; a new provider with a key
  lists its models straight away. Old `#models` links open LLM.
- Runs lists its history first; **Run steps once** opens a dialog grouped
  into sources and processing, with steps disabled in the process greyed
  out. An opened run starts with what went wrong, step by step, in words,
  then shows each step's counts as labelled numbers, failed counts
  highlighted and per-attempt model calls in a table.
- Sources starts with one card per source: its timer state, next and last
  run, last successful read, the timer switch and **Run now**. Selecting a
  card shows that source's timer, window fetch and YouTube push and live
  settings.
- Schedule and Posts filters are labelled pills with one date range and a
  clear button that counts the active filters.
- The schedule entry dialog picks the type from a segmented control and
  groups the date, start and length in one card with an **At a time** /
  **All day** switch. A new entry starts timed and needs a start time;
  **All day** saves a date-only entry. The length defaults to the type's
  calendar length (an hour for streams and memberships, half an hour
  otherwise) without saving an end, and presets or a custom end save one.
  A preview line shows how calendar subscribers will see the entry. The
  detail box grows with its text and counts characters toward the limit. The
  status note and cancellation fold away and open on their own when the
  entry already uses them; save and cancel stay pinned to the dialog's foot.
- Translation chips open the editor on the clicked language; the editor
  shows the original beside one language at a time.
- Video cards keep their actions on one row at the foot of each card.

- The post list shows a post's quoted post read-only in its own cell, apart
  from the post's images: author, time, wording, cached thumbnails or the
  unavailable state. An operator can hide the quote from the public board,
  archive and MCP and show it again (`PUT /api/admin/posts/{id}/quote/hidden`);
  synchronization never overrides that decision. **Refresh from source**
  (`POST /api/admin/posts/{id}/refresh`) queues the post for a detail request
  in the next X synchronization, even when a timeline page lists it or a
  failure backoff holds it, and the list shows the queued refresh.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- A pending X status that turns out to be another account's (such as a quote
  an older profile fallback queued) is dropped on its first answer as
  `pendingDropped` with `x_status_foreign`, instead of an `account_mismatch`
  error, a retry and a partial run. Profile fallbacks now queue only the
  account's own `/<handle>/status/<id>` links.
- A quoted status's images are no longer merged into the quoting post's own
  images. They are stored as the post's `quote-image` media beside a
  sanitized `posts.quote_json` projection, and are never schedule
  candidates or extraction input. The first start fills the projection for
  stored posts from their `raw_json` and moves quoted images that earlier
  releases linked as the post's own.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- GitHub Actions validation is split into the `PR` and `Full` workflows,
  which share the jobs in `validate.yml`. A pull request runs only the jobs
  its changed paths reach, and the single `PR / Gate` check decides whether it
  may merge. Every push to `main` and every release tag still runs every job,
  reported as `Full / Gate` and `Verify release / Gate`. Branch rulesets that
  required checks of the `CI` workflow must require `PR / Gate` instead.

</details>
