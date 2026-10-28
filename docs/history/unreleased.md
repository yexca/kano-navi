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
