# ADR-0001: SQLite-First Snapshot Architecture

- Status: Accepted
- Date: 2026-08-28

## Context

The page needs to show X posts, future events, and YouTube reservations together.
Fetching external platforms on every page view introduces rate limits, login
walls, changing structures, and non-repeatable page state. It also makes it
hard for maintainers to tell when data was last refreshed.

## Decision

Use server-side synchronization, a SQLite snapshot, and a read-only frontend API:

1. `scripts/sync.mjs` reads and normalizes public data source by source.
2. `server/database.js` stores snapshots and synchronization results with transactions and upserts.
3. `server/index.js` returns stable dashboard and health data only.
4. The React page never calls external platforms directly; when synchronization fails, it continues to show the last usable snapshot.

## Consequences

Benefits include controlled request frequency, offline-friendly page reads,
explicit failure behavior, and structured data that can be inspected and
extended. The trade-off is that data is not real-time: a scheduler must run the
synchronization script, and parsers must be maintained as external platforms
change.

## Alternatives Considered

- Browser-side real-time fetching would increase CORS, rate-limit, and privacy risks and would not provide a stable historical snapshot.
- A remote SaaS database would be excessive for a single-instance fan-made board and would add credential and deployment boundaries.
- Static JSON alone would be easy to publish but would provide weaker query, synchronization-history, and future-extension constraints.
