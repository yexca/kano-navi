# Unreleased

<details>
<summary><strong>Operator console</strong></summary>

- The Videos page shows each video's locally cached thumbnail, and vertical
  covers stay inside the 16:9 frame.

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- Thumbnail downloads retry transient failures after 30 minutes, fall back to
  a smaller YouTube size when the larger one was never generated, and pick up
  a replaced cover for upcoming and recent videos.

</details>
