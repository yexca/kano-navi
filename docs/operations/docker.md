# Docker Deployment

The root `docker-compose.yml` deploys the published production image. Copy
`.env.example` to `.env`, keep `APP_MODE=production`, and configure
`ADMIN_PASSWORD` with at least 12 characters after trimming surrounding
whitespace, plus a non-blank `LLM_SECRETS_KEY`. Both Compose configurations
require the master key; development bypasses login only. Then run:

```bash
docker compose up -d
```

Compose pulls `yexca/kano-navi:latest` from Docker Hub on each startup. A deployment
directory needs only `docker-compose.yml` and `.env`; the `./data` directory
is created when needed and mounted at `/app/data`, preserving SQLite and media
across container replacement. Run the same command to apply an image update.
Set `KANO_IMAGE=yexca/kano-navi:0.4.3` in `.env` to pin the release,
and use `KANO_PORT` to change the default host port of `7657`. The container
listens on `7657`; the default public address is `http://localhost:7657`.
To use the equivalent GHCR image, set
`KANO_IMAGE=ghcr.io/yexca/kano-navi:latest`.

Fresh deployments start with empty X/YouTube snapshots and no LLM providers.
Add providers and the model queue in `/admin`, then use a source's Run now or
enable its timer on the Sources page to populate source data. Fixed history
images are already bundled. The LINE BLOG archive's text is bundled too; its
media live in `./data/lineblog/` or a read-only mount over it, taken from
the recovery archive as described in
[LINE BLOG archive maintenance](../development/lineblog-archive.md#deployments).
Existing `./data` snapshots, archive media and provider configuration survive
upgrades.

## Reverse Proxy

Behind an HTTPS reverse proxy, preserve `Sec-Fetch-Site` from browser requests
and forward `X-Forwarded-Proto: https` so session cookies receive `Secure`.
Same-origin browser mutations work even if the proxy rewrites Host. Clients
without Fetch Metadata need the original public Host and forwarded protocol
to match any supplied `Origin`.

To enable the optional mainland China site suggestion, enter the provider name,
provider website and mirror site, turn on the setting on `/admin` → Site, then configure the proxy to overwrite `CF-IPCountry`,
`X-Country-Code`, `X-Geo-Country` or `X-Visitor-Country` with its country code.
The setting is off by default and Cloudflare's `CF-IPCountry` is checked first.
Requests that reach the Go listener without one of these headers receive
`mainlandChina: false`; the browser never sends the visitor's IP to a
geolocation service.

The production listener gzip-compresses public JSON directly and sends the
build's text from its precompressed Brotli and gzip siblings; a proxy is not
required for compression. Preserve `Content-Encoding`, `Vary` and
validators and avoid recompressing an already encoded response. Exclude admin,
authentication, MCP and runtime media from any additional proxy compression.
Preserve the admin/MCP `no-transform` directive.

Open boards follow changes over the
[revision stream](../architecture/public-api.md#revision-stream)
`/api/dashboard/events`, a long-lived `text/event-stream` response. Pass it
through unbuffered and uncompressed: nginx honours the stream's
`X-Accel-Buffering: no`, and other proxies need response buffering turned off
for the path. The stream sends a comment every 25 seconds, so a proxy read or
idle timeout of 60 seconds or more keeps it open; a shorter one only makes the
browser reconnect. Count one open connection per visible board tab in the
proxy's connection limits. When the stream cannot be kept open, boards fall
back to asking for the revision every 30 seconds and keep working.

## Build and Maintenance

Deploy `dist/` together with its `.vite/manifest.json` and the `.br` and `.gz`
siblings the build writes next to its text files: manifest-listed hashed
JS/CSS and hash-addressed history images are immutable for one year, while
HTML and other fixed assets revalidate. Copy siblings with or after their
originals; a sibling older than its original is ignored.
Keep earlier hashed files available during a rolling deployment when possible;
an already open page can request its old lazy chunks. If a chunk is unavailable,
the page's reload action obtains the latest revalidated HTML.

To build the production image from the local checkout:

```bash
docker build -t kano-navi:local .
KANO_IMAGE=kano-navi:local docker compose up -d --pull never
```

The runtime image contains the static Go binary, the built frontend, the
bundled assets, Node.js and the checksum-verified yt-dlp `2026.10.26`
executable. The
container health check runs `kano-go healthcheck`. One-shot maintenance
commands run against the mounted `./data`:

```bash
docker compose run --rm kano seed
docker compose run --rm kano sync
docker compose run --rm kano migrate-llm
```

### Upgrades and rollback

Back up `./data` as described in
[Backup and recovery](database.md#backup-and-recovery) before starting a new
image. The first start upgrades the snapshot in place and keeps every record,
provider and manual decision; provider keys stay readable with the same
`LLM_SECRETS_KEY`. To roll back, stop the service, restore the `./data` backup
taken before the upgrade, set `KANO_IMAGE` to the previous image tag and start
it again. Only one service may own a snapshot at a time. Upgrade notes for
each release are in the [release history](../history/index.md).

Version tags run the Release workflow, which builds once and publishes the
same image to Docker Hub and GHCR with version, major/minor, and `latest` tags.
Each tag is a multi-platform image for `linux/amd64` and `linux/arm64`, so
Docker pulls the variant of the host. The frontend and Go stages run on the
build machine and Go cross-compiles each platform; no emulation is involved.
Configure the repository secret `DOCKERHUB_TOKEN` with push access. The Docker
Hub username defaults to the GitHub repository owner; set the optional
repository variable `DOCKERHUB_USERNAME` when those accounts differ. GHCR
uses the workflow's `GITHUB_TOKEN` with package-write permission.

For Docker development, use the separate Compose file:

```bash
docker compose -f docker-compose.dev.yml up -d --build --renew-anon-volumes
```

Open `http://localhost:7657` for the dashboard or
`http://localhost:7657/admin` for the password-free development console.
The development image carries the Go toolchain and Node. It builds and starts
the Go backend and runs Vite, which proxies `/api`, `/media`, and `/mcp` to it;
everything shares container port `7657`, published on loopback.
Set `KANO_PORT` to change the host port. The repository is mounted for
frontend hot reload; a Windows or macOS host sends no file events into the
container, so the Compose file sets `KANO_WATCH_POLLING=1` and Vite polls the
sources (not `data/`, `backend/`, `dist/`, `.claude/` or `.codegraph/`).
Anonymous volumes keep Linux dependencies and the Linux backend binary
separate from any host `node_modules` and `backend/bin`. Rebuild after
dependency changes; `--renew-anon-volumes` refreshes those volumes. Backend
source changes require `docker compose -f docker-compose.dev.yml restart`,
which rebuilds the binary. The existing `./data`
directory persists SQLite and media. A local `.env` is optional and is read
by the server when present; the Compose file sets development mode and disables
the source timers.

```bash
docker compose -f docker-compose.dev.yml logs -f
docker compose -f docker-compose.dev.yml down
```

## Local Validation

Run make docker-build and make production-smoke to validate the production
image before deployment. The smoke command creates a disposable container with
its own data volume and synthetic authentication, checks public pages and API
contracts, and removes the container and anonymous volume afterwards.
It does not mount the operator data/ directory or load .env.

The runtime stage copies the `kano-go` and yt-dlp executables, `dist/` with its
precompressed siblings, and `public/`. The embedded schema and static seeds
are compiled into the binary. `YTDLP_BINARY` defaults to `/app/yt-dlp` in the
production Compose deployment; set it to an empty value to disable the method.
Production starts through `kano-go serve`, validates authentication before
opening SQLite and stores runtime state only in the `/app/data` volume.

## Related Docs

- [Configuration](configuration.md)
- [Database backup and recovery](database.md)
- [Deployment security](security.md)
- [Release workflow](../development/commit-and-release.md)
