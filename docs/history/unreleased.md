# Unreleased

<details>
<summary><strong>Public pages</strong></summary>

- The Up next ticket keeps its artwork layout only for streams and videos with
  a thumbnail. A schedule entry without one gets a compact date ticket with its
  provenance and a link named for what it opens (the stream or its X notice),
  and the latest video follows it as a small card.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- `make sensitive-check` scans only the files Git can see, so ignored local
  state such as `.env`, `data/` and tool indexes no longer fails it. Tracked
  files are still scanned even when an ignore rule matches them.
- The backend and the release image build with Go 1.26.10, which fixes the
  standard library vulnerabilities reported for 1.26.4. Vite 8.3.5 resolves the
  development-only launch-editor advisories.

</details>
