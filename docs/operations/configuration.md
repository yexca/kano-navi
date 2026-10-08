# Runtime Configuration

## Environment Variables

| Variable                      | Default              | Purpose                                                            |
| ----------------------------- | -------------------- | ------------------------------------------------------------------ |
| `APP_MODE`                    | `production`         | Explicit `development` bypasses login; production requires it      |
| `ADMIN_PASSWORD`              | Empty                | Production startup requires at least 12 non-padding characters     |
| `LLM_SECRETS_KEY`             | Empty                | Environment-only master key for encrypted provider API keys        |
| `PORT`                        | `7657`               | Shared frontend/API port for development, preview, and production  |
| `SCHEDULE_EXTRACTION_ENABLED` | `1`                  | Initial legacy default for independent schedule stages             |
| `SCHEDULE_KEYWORD_ENABLED`    | `1`                  | Enable keyword candidate selection                                 |
| `SCHEDULE_VISION_ENABLED`     | `1`                  | Enable schedule-board LLM extraction                               |
| `SCHEDULE_MESSAGE_ENABLED`    | `1`                  | Enable single-message schedule detection                           |
| `SCHEDULE_KEYWORDS`           | `schedule,...`       | Comma-separated schedule candidate keywords                        |
| `WORKFLOW_SCHEDULER_ENABLED`  | `1`                  | Start timed saved workflows inside the server process              |
| `MCP_ENABLED`                 | `1`                  | Expose the stateless `/mcp` endpoint                               |
| `MCP_CONTROL_TOKEN`           | Empty                | Bearer token for MCP mutation tools                                |
| `X_HANDLES`                   | `kano_2525,_Kanotic` | Comma-separated public X account names                             |
| `X_HANDLE`                    | Empty                | Single-account compatibility fallback                              |
| `X_API_BEARER_TOKEN`          | Empty                | Optional environment-only credential for X archive search          |
| `YOUTUBE_API_KEY`             | Empty                | Optional environment-only credential for YouTube range search      |
| `SOURCE_HISTORY_MAX_PAGES`    | `4`                  | Official source pages per account/run (1–20), also within X budget |
| `X_BOOTSTRAP_DAYS`            | `7`                  | First-run X lookback in days                                       |
| `X_DISCOVERY_LIMIT`           | `50`                 | Maximum discovered status IDs considered                           |
| `X_MAX_STATUS_REQUESTS`       | `12`                 | Maximum X detail requests per run                                  |
| `X_REFRESH_KNOWN`             | `1`                  | Known X posts refreshed per run                                    |
| `X_SCHEDULE_REFRESH_LIMIT`    | `1`                  | Missing schedule source posts prioritized per account              |
| `YOUTUBE_CHANNEL_ID`          | Main channel         | YouTube channel identifier                                         |
| `YOUTUBE_BOOTSTRAP_VIDEOS`    | `6`                  | First-run RSS entries stored                                       |
| `YOUTUBE_MAX_DETAIL_REQUESTS` | `12`                 | Reservation pages inspected per run                                |
| `SYNC_TIMEOUT_MS`             | `7000`               | Timeout for a source network request                               |
| `SYNC_REQUEST_DELAY_MS`       | `150`                | Delay between source detail requests                               |
| `MEDIA_DOWNLOAD_LIMIT`        | `20`                 | Pending images attempted per run                                   |
| `MEDIA_DOWNLOAD_TIMEOUT_MS`   | `10000`              | Timeout for an image request                                       |
| `MEDIA_MAX_BYTES`             | `10485760`           | Maximum bytes accepted for one image                               |
| `SKIP_X`, `SKIP_YOUTUBE`      | `0`                  | Set an individual source flag to `1` to skip it                    |
| `SKIP_MEDIA`, `SKIP_LLM`      | `0`                  | Set a post-processing stage flag to `1` to skip it                 |

Docker Compose also uses `KANO_IMAGE` (default
`yexca/kano-navi:latest` from Docker Hub) for the published image and `KANO_PORT`
(default `7657`) for the host port. The API also defaults to `7657` inside the container.

`.env.example` is the complete non-secret inventory. `ADMIN_PASSWORD` and
`LLM_SECRETS_KEY`, `MCP_CONTROL_TOKEN`, and provider API keys are credentials;
never place real values in source code, SQLite plaintext, URLs, API responses,
or logs. `OPENAI_API_KEY` is accepted only by the one-time migration command.

Configure local values in the ignored .env or deployment environment. Changes
to environment-only credentials require a server restart. Provider hosts,
models, encrypted keys, routes, source accounts, and workflow windows are
operator settings managed through /admin. Source windows and timer intervals
are independent.

## Related Docs

- [Local development](../development/local-dev.md)
- [Docker deployment](docker.md)
- [Deployment security](security.md)
- [Historical source access](../architecture/workflows.md#fetch-windows-and-historical-backfill)
