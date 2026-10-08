# Deployment Security

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

## Admin Authentication

Optional historical source credentials (`X_API_BEARER_TOKEN`,
`YOUTUBE_API_KEY`) exist only in the environment. Official source requests use
Bearer or `X-Goog-Api-Key` headers with redirects blocked. The admin API
returns availability flags, not credentials; error reports contain controlled
codes rather than upstream response bodies or request exceptions. Time-window
fetches require the same admin session and mutation-origin checks as other
operator actions, and share the bounded single-flight synchronization queue.

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

## LLM Secrets and Dependencies

LLM_SECRETS_KEY stays in the environment; provider keys are encrypted before
storage. All runtime entry points, including development, reject an unset,
empty, or whitespace-only master key before opening SQLite. Plaintext and ciphertext must never appear in API responses, logs,
raw_json, or extraction payloads. Rotate the master key together with stored
provider keys when compromised. Historical source credentials and the MCP
control token remain environment-only. See [Secure development](../development/security.md).

Use HTTPS for production admin and MCP control access. Preserve browser
Sec-Fetch-Site and forward X-Forwarded-Proto through the reverse proxy as
described in [Docker deployment](docker.md). Protect the complete runtime data
backup and its separate key backup.

## Reporting a Problem

Do not paste credentials, cookies, private URLs, or a complete database into a
public issue, log, or pull request. Revoke and rotate exposed credentials first,
then use a maintainer-approved private channel. Ordinary documentation and code
issues can follow the repository's normal workflow.

## Related Docs

- [Security policy](../../SECURITY.md)
- [Security map](../security/index.md)
- [Configuration](configuration.md)
- [Database backup](database.md)
