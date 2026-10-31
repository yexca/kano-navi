# Database and Maintenance

SQLite and dynamic media live under the ignored data/ directory, together with
the packaged LINE BLOG archive media in data/lineblog/. Static history
images ship in public/assets/history/ and are included in the image. A fresh
installation starts with static profile, timeline, and resources, plus empty
source snapshots and LLM provider/model routes.

## Seed and Synchronization

```bash
make seed                    # Idempotently add missing static records
npm run seed -- -overwrite   # Restore the shipped profile, timeline and resources
make sync                    # Update the snapshot from public sources once
```

These run `kano-go seed` and `kano-go sync`. In a container deployment use
`docker compose run --rm kano seed` (or `sync`, `migrate-llm`), which runs the
command against the mounted `./data`. Normal operation does not need them:
the service seeds at startup, and synchronization is started from `/admin`,
MCP or a source timer. A one-shot `sync` shares the service's single-flight
reservation and reports a busy queue while the service is synchronizing.

Synchronization accesses the network and is therefore not part of the default
CI build. To debug one source, set `SKIP_X=1` or `SKIP_YOUTUBE=1`; never put a
temporary credential in shell history, source code, or the database.

Provider keys are managed in `/admin` and encrypted in SQLite with the
environment-only `LLM_SECRETS_KEY`. With no configured provider key, the
schedule-extraction stage reports a safe skip. To store an `OPENAI_API_KEY`
as the encrypted key of the `openai-default` provider, set both environment
variables, run `kano-go migrate-llm` (`npm run migrate:llm`) once, and remove
`OPENAI_API_KEY` afterwards. `SCHEDULE_MESSAGE_ENABLED` controls the
single-message detector; `SCHEDULE_KEYWORD_ENABLED` and
`SCHEDULE_VISION_ENABLED` control the board path. `SCHEDULE_EXTRACTION_ENABLED`
only supplies the default of a stage flag that has no stored or environment
value; the stage flags determine what runs.
Provider API keys entered in `/admin` require `LLM_SECRETS_KEY` and are
encrypted before they reach SQLite; they are never returned by the API. Once
configured, keys are read only by the server-side synchronization process.
Provider eligibility is based on the original post modality: Text, Image, or
both for mixed input. A failed provider gets up to the
[attempts per model](configuration.md#sync-settings) in total calls before the
next compatible provider is used.
X synchronization reads both handles in `X_HANDLES` and keeps a separate cursor
for each account.

## Backup and Recovery

Stop the server and any CLI synchronization before copying the complete data/
directory. Preserve SQLite and any WAL/SHM sidecars, media, and profile files
together. Keep the environment-only LLM_SECRETS_KEY in an access-controlled
secret backup so encrypted provider keys remain usable. Never commit backups.
Restore into a stopped deployment and verify health and the public snapshot
before enabling source timers. Avoid simultaneous Windows/Linux access to
the same SQLite file.

Use additive schema upgrades in
backend/internal/store/sqlite/schema_upgrade.go. Startup upgrades an older
snapshot in one transaction and never clears it; back up data/ before the
first start of a new release all the same. Seeding fills missing
static IDs; it is not a database restore. The -overwrite option intentionally
replaces the shipped static records and should only be used for reviewed
maintenance.

## Manual Maintenance

Use `/admin` instead of direct SQL for event maintenance. Creating, editing,
confirming, or deleting through that API records manual precedence and preserves
source links. Direct database edits can omit the lock/tombstone rules and should
be reserved for reviewed recovery work.

The `/mcp` endpoint is intentionally narrower than `/admin`. Its public tools
read the prepared snapshot and sanitized schedule/status views. Bearer-protected
tools may request a revision, start a full synchronization, or run the automatic
keyword/board/message scan; all of these return an asynchronous job result. MCP has no
manual confirmation, edit, delete, profile-media selection/upload, SQL, file, or
arbitrary URL-proxy operation. A page refresh or revision request never performs
an external fetch.

## Related Docs

- [Schema and precedence](../architecture/data-model.md)
- [Media layout](../architecture/media-cache.md)
- [Configuration](configuration.md)
