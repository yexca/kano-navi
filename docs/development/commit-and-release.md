# Commit and Release

## Commit Convention

- Use Conventional Commits with the exact subject form `<type>(<scope>): <description>`.
- Use one of `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `perf`, `style`, or `revert` as the type.
- Keep the scope short, lowercase, and tied to the primary subsystem, such as `app`, `media`, `sync`, `docs`, or `ci`.
- Write the description in lowercase imperative language, keep the subject at 72 characters or fewer, and do not end it with a period.
- Keep each commit focused on one coherent change. Use a body when the reason or migration behavior is not evident from the subject.
- Mark an incompatible change with `!` before the colon and a `BREAKING CHANGE:` footer.
- Before committing, run the required checks, review `git diff --cached`, and confirm that staged files match the user's requested boundary. Never stage ignored runtime data, secrets, or unrelated user changes.
- Create, amend, rebase, or otherwise rewrite a commit only when the user explicitly requests it.

## Before Committing

Run make ci and make sensitive-check, inspect scanner findings manually, and
review git diff --cached. Stage only files within the requested boundary.
Never stage runtime state, secrets, or unrelated user changes. Agents create
commits and push only on an explicit user request. Keep the configured signing
path when committing; do not disable signing to work around a missing signer.

## Version and Release Validation

package.json owns the application version. Release tags use v<major>.<minor>.<patch>.
Read the current version and workflow rather than embedding a release number in
agent instructions or deployment guidance.

The Release workflow reuses .github/workflows/ci.yml for the tagged revision.
The full read-only validation, including the production image and runtime smoke,
must pass before publishing. CI is intentionally rerun for the tag; there is no
lookup or wait for a previous main-branch run.

## Publication Order and Credentials

1. Complete the reusable CI workflow.
2. Build one publication image and push it to Docker Hub and GHCR with version,
   major/minor, and latest tags. Preserve provenance and SBOM generation.
3. Create or publish the GitHub Release with generated notes after image publication.

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
