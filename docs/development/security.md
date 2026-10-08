# Secure Development

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

## Sensitive-Information Scan

`scripts/check-sensitive.ts` checks the workspace and CI for:

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
make sensitive-check
make test-sensitive
```

When a finding appears, fix the underlying content. Do not hide it with an
`allow` comment, a string rewrite, or a wider ignore directory.

## Validation and Publication Boundary

CI has read-only repository permissions and checks formatting, documentation,
scanner behavior, source privacy, server contracts, the frontend build, local
API smoke, and a disposable production container. Platform and model data is
never fetched by validation. Release reuses the same CI workflow before registry
publication; only registry and release jobs receive write permissions.
Credentials are used only for registry login and must not enter build arguments,
images, source artifacts, or logs. See [Testing](testing.md) and
[Commit and release](commit-and-release.md).

Run npm audit for the full dependency tree and npm audit --omit=dev for the
production subset. These network advisory checks are separate from source
privacy and deterministic CI. Review the lockfile and outbound boundaries on
upgrades. concurrently stays development-only; its scoped shell-quote override
selects a patched package. The allowScripts policy approves only the reviewed
better-sqlite3@13.0.3 native build; update the version-specific approval with
SQLite upgrades instead of approving all dependency scripts.

## Related Docs

- [Deployment security](../operations/security.md)
- [Media downloader boundary](../architecture/media-cache.md)
- [Security policy](../../SECURITY.md)
