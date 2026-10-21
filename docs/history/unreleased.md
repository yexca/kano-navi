# Unreleased

<details>
<summary><strong>Public pages</strong></summary>

- The Up next ticket keeps its artwork layout only for streams and videos with
  a thumbnail. A schedule entry without one gets a compact date ticket with its
  provenance and a link named for what it opens (the stream or its X notice),
  and the latest video follows it as a small card.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- Model routing is one priority queue instead of separate board, message and
  fallback routes. Every stage tries the queued models in order and skips those
  without the Text or Image tag it needs. `GET /api/admin/llm` returns `queue`
  in place of `routes`, and `PUT /api/admin/llm/queue` replaces
  `PUT /api/admin/llm/routes/{route}`. The first start merges the saved routes
  into the queue (message, board, then fallback order, without duplicates).

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

</details>
