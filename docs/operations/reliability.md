# Reliability and Freshness

## Failure and Freshness

- Each run starts a `sync_runs` record and finishes with `success`, `partial`, or `failed`.
- A source failure does not clear another source or any existing snapshot; errors are available for maintainer diagnosis.
- A media failure never downgrades an existing ready file. An invalid LLM result
  never replaces existing events.
- `meta.lastSync` is the latest completed run whatever its outcome, and the
  board shows its status and time as the overall state; with no run it is
  `null`, and the page shows “Not synced yet” instead of a time or healthy
  state. `meta.fetchedAt` is the latest completed `success` or `partial` run's
  time, so a failed attempt changes `meta.lastSync` but retains it. Profile
  dates, post/video publication dates, and API reads do not count as
  synchronization. This is synchronization freshness, not the latest message's
  publication time; original platform pages remain authoritative.
- `meta.sourceFetchedAt` reports X, YouTube and Bilibili separately. Only
  successful incremental, recent-window or latest-count reads, and Bilibili's
  successful account reads, advance those times;
  historical backfills and media-only runs cannot make old sources look fresh.
  Recent-window timestamps never exceed the window's end. Timeline windows
  keep the successful head-page read time across resumed pages; clearing a
  failed cursor does not count as a successful read. The board warns after
  24 hours using its local clock and keeps the stored snapshot visible.
- Media retries persist across restarts: transient failures back off from
  30 minutes to at most 24 hours, while missing files and other HTTP errors
  wait 24 hours. A failed revalidation keeps the ready file and respects the
  same deadline; successful bytes or a 304 clear the retry state.
- CI never performs live synchronization, so builds do not depend on platform networks, login walls, or rate limits.
- The board retries even when its first API read fails, and retains a known
  snapshot after subsequent failures. Requests time out after 15 seconds;
  completion-based polling backs off to at most 60 seconds during an outage.
  Hidden pages pause polling and check promptly on return without bypassing
  outage backoff. Refresh and revision checks only read local SQLite.
  See [Frontend request ownership](../architecture/frontend.md#request-and-ui-ownership).

## Process Lifetime

Source schedules execute only inside the API process. The single-flight
queue runs one job at a time; a busy queue leaves a due source due for a
later tick. Restarting the server invalidates in-memory admin sessions.
Inspect protected run details before retrying; historical source checkpoints
persist independently of normal incremental cursors.

## Related Docs

- [Workflow scheduling](../architecture/workflows.md)
- [Database backup](database.md)
- [Troubleshooting](troubleshooting.md)
