# Unreleased

<details>
<summary><strong>Public pages</strong></summary>

- The Up next ticket keeps its artwork layout only for streams and videos with
  a thumbnail. A schedule entry without one gets a compact date ticket with its
  provenance and a link named for what it opens (the stream or its X notice),
  and the latest video follows it as a small card.
- The board's link list and milestones follow the selected language instead
  of always showing the Chinese seed text, and the journey portrait caption
  names the agency in the selected language.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- Model routing is one priority queue instead of separate board, message and
  fallback routes. Every stage tries the queued models in order and skips those
  without the Text or Image tag it needs. `GET /api/admin/llm` returns `queue`
  in place of `routes`, and `PUT /api/admin/llm/queue` replaces
  `PUT /api/admin/llm/routes/{route}`. The first start merges the saved routes
  into the queue (message, board, then fallback order, without duplicates).
- Providers no longer have a default model. Every queue target and connection
  test names a catalog model: `PUT /api/admin/llm/queue` requires `modelId`,
  provider responses drop `model`, and a provider write ignores it. Creating a
  provider or adding models queues nothing. The first start rewrites targets
  that followed the default to name that model, keeps the earlier position of a
  duplicate, and drops targets of providers that never had one.
- The provider dialog has presets on the left and the connection, models and
  request settings on the right. Discovering models saves the connection
  first; on save, ticked models join the catalog and models marked Queue join
  the end of the queue. The models dialog queues and tests one model at a time.
- Content is split into separate Posts, Videos and Profile images pages; older
  `#content/...` links open the matching page. Posts and Videos each have a
  quick fetch for their source: one-click recent windows of 24 hours, 3, 7 or
  30 days, and a date range when the source's official API credential is set.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- `make sensitive-check` scans only the files Git can see, so ignored local
  state such as `.env`, `data/` and tool indexes no longer fails it. Tracked
  files are still scanned even when an ignore rule matches them.
- A daily Dependency audit workflow runs npm audit and govulncheck. Run the
  same checks locally with `make audit`.
- The backend and the release image build with Go 1.26.10, which fixes the
  standard library vulnerabilities reported for 1.26.4. Vite 8.3.5 resolves the
  development-only launch-editor advisories.
- CI, the Docker frontend build and the development image use Node 24.22.1,
  the October 2026 security release, and `package.json` requires at least that
  version.

</details>
