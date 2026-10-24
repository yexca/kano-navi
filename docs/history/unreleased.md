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
- YouTube sync also checks recent RSS entries, so waiting rooms and premieres
  missing from the streams page get their start time. Premieres become
  releases, and finished streams and premieres keep YouTube's reported end.

</details>

<details>
<summary><strong>Public pages</strong></summary>

- A started premiere or release no longer shows as on air for three hours;
  only streams are presumed live without an end, and a presumed-live item
  gives the spotlight to a reservation starting within 30 minutes.

</details>
