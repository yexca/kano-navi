# Runtime Configuration

## Environment Variables

| Variable                        | Default              | Purpose                                                           |
| ------------------------------- | -------------------- | ----------------------------------------------------------------- |
| `APP_MODE`                      | `production`         | Explicit `development` bypasses login; production requires it     |
| `ADMIN_PASSWORD`                | Empty                | Production startup requires at least 12 non-padding characters    |
| `ADMIN_ORIGIN`                  | Empty                | Optional explicit origin accepted for admin mutations             |
| `PUBLIC_ORIGIN`                 | Empty                | Optional public origin for absolute share-tag and feed links      |
| `LLM_SECRETS_KEY`               | Empty                | Required non-blank encryption master key at startup in every mode |
| `PORT`                          | `7657`               | Shared frontend/API port for development, preview, and production |
| `KANO_API_PORT`                 | `7658`               | Development only: loopback port of the backend behind Vite        |
| `KANO_WATCH_POLLING`            | `0`                  | Development only: `1` makes Vite poll a mounted checkout          |
| `SCHEDULE_EXTRACTION_ENABLED`   | `1`                  | Default for stage flags without a stored or environment value     |
| `SCHEDULE_KEYWORD_ENABLED`      | `1`                  | Enable keyword candidate selection                                |
| `SCHEDULE_VISION_ENABLED`       | `1`                  | Enable schedule-board LLM extraction                              |
| `SCHEDULE_MESSAGE_ENABLED`      | `1`                  | Enable single-message schedule detection                          |
| `SCHEDULE_MESSAGE_ALL_POSTS`    | `0`                  | Send every post through the message route, skipping the heuristic |
| `SCHEDULE_KEYWORDS`             | `schedule,...`       | Comma-separated schedule candidate keywords                       |
| `TRANSLATION_SCAN_LIMIT`        | `20`                 | Posts and titles the translate step sends to a model per run      |
| `TRANSLATION_LOOKBACK_DAYS`     | `30`                 | Days of posts and videos the translate step covers unrequested    |
| `WORKFLOW_SCHEDULER_ENABLED`    | `1`                  | Start the source timers in the server process                     |
| `MCP_ENABLED`                   | `1`                  | Expose the stateless `/mcp` endpoint                              |
| `MCP_CONTROL_TOKEN`             | Empty                | Bearer token for MCP mutation tools                               |
| `X_HANDLES`                     | `kano_2525,_Kanotic` | Comma-separated public X account names                            |
| `X_HANDLE`                      | Empty                | Single-account compatibility fallback                             |
| `X_API_BEARER_TOKEN`            | Empty                | Optional environment-only X API credential: fetches, chain method |
| `YOUTUBE_API_KEY`               | Empty                | Optional environment-only Data API key: YouTube sync and fetches  |
| `SOURCE_HISTORY_MAX_PAGES`      | `4`                  | Manual fetch pages per account or channel (1–20), within X budget |
| `X_BOOTSTRAP_DAYS`              | `7`                  | Age limit in days while an X account has no cursor yet            |
| `X_DISCOVERY_LIMIT`             | `50`                 | Full-sync cap of unstored X statuses per account; pages of 20     |
| `X_MAX_STATUS_REQUESTS`         | `12`                 | Maximum X detail requests per run; manual X fetch page budget     |
| `X_REFRESH_KNOWN`               | `1`                  | Known X posts refreshed per run                                   |
| `X_SCHEDULE_REFRESH_LIMIT`      | `1`                  | Missing schedule source posts prioritized per account             |
| `YOUTUBE_CHANNEL_ID`            | Main channel         | YouTube channel identifier                                        |
| `YOUTUBE_BOOTSTRAP_VIDEOS`      | `6`                  | First-run RSS entries stored                                      |
| `YOUTUBE_MAX_DETAIL_REQUESTS`   | `12`                 | Reservation pages inspected per run                               |
| `BILIBILI_UID`                  | Kano's account       | Bilibili account (numeric mid) whose uploads are read             |
| `BILIBILI_MIN_INTERVAL_MINUTES` | `180`                | Minimum minutes between Bilibili reads and timer runs (60–1440)   |
| `BILIBILI_SESSDATA`             | Empty                | Optional environment-only Bilibili login cookie                   |
| `SYNC_TIMEOUT_MS`               | `7000`               | Timeout for a source network request                              |
| `SYNC_REQUEST_DELAY_MS`         | `150`                | Delay between source detail requests                              |
| `MEDIA_DOWNLOAD_LIMIT`          | `20`                 | Pending images attempted per run                                  |
| `MEDIA_DOWNLOAD_TIMEOUT_MS`     | `10000`              | Timeout for an image request                                      |
| `MEDIA_MAX_BYTES`               | `10485760`           | Maximum bytes accepted for one image                              |
| `SKIP_X`, `SKIP_YOUTUBE`        | `0`                  | Set to `1` to skip that source's step and its timer               |
| `SKIP_BILIBILI`                 | `0`                  | Set to `1` to skip the Bilibili step, timer and console reads     |
| `SKIP_MEDIA`, `SKIP_LLM`        | `0`                  | Set a post-processing stage flag to `1` to skip it                |

`SKIP_LLM=1` skips both model steps, `schedule` and `translate`. Neither
translation variable configures a model: the translate step reads only the
translation queue set in `/admin` → Models, so it does nothing until that
queue holds a text model.

`PUBLIC_ORIGIN` is a bare `http` or `https` origin such as
`https://kano.example.invalid` (a trailing `/` is accepted and the host is
lowercased; a path, query, credentials, or a host that is not a DNS name or
IP address with an optional port fail startup). It only completes the link-preview tags of
`index.html` (see [Share tags](../architecture/backend.md#share-tags)) and the
Atom feed's site and self links (see
[Public feeds](../architecture/public-api.md#public-feeds)); it grants no access
and is not an admin origin.

Docker Compose also uses `KANO_IMAGE` (default
`yexca/kano-navi:latest` from Docker Hub) for the published image and `KANO_PORT`
(default `7657`) for the host port. The API also defaults to `7657` inside the container.

`BILIBILI_SESSDATA` is the `SESSDATA` cookie of a browser signed in to
Bilibili (developer tools → Application → Cookies → `.bilibili.com`). With
it, the `bilibili` step and the console's reads are sent as that account,
which Bilibili's captcha blocks far less often than anonymous reads. It is
sent only to `api.bilibili.com`, never stored in SQLite, logged, or returned;
the console shows only whether one is configured. Prefer a secondary account,
and replace the value when it expires or the account signs out.

`.env.example` is the complete non-secret inventory. `ADMIN_PASSWORD` and
`LLM_SECRETS_KEY`, `MCP_CONTROL_TOKEN`, `BILIBILI_SESSDATA`, and provider API keys are credentials;
never place real values in source code, SQLite plaintext, URLs, API responses,
or logs. `OPENAI_API_KEY` is accepted only by the one-time
`kano-go migrate-llm` command.

Configure local values in the ignored .env or deployment environment. Changes
to environment-only credentials require a server restart. Provider hosts,
models, encrypted keys, routes, source accounts, and each source schedule's
sync setting are operator settings managed through /admin. How much a run
reads and the timer interval are independent. A source schedule's
incremental cap replaces `X_DISCOVERY_LIMIT` for every run that reads X with
it (timer, Run now, YouTube push, one-off step run); full syncs
(`kano-go sync`, the console's Sync everything, MCP) keep the variable for X
and read 50 YouTube uploads at most. Each schedule's
[method chain](../architecture/workflows.md#method-chains) decides whether
its runs use `X_API_BEARER_TOKEN` and `YOUTUBE_API_KEY`: the X API only as a
fallback behind its gate and daily cap, the YouTube Data API only while the
chain enables it ([ADR-0004](../decisions/ADR-0004-method-fallback-chains.md)).
Full syncs read through the default chains, which leave the X API off, and
no timed read turns a credential into a date search
([ADR-0002](../decisions/ADR-0002-count-based-source-sync.md)).

The backend (`kano-go`) reads `.env` from its working directory; variables
already present in the environment win. The database path is a command-line
option (`-database`, default `data/database/kano.sqlite`) rather than an
environment variable, as are `-static` and `-listen`. `KANO_API_PORT` is read
only by `make dev`/`make preview` and Vite; production serves everything on
`PORT`.

Run with flags only and no command, the binary serves public reads from an
existing snapshot without writing it; only `make go-check` uses this mode. See
[Read-only mode](../architecture/backend.md#read-only-mode).

Set `LLM_SECRETS_KEY` before starting development, preview, or production, even
when no provider has been added. Missing, empty, or whitespace-only values
refuse startup before SQLite opens. Use a random master key and retain it
across restarts; changing it makes previously stored provider keys unreadable.
For example, generate a value locally with
`node -e "console.log(require('node:crypto').randomBytes(32).toString('base64'))"`
and save it only in the ignored environment file or deployment secret store.

## Source timers

`kano-go serve` starts the timer process unless
`WORKFLOW_SCHEDULER_ENABLED=0`, which stops the source schedules and the
live-status probe. Manual runs from /admin, including each source's Run now,
work either way. Page requests and admin reads never start a
timer or a run. Each source's timer is an operator setting, not an
environment variable: `/admin` → Sources turns it on or off and sets its
interval and read mode, stored in the `source_schedules` table (X 30 minutes
and YouTube 60 by default, 15 minutes through seven days; Bilibili 180 by
default, never below `BILIBILI_MIN_INTERVAL_MINUTES`). `SKIP_X`,
`SKIP_YOUTUBE` and `SKIP_BILIBILI` keep that source's timer from running.
Per-source timers are the only timed sync
([ADR-0003](../decisions/ADR-0003-per-source-sync-schedules.md)). See
[Workflows](../architecture/workflows.md#source-schedules).

The YouTube live-status probe is an operator setting, not an environment
variable: `/admin` → Sources → Live-status probe turns it on or off and sets
its interval (default 15 minutes, 5–360), stored in `app_settings` as
`youtube_live_probe_enabled` and `youtube_live_probe_interval_minutes`. The
interval paces streams on air and waiting rooms that stay closed past their
start; around the start itself the probe checks every 3 minutes. With
`YOUTUBE_API_KEY`, and unless the YouTube timer's method chain turns the Data
API off, it asks the Data API first, one quota unit per run. It
uses the same timer process, so `WORKFLOW_SCHEDULER_ENABLED=0` or
`SKIP_YOUTUBE=1` stops it. See
[Workflows](../architecture/workflows.md#live-status-probe).

## Related Docs

- [Local development](../development/local-dev.md)
- [Docker deployment](docker.md)
- [Deployment security](security.md)
- [Manual source fetches](../architecture/workflows.md#manual-fetches-and-historical-backfill)
