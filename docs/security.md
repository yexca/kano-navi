# Security and Privacy

## Trust Boundary

- The browser trusts only the prepared snapshot returned by the local Express API. X, YouTube, image CDNs, and other public pages are untrusted input.
- SQLite, runtime directories, and environment variables belong to the local maintainer boundary and must not be exposed verbatim through the API.
- Cached media is runtime state. The only file read route is `/media/:id`; it
  validates an opaque database ID, a ready status, and a resolved path below the
  cache root.
- This is a public fan-made project; it does not collect visitor accounts, cookies, or personal profiles.

## External Request Rules

- Keep all external requests in reviewed server-side scripts under `scripts/`,
  with fixed HTTP(S) destinations, timeouts, and failure handling.
- Never embed usernames, passwords, tokens, or cookies in a URL. Do not write credentials from a response into `raw_json`, logs, or the UI.
- Register a new source host and its purpose in `scripts/privacy-allowlist.json` first. The allowlist is a reviewable source record, not a comment-based way to bypass secret detection.
- Extract only the fields needed from external HTML/JSON. Filter stack traces, absolute paths, and raw responses before displaying anything.

## Sensitive-Information Scan

`scripts/check-sensitive.mjs` checks the workspace and CI for:

- common platform tokens, cloud keys, private-key headers, and `Bearer` credentials;
- literals assigned to sensitive keys such as `password`, `secret`, `token`, or `apiKey`;
- credentials embedded in URLs or sensitive query parameters;
- personal machine paths and non-documentation IPv4 addresses;
- `.env`, `.npmrc`, private-key, and database files.

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
call live X or YouTube.
Dependency upgrades and new external hosts should include a review of the lock
file, allowlist, and synchronization boundary.

## Reporting a Problem

Do not paste credentials, cookies, private URLs, or a complete database into a
public issue, log, or pull request. Revoke and rotate exposed credentials first,
then use a maintainer-approved private channel. Ordinary documentation and code
issues can follow the repository's normal workflow.
