# Unreleased

<details>
<summary><strong>Public board</strong></summary>

- The X posts section always has at least the eight newest posts, however
  old, so a quiet three-day window no longer leaves the wall half empty. The
  wall shows whole rows only: up to six posts or two rows, whichever is more,
  and fewer posts drop the short last row (five posts in two columns show
  four, five in three columns show three). The profile's post count still
  counts the three-day window, and the section subtitle reads "Latest posts"
  instead of naming the window.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- A pending X status that turns out to be another account's (such as a quote
  an older profile fallback queued) is dropped on its first answer as
  `pendingDropped` with `x_status_foreign`, instead of an `account_mismatch`
  error, a retry and a partial run. Profile fallbacks now queue only the
  account's own `/<handle>/status/<id>` links.

</details>
