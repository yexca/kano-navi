# Commit and Release

## Commit Convention

- Use Conventional Commits with the exact subject form `<type>(<scope>): <description>`.
- Use one of `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `perf`, `style`, or `revert` as the type.
- Keep the scope short, lowercase, and tied to the primary subsystem, such as `app`, `media`, `sync`, `docs`, or `ci`.
- Write the description in lowercase imperative language, keep the subject at 72 characters or fewer, and do not end it with a period.
- Keep each commit focused on one coherent change. Use a body when the reason or upgrade behavior is not evident from the subject.
- Mark an incompatible change with `!` before the colon and a `BREAKING CHANGE:` footer.
- Before committing, run the required checks, review `git diff --cached`, and confirm that staged files match the user's requested boundary. Never stage ignored runtime data, secrets, or unrelated user changes.
- Create, amend, rebase, or otherwise rewrite a commit only when the user explicitly requests it.

## Before Committing

Run make ci and make sensitive-check, inspect scanner findings manually, and
review git diff --cached. Stage only files within the requested boundary.
Never stage runtime state, secrets, or unrelated user changes. Agents create
commits and push only on an explicit user request. Keep the configured signing
path when committing; do not disable signing to work around a missing signer.

## Merging into Main

Before merging a branch into `main`, judge whether its commits are worth
keeping, and choose the merge method from that judgement. Agents merge only on
an explicit user request, and the checks above apply to the result on `main`.

- **Squash merge** (`git merge --squash`, then one commit) when the history has
  no value of its own: a single commit, work-in-progress or fixup commits,
  review rounds, reverted attempts, or subjects that do not follow the commit
  convention. The squashed commit follows the convention and describes the
  branch's change as a whole.
- **Merge commit** (`git merge --no-ff`) only when every commit is a focused,
  conventional step that builds and passes the checks on its own, and a reader
  would want to read, bisect or revert those steps separately, for example a
  schema upgrade followed by the code that uses it. Its subject also follows
  the convention, such as `feat(sync): merge per-source schedules`.

When unsure, squash. Do not rewrite a branch's commits to make them worth
keeping unless the user asks for it.

## Version Source

package.json owns the application version as `<major>.<minor>.<patch>`; keep
package-lock.json in step with it. The About page imports it, and every Go
build (the image, `make go-build` and `scripts/run-app.mjs`) injects it into
`internal/buildinfo.Version` with `-ldflags`, which MCP reports in
`serverInfo`. A plain `go build` or `go test` reports `0.0.0-dev`. Read the
current version and workflow rather than embedding a release number in agent
instructions or deployment guidance.

Release tags use `v<major>.<minor>.<patch>`. The tag must equal `v` plus the
package.json version and point to a commit on `main`.

## Release Validation

The Release workflow first verifies its source: the tag matches package.json,
`docs/history/<tag>.md` exists, and the tagged commit is on `main`. It then
reuses .github/workflows/ci.yml for the tagged revision. The full read-only
validation, including the production image and runtime smoke, must pass before
publishing. CI is intentionally rerun for the tag; there is no lookup or wait
for a previous main-branch run.

## Release Notes

A release note shows its area titles and folds the changes under them, so a
reader sees what the release touches before expanding any detail. Its parts
appear in this order:

1. Upgrade notes as GitHub alerts (`> [!IMPORTANT]`, `> [!NOTE]`,
   `> [!WARNING]`) when the release changes configuration, public or admin API
   behavior, or the upgrade path.
2. A short summary paragraph.
3. One folded block per user-facing area.
4. A `## Known Issue` section, when the release has one.

Only the area blocks are folded; upgrade notes, the summary and known issues
stay expanded because a reader must not miss them. Each area is a `<details>`
block whose summary is the area title, in place of a `##` heading:

```markdown
<details>
<summary><strong>Public pages</strong></summary>

- One change per bullet.

</details>
```

Keep the blank line after `<summary>` and the one before `</details>`; GitHub
renders the list as literal text without them. The usual areas are Public
pages, Operator console, Synchronization and extraction, and Runtime and
development; name an area after what the release changes when none fits.

`docs/history/unreleased.md` holds pending changes in the same area blocks
under its heading. Add a pending change as a bullet in the block of its area,
and create the block when the area has none.

Store each release note at `docs/history/<tag>.md` and add it to the
[history index](../history/index.md). The Release workflow requires the file
and uses it as the GitHub Release body; rerunning a release synchronizes the
existing body with the tracked file. The file starts directly with the release
body and does not repeat the tag as a level-one heading. Keep current system
contracts in the focused architecture, product, operations and development
documents.

## Release Steps

1. Set the version in package.json and package-lock.json.
2. Move the area blocks of `docs/history/unreleased.md` into
   `docs/history/<tag>.md`, add the upgrade notes and summary above them,
   leave `unreleased.md` with only its heading, and add the page to the
   history index.
3. Update version examples in .env.example and the Docker deployment guide.
4. Run make ci and make sensitive-check, commit as
   `chore(release): prepare <tag> release`, and push the commit to `main`.
5. Push the tag; the Release workflow verifies and publishes it.

## Publication Order and Credentials

1. Verify the release source and complete the reusable CI workflow.
2. Build one publication image and push it to Docker Hub and GHCR with version,
   major/minor, and latest tags. Preserve provenance and SBOM generation.
3. Create or publish the GitHub Release from `docs/history/<tag>.md` after
   image publication.

The publisher uses DOCKERHUB_TOKEN and an optional DOCKERHUB_USERNAME repository
variable (otherwise the repository owner). GHCR uses the package-write
GITHUB_TOKEN; the final GitHub Release job alone has contents-write permission.
Registry credentials are never build arguments. A per-tag concurrency group
serializes reruns without cancelling an in-progress publication. A failed image
publication prevents publishing the GitHub Release; registry updates across two
registries are not atomic.

## Related Docs

- [Testing and Actions](testing.md)
- [Secure development](security.md)
- [Docker deployment](../operations/docker.md)
