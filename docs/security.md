# Security and Privacy

## Trust Boundary

- The browser trusts only the prepared snapshot returned by the local Express API. X, YouTube, image CDNs, and other public pages are untrusted input.
- SQLite, runtime directories, and environment variables belong to the local maintainer boundary and must not be exposed verbatim through the API.
- `/mcp` is a separate integration boundary: public tools expose only prepared
  read-only facts, while mutation tools require a dedicated bearer token. The
  token never grants the browser admin session or arbitrary server access.
- Cached media is runtime state. The only file read route is `/media/:id`; it
  validates an opaque database ID, a ready status, and a resolved path below the
  cache root.
- This is a public fan-made project; the public dashboard does not collect
  visitor accounts or profiles. The production admin API uses one local session
  cookie after password authentication.

## External Request Rules

- Keep all external requests in the reviewed synchronization path under
  `scripts/` and its server-side helpers, with fixed HTTP(S) destinations,
  timeouts, limits, and failure handling.
- Never embed usernames, passwords, tokens, or cookies in a URL. Do not write credentials from a response into `raw_json`, logs, or the UI.
- Register a new source host and its purpose in `scripts/privacy-allowlist.json` first. The allowlist is a reviewable source record, not a comment-based way to bypass secret detection.
- Extract only the fields needed from external HTML/JSON. Filter stack traces, absolute paths, and raw responses before displaying anything.

The model boundary is opt-in. Matching public schedule-post text and ready
cached post images are sent only to the ordered, administrator-configured
OpenAI-compatible provider route. Responses and Chat Completions protocols are
supported. Every provider request has a timeout, bounded retries, and redirect
blocking; a failed provider is skipped in favor of the next configured one.
Provider keys are encrypted with the `LLM_SECRETS_KEY` master key before they
reach SQLite. `OPENAI_API_KEY` is accepted only by the explicit one-time
`npm run migrate:llm` command and is never read by synchronization or the API.
Neither plaintext keys nor ciphertext are returned by the admin API, written to
`raw_json`, or logged.
The master key itself exists only in the process environment and must be
rotated together with the stored provider keys when compromised.

## Admin Authentication

- `/admin` is intentionally unlinked, but path obscurity is not authentication.
- The example environment and server default to `APP_MODE=production`.
  Only an explicit `APP_MODE=development` bypasses login for local development;
  `NODE_ENV` does not enable that bypass.
- Production validates authentication before opening or seeding SQLite.
  `ADMIN_PASSWORD` must contain at least 12 characters after trimming surrounding
  whitespace; missing, whitespace-only, and shorter passwords refuse startup.
  The configured password itself is preserved for login comparison.
- Successful production login creates a random in-memory session with an
  HttpOnly, SameSite=Strict cookie scoped to `/api/admin`. HTTPS requests also
  receive the `Secure` attribute.
- Failed logins are rate-limited per observed client address. Server restarts
  clear all sessions.
- Admin mutations reject `Sec-Fetch-Site: cross-site`. Browser-provided
  `same-origin` metadata survives reverse-proxy Host rewriting and HTTPS
  termination, so it is accepted without comparing the public origin to the
  internal Host. Page JavaScript cannot set this header. Other clients with an
  `Origin` must match the observed host and protocol; authentication is still
  required for every protected request.
- Manual event mutations require the authenticated admin API and create durable
  locks/tombstones so untrusted source or model output cannot overwrite them.

## MCP Authentication and Scope

- Public `/mcp` read tools do not require a key. They return a sanitized view of
  the stored dashboard, schedules, posts, health, and synchronization status;
  deleted tombstones and operator-only lock/source metadata are omitted.
- `dashboard_request_reload`, `sync_start`, and `scan_run_automatic` require
  `Authorization: Bearer <MCP_CONTROL_TOKEN>`. Use a long random token, send it
  only over HTTPS, and rotate it through the runtime environment. An admin
  session cookie is deliberately not accepted as an MCP credential.
- MCP control calls start asynchronous, single-flight jobs. They cannot confirm,
  edit, delete, or select records, upload media, execute SQL, read files, issue
  arbitrary proxy requests, or send DOM/browser commands. Human confirmation
  remains available only to a person in `/admin`.
- A dashboard revision is a notification hint, not a data write. Page reload or
  `dashboard_request_reload` reads the existing SQLite snapshot; only the
  server-side synchronization job performs external source requests.

## Sensitive-Information Scan

`scripts/check-sensitive.mjs` checks the workspace and CI for:

- common platform tokens, cloud keys, private-key headers, and `Bearer` credentials;
- literals assigned to sensitive keys such as `password`, `secret`, `token`, or `apiKey`;
- credentials embedded in URLs or sensitive query parameters;
- personal machine paths and non-documentation IPv4 addresses;
- `.env`, `.npmrc`, private-key, and database files. The committed
  `.env.example` contains only empty credentials and public/local defaults.

`LLM_SECRETS_KEY`, `MCP_CONTROL_TOKEN`, `ADMIN_PASSWORD`, and provider API keys
must remain in the ignored runtime environment. Example values are intentionally
empty; never copy a production token into documentation or tests.

Public platform URLs are allowed because they are product data. The scanner
still checks that their hosts are in the allowlist or a reserved-domain set.
Adding a public host requires a reason. Images, SQLite, and other binary assets
are excluded from text scanning, but their source and licensing still need
manual review.

Run locally:

```bash
make check-sensitive
make test-sensitive
```

When a finding appears, fix the underlying content. Do not hide it with an
`allow` comment, a string rewrite, or a wider ignore directory.

## CI Security Boundary

GitHub Actions uses read-only repository permissions, `npm ci`, and fixed build
commands. CI runs scanner and server/cache tests, the workspace scan,
documentation-link check, Vite build, and local API smoke check; it does not
call live X, YouTube, image CDN, or OpenAI endpoints. It also builds the
production Docker image without pushing it. Version tags run the same checks,
then publish the same image to Docker Hub and GitHub Container Registry before
creating a GitHub Release. Docker Hub uses the repository's `DOCKERHUB_TOKEN`
secret; GHCR uses the workflow's package-scoped `GITHUB_TOKEN`. Registry
credentials are used only for login and must not enter build arguments or images.
Dependency upgrades and new external hosts should include a review of the lock
file, allowlist, and synchronization boundary.

Run `npm audit` to check the full dependency tree against the current npm
advisories, and `npm audit --omit=dev` for the production subset. The
sensitive-information scan checks source content, not dependency advisories.
`concurrently` stays in development dependencies, and its `shell-quote`
override installs a patched package instead of suppressing an audit finding.
The `allowScripts` policy approves only the reviewed `better-sqlite3@13.0.3`
native build. Review and update that version-specific entry when upgrading
SQLite; do not grant a blanket approval to dependency install scripts.

## Reporting a Problem

Do not paste credentials, cookies, private URLs, or a complete database into a
public issue, log, or pull request. Revoke and rotate exposed credentials first,
then use a maintainer-approved private channel. Ordinary documentation and code
issues can follow the repository's normal workflow.
