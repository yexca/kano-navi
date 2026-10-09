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
Set `KANO_IMAGE=yexca/kano-navi:0.1.1` in `.env` to pin the release,
and use `KANO_PORT` to change the default host port of `7657`. The container
listens on `7657`; the default public address is `http://localhost:7657`.
To use the equivalent GHCR image, set
`KANO_IMAGE=ghcr.io/yexca/kano-navi:latest`.

Fresh deployments start with empty X/YouTube snapshots and no LLM providers.
Add providers and routes in `/admin`, then run a workflow to populate source
data. Fixed history images are already bundled. Existing `./data` snapshots
and provider configuration survive upgrades.

Behind an HTTPS reverse proxy, preserve `Sec-Fetch-Site` from browser requests
and forward `X-Forwarded-Proto: https` so session cookies receive `Secure`.
Same-origin browser mutations work even if the proxy rewrites Host. Clients
without Fetch Metadata need the original public Host and forwarded protocol
to match any supplied `Origin`.

To build the production image from the local checkout:

```bash
docker build -t kano-navi:local .
KANO_IMAGE=kano-navi:local docker compose up -d --pull never
```

Version tags run the Release workflow, which builds once and publishes the
same image to Docker Hub and GHCR with version, major/minor, and `latest` tags.
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
The development image mounts Express routes in Vite's HTTP server. Frontend,
`/api`, `/media`, and `/mcp` share container port `7657`, published on loopback.
Set `KANO_PORT` to change the host port. The repository is mounted for frontend hot reload,
while an anonymous volume keeps Linux dependencies separate from any host
`node_modules`. Rebuild after dependency changes; `--renew-anon-volumes`
refreshes that dependency volume. API source changes require
`docker compose -f docker-compose.dev.yml restart`. The existing `./data`
directory persists SQLite and media. A local `.env` is optional and is read
by the server when present; the Compose file sets development mode and disables
workflow timers.

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

The runtime stage copies `server/`, `scripts/`, the shared `src/lib/` directory
and the history media catalog alongside the built frontend and production
dependencies. Shared server imports, including stream identity and their local
dependencies, therefore survive packaging. Production still starts through
`server/index.ts`, validates authentication before opening SQLite and stores
runtime state only in the `/app/data` volume.

## Related Docs

- [Configuration](configuration.md)
- [Database backup and recovery](database.md)
- [Deployment security](security.md)
- [Release workflow](../development/commit-and-release.md)
