# Secure Development

## External Request Rules

- Keep all external requests in the reviewed server-side packages under
  `backend-go/internal/` (`source/`, `mediadownload/`, and the provider
  clients of `schedule/` and `llmadmin/`), with fixed HTTP(S) destinations,
  timeouts, limits, and failure handling.
- Never embed usernames, passwords, tokens, or cookies in a URL. Do not write credentials from a response into `raw_json`, logs, or the UI.
- Register a new source host and its purpose in `scripts/privacy-allowlist.json` first. The allowlist is a reviewable source record, not a comment-based way to bypass secret detection.
- An inbound push endpoint (the YouTube WebSub callback is the only one) must
  verify a shared-secret signature in constant time, bound its body, coalesce
  bursts, and only enqueue existing workflow steps rather than write data.
- Extract only the fields needed from external HTML/JSON. Filter stack traces, absolute paths, and raw responses before displaying anything.
  The request log records only a normalized route, method, status, duration
  and response size. Never log an error value, request body or header:
  upstream and parser errors can carry original JSON bodies, headers,
  passwords and provider secrets. Request bodies are bounded before parsing;
  invalid JSON and oversized bodies return 400/413 with a controlled error
  code.

The model boundary is opt-in. Matching public schedule-post text and ready
cached post images are sent only to the ordered, administrator-configured
model queue. OpenAI Responses, Chat Completions, and Anthropic Messages are
supported. Every provider request has a timeout, bounded responses/retries, and redirect
blocking; a failed provider is skipped in favor of the next configured one.
Provider keys are encrypted with the `LLM_SECRETS_KEY` master key before they
reach SQLite. `OPENAI_API_KEY` is accepted only by the explicit one-time
`kano-go migrate-llm` (`npm run migrate:llm`) command and is never read by synchronization or the API.
Neither plaintext keys nor ciphertext are returned by the admin API, written to
`raw_json`, or logged.
The master key itself exists only in the process environment and must be
rotated together with the stored provider keys when compromised.
Runtime startup rejects a missing or whitespace-only `LLM_SECRETS_KEY` before
opening SQLite, including in development mode.

## Sensitive-Information Scan

`scripts/check-sensitive.ts` checks the workspace and CI for:

- common platform tokens, cloud keys, private-key headers, and `Bearer` credentials;
- literals assigned to sensitive keys such as `password`, `secret`, `token`, or `apiKey`;
- credentials embedded in URLs or sensitive query parameters;
- personal machine paths and non-documentation IPv4 addresses;
- `.env`, `.npmrc`, private-key, and database files. The committed
  `.env.example` contains only empty credentials and public/local defaults.

The scan covers the files Git can see: tracked files and untracked files that
no ignore rule matches. Ignored local state such as `.env`, `data/`, and tool
indexes like `.codegraph/` is skipped. A tracked file stays in the scan even
when an ignore rule matches it, so a force-added secret is still reported.

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

## Dependency Updates

make npm-audit audits the full npm dependency tree, make npm-audit-production
the production subset, make npm-audit-signatures verifies the registry
signatures and provenance attestations of the installed packages, and make
go-audit runs govulncheck against the backend and its Go toolchain; make audit
runs all four. The Dependency audit workflow runs them daily. The advisory
checks are separate from source privacy and deterministic CI; CI and make ci
verify the signatures of every fresh installation. Review the lockfile and
outbound boundaries on upgrades. No npm dependency needs an install script: the
image build installs with `--ignore-scripts`, and SQLite is the pure-Go
`modernc.org/sqlite` driver pinned in `backend-go/go.mod`. Review `go.sum`
changes like the lockfile.

Dependabot opens weekly grouped pull requests for GitHub Actions (including the
composite actions in `.github/actions/`), Go modules and npm packages, each
after a seven-day cooldown so a freshly published release has time to be
withdrawn. Its commit messages follow the
[commit convention](commit-and-release.md#commit-convention). Review such a
pull request like any other dependency change. The Node version in `.nvmrc`,
`package.json` engines and the Dockerfiles, the Go version in
`backend-go/go.mod` and the Dockerfiles, and the Go tool versions pinned in the
Makefile are upgraded by hand in one change.

## Related Docs

- [Deployment security](../operations/security.md)
- [Media downloader boundary](../architecture/media-cache.md)
- [Security policy](../../SECURITY.md)
