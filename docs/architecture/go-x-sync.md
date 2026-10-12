# Go X recent sync vertical slice

Phase ten adds one development-only writable path beside the TypeScript owner:
`POST /api/admin/sources/x/fetch` with `{ "window": { "mode": "recent", "days": 7 } }`.
It requires a Go admin session, `APP_MODE=development`, and the explicit
`-enable-x-sync` flag. The response is `202` with the durable workflow run;
`GET /api/admin/runs/:id` exposes the run, step and attempt projections.

The slice reads public profile HTML from `x.com/<handle>` and detail JSON from
the allowlisted `api.vxtwitter.com/<handle>/status/<id>` path. The adapter has
an injectable `http.RoundTripper`, fixed timeout and User-Agent, 4 MiB response
limit, redirect rejection, typed error codes and no credential-bearing URLs.
Tests use only fake transports.

A successful detail response is normalized and committed in one SQLite
transaction to `posts`, pending `media_assets`/`media_links`, schedule-board
`assets`/`schedule_asset_reviews`, and the account's `sync_state`. Repeated IDs
are upserts. Failed detail calls retain old posts and cursors while discovered
IDs remain in `pendingStatusIds`; historical/window state is never changed.
Media is registered as pending and is never downloaded. Schedule candidates
must pass the source-text gate and remain `pending`/`unreviewed`; no LLM runs.

The Go scheduler remains disabled. `-enable-x-sync` is rejected outside
`APP_MODE=development`; use a disposable development database and never let Go
and TypeScript write the same file concurrently. Rollback is simply to stop Go,
remove the flag, and continue TypeScript synchronization.

This phase deliberately leaves X `before`/`range`, X archive search, YouTube,
media downloading, LLM extraction, MCP control, frontend changes and production
route cutover as `capability_not_migrated`.
