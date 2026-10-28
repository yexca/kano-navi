# Unreleased

> [!NOTE]
> The admin API adds `POST /api/admin/events/{id}/merge` with `{into}`, and
> the `reconcile` step result adds the `eventsTimed` and `eventsCreated`
> counts.

<details>
<summary><strong>Public pages</strong></summary>

- The header shows the avatar beside the site name on phones too.
- The calendar subscription `/api/calendar.ics` names no time zone
  (`X-WR-TIMEZONE` is gone) and marks all-day entries with
  `X-MICROSOFT-CDO-ALLDAYEVENT:TRUE`, so Outlook shows an all-day entry on
  its one day instead of across two.
- The **Subscribe** menu lists only its two actions, without the Japan-time
  and hourly-update note.
- The schedule header puts **Subscribe** and **Month view** first and the week
  arrows with **Today** last, set slightly apart. On phones those two, **Past
  posts** and **Past videos** shrink to icons so each section's controls fit
  on one row, and the video filter drops its counts.
- The video section no longer has an **Open channel** button; the profile
  card links the channel.
- Stream archives on phones are two rows of two instead of a sideways row.
- The profile card links both X accounts by handle, and YouTube and bilibili
  by icon.
- bilibili links use the bilibili television mark on the board and in the
  operator console.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- A schedule entry can be merged into another entry from the day before to
  the day after; automatic merging keeps the decision, and the list can undo
  it.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- A schedule entry for a linked YouTube video takes the video's start or
  publication time, so an X notice of the same Short or upload merges into
  it.
- An X notice that names no activity merges into the YouTube stream starting
  within 30 minutes of its exact time, and an X release notice into the
  YouTube release.
- An ended broadcast the watch page could not classify gets its schedule
  entry when it lasted at least 30 minutes.

</details>
