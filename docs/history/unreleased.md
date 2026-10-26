# Unreleased

<details>
<summary><strong>Public pages</strong></summary>

- Posts on X shows only two rows of the grid and no longer has a Show all
  button; older posts stay in the Past posts dialog.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- With `YOUTUBE_API_KEY` set, an ended stream or premiere that no check has
  classified still updates when its watch page cannot be read: the Data API
  answer writes its title, thumbnail, start and end times and length and
  answers a refresh request, while its kind, its premiere verdict and its
  schedule entry type stay as stored. Hosts that never get a readable watch
  page no longer leave such videos, premiere placeholder titles included,
  unchecked.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- The YouTube step result lists the error codes it recorded as `errors`, each
  once with its count. The run view now shows lists of plain values such as
  error codes instead of leaving them out, so the X step's `errors` appear
  too.

</details>
