# Deployment Security

## Trust Boundary

- The browser trusts only the prepared snapshot returned by the local API. X, YouTube, Bilibili, image CDNs, and other public pages are untrusted input.
- SQLite, runtime directories, and environment variables belong to the local maintainer boundary and must not be exposed verbatim through the API.
- `/mcp` is a separate integration boundary: public tools expose only prepared
  read-only facts, while mutation tools require a dedicated bearer token. The
  token never grants the browser admin session or arbitrary server access.
- `/api/websub/youtube` is active only while YouTube push is enabled in
  `/admin`. It accepts unauthenticated hub traffic by design, so it trusts only
  bodies whose `X-Hub-Signature` matches the server-generated signing secret,
  and even those can only queue a debounced YouTube sync; it never writes
  snapshots or reveals whether a signature was valid. The secret is sealed with
  `LLM_SECRETS_KEY` in SQLite and never returned by the admin API; rotate it
  from the YouTube push card, which resubscribes with the new value.
- Cached media is runtime state. The file read routes are `/media/:id`, which
  validates an opaque database ID, a ready status, and a resolved path below the
  cache root, the selected profile media, and `/media/lineblog/<name>` and
  `/media/works/<name>`, which accept only an archive name and a regular file
  directly inside `data/lineblog/` or `data/works/`.
- This is a public fan-made project; the public dashboard does not collect
  visitor accounts or profiles. The production admin API uses one local session
  cookie after password authentication.

## Admin Authentication

Optional source API credentials (`X_API_BEARER_TOKEN`, `YOUTUBE_API_KEY`,
`BILIBILI_COOKIE`, `BILIBILI_SESSDATA`) exist only in the environment. The
Bilibili login cookies go only to `api.bilibili.com`, never to its ticket
endpoint; the opus page on
`www.bilibili.com`, the live host `api.live.bilibili.com` and the official
client's listing on `app.bilibili.com` are read without it. That listing is
signed with the app key pair the operator configures (`BILIBILI_APP_KEY`,
`BILIBILI_APP_SECRET`), which stays in the environment like the other source
credentials; without it the listing is never requested. The `bili_ticket` request goes to the same fixed `api.bilibili.com` host
with a key Bilibili's public web script carries; the ticket it returns is a
device cookie kept with the device identity, not a credential. Official source requests use
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
- An optional one-time code
  ([`ADMIN_TOTP_SECRET`](configuration.md#admin-one-time-code)) joins the
  password at production login. It is off by default; setting it is
  recommended whenever `/admin` is reachable from the internet, so a leaked
  password alone does not open the console. Login sends both in one request
  and a failure names neither, so it never confirms the password. A code from
  the step before or after the server's is accepted for clock skew, and each
  accepted code's step is spent: a later login needs a later code. Keep the
  server clock synchronized. The secret stays in the environment and process
  memory, is never stored or returned, and a malformed value refuses
  startup.
- Successful production login creates a random in-memory session with an
  HttpOnly, SameSite=Strict cookie scoped to `/api/admin`. HTTPS requests also
  receive the `Secure` attribute.
- Failed logins are rate-limited per observed client address. With the
  one-time code on, logins with the right password and a wrong code are also
  limited across every address (10 per 5 minutes), after which every login
  waits for the window to reset; such failures mean the password has leaked,
  so replace it. A wrong password counts only toward its address. The limit
  check, the verification and the counters are one atomic step, so
  concurrent logins cannot exceed either limit or sign in after it is
  reached. Server restarts clear all sessions.
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
  the stored dashboard, schedules, posts, health, and synchronization status
  (job kind, status and times, without job IDs);
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

## Web Bot Auth Key

The Web Bot Auth key identifies the shop source's requests as this site's.
It is an identity key this server generates, not a credential a source
issued: the operator makes it in `/admin`, and its private seed is sealed
with `LLM_SECRETS_KEY` in `app_settings`, like the YouTube push signing
secret. The seed is never returned by the admin API, which reports its
keyid and state, and never logged or written to `raw_json`; the public
directory at `/.well-known/http-message-signatures-directory` holds public
keys only. The optional `WEB_BOT_AUTH_KEY` override stays in the
environment and process memory. Anyone holding the seed can sign as this
site, so rotate the key in `/admin` (or replace the override) when it may
have leaked; the replaced public key stays listed for 24 hours, and
removing the key withdraws it at once. See
[Web Bot Auth](../architecture/web-bot-auth.md).

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
