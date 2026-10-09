# Reliability and Freshness

## Failure and Freshness

- Each run starts a `sync_runs` record and finishes with `success`, `partial`, or `failed`.
- A source failure does not clear another source or any existing snapshot; errors are available for maintainer diagnosis.
- A media failure never downgrades an existing ready file. An invalid LLM result
  never replaces existing events.
- `meta.fetchedAt` is the latest completed `success` or `partial` synchronization
  time. With no such run it is `null`, and the page shows “Not synced yet” instead
  of a timestamp or healthy snapshot. Profile dates, post/video publication
  dates, and API reads do not count as synchronization. A failed attempt changes
  `meta.lastSync` and the warning state but retains the previous snapshot time.
  This is synchronization freshness, not the latest message's publication time;
  original platform pages remain authoritative.
- CI never performs live synchronization, so builds do not depend on platform networks, login walls, or rate limits.
- The board retries even when its first API read fails, and retains a known
  snapshot after subsequent failures. Requests time out after 15 seconds;
  completion-based polling backs off to at most 60 seconds during an outage.
  Hidden pages pause polling and check promptly on return without bypassing
  outage backoff. Refresh and revision checks only read local SQLite.
  See [Frontend request ownership](../architecture/frontend.md#request-and-ui-ownership).

## Process Lifetime

Saved workflow timers execute only inside the API process. The single-flight
queue runs one job at a time; a busy queue leaves a timed workflow due for a
later tick. Restarting the server invalidates in-memory admin sessions.
Inspect protected run details before retrying; historical source checkpoints
persist independently of normal incremental cursors.

## Related Docs

- [Workflow scheduling](../architecture/workflows.md)
- [Database backup](database.md)
- [Troubleshooting](troubleshooting.md)
