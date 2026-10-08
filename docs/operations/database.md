# Database and Maintenance

SQLite and dynamic media live under the ignored data/ directory. Static history
images ship in public/assets/history/ and are included in the image. A fresh
installation starts with static profile, timeline, and resources, plus empty
source snapshots and LLM provider/model routes.

## Seed and Synchronization

The `/history` visual archive has an offline import command:
`npm run history:import -- --from /path/to/kano_official`. Run it in the server's
environment. See [History and visual archive](../product/history.md) for details.

```bash
npm run seed                 # Idempotently add missing initial records
npm run seed -- --overwrite # Overwrite records with matching IDs from seed-data
npm run sync                 # Update the snapshot from public sources
```

Synchronization accesses the network and is therefore not part of the default
CI build. To debug one source, set `SKIP_X=1` or `SKIP_YOUTUBE=1`; never put a
temporary credential in shell history, source code, or the database.

Provider keys are managed in `/admin` and encrypted in SQLite with the
environment-only `LLM_SECRETS_KEY`. With no configured provider key, the
schedule-extraction stage reports a safe skip. To import a legacy
`OPENAI_API_KEY` once, set both environment variables and run
`npm run migrate:llm`; remove the legacy variable afterwards. `SCHEDULE_MESSAGE_ENABLED` controls the single-message detector;
`SCHEDULE_KEYWORD_ENABLED` and `SCHEDULE_VISION_ENABLED` control the board
path. `SCHEDULE_EXTRACTION_ENABLED` is retained for initial defaults and old
API compatibility; the stage flags determine what runs after configuration.
Provider API keys entered in `/admin` require `LLM_SECRETS_KEY` and are
encrypted before they reach SQLite; they are never returned by the API. Once
configured, keys are read only by the server-side synchronization process.
Provider eligibility is based on the original post modality: Text, Image, or
both for mixed input. A failed provider is retried up to three total calls
before the next compatible provider is used.
X synchronization reads both handles in `X_HANDLES` and keeps a separate cursor
for each account.

## Backup and Recovery

Stop the server and any CLI synchronization before copying the complete data/
directory. Preserve SQLite and any WAL/SHM sidecars, media, and profile files
together. Keep the environment-only LLM_SECRETS_KEY in an access-controlled
secret backup so encrypted provider keys remain usable. Never commit backups.
Restore into a stopped deployment and verify health and the public snapshot
before enabling workflow timers. Avoid simultaneous Windows/Linux access to
the same SQLite file.

Use additive schema upgrades in server/database.js. Seeding fills missing
static IDs; it is not a database restore. The --overwrite option intentionally
replaces matching seed records and should only be used for reviewed maintenance.

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
