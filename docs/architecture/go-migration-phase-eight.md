# Go migration: phase eight

Go admin reads now use the same sanitized session, pagination and event fields
as TypeScript. The canary session cookie is `kano_go_canary_session`; it is
deliberately separate from the TypeScript `kano_admin_session` cookie. Sessions
remain in memory and passwords are never stored in SQLite.

The Go service remains production-read-only by default. `-enable-admin-writes`
is accepted only with explicit `APP_MODE=development`; it opens an existing
synthetic database with SQLite `mode=rw`, performs no migrations or seeding, and
is not a production route. Production admin, synchronization, LLM and workflow
ownership remains in TypeScript. Mutation transactions update the event,
`event_sources`, and `dashboard_revision` together; a failed step rolls back.

Login rejects malformed JSON, duplicate keys, trailing content, wrong field
types, and bodies over the bounded limit. Password length follows TypeScript's
trimmed UTF-16 semantics. Rollback is to stop the Go test process and discard
the synthetic fixture; no production snapshot is modified.
