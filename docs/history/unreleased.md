# Unreleased

<details>
<summary><strong>Operator console</strong></summary>

- The Videos page shows each video's locally cached thumbnail, and vertical
  covers stay inside the 16:9 frame.
- Sources has a Live-status probe panel to turn the probe on or off and set
  its interval (default 30 minutes, 5 to 360); its runs appear in the run
  history as "Live-status probe".

</details>

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- Thumbnail downloads retry transient failures after 30 minutes, fall back to
  a smaller YouTube size when the larger one was never generated, and pick up
  a replaced cover for upcoming and recent videos.
- YouTube sync also checks recent RSS entries, so waiting rooms and premieres
  missing from the streams page get their start time. Premieres become
  releases, and finished streams and premieres keep YouTube's reported end.
- A lightweight live-status probe rechecks only the watch pages of up to five
  reservations that are starting or started and streams without a known end,
  on its own interval through the job queue. It records the actual start,
  the end, the reservation state and the live status, and updates the board
  only when something changed.
- A stream YouTube reports as live is no longer stored as completed.

</details>

<details>
<summary><strong>Public pages</strong></summary>

- A started premiere or release no longer shows as on air for three hours;
  only streams are presumed live without an end, and a presumed-live item
  gives the spotlight to a reservation starting within 30 minutes.
- On air follows YouTube's confirmed live state when the probe has a fresh
  result: a long stream stays on air past three hours, a late waiting room no
  longer shows as on air, and an ended stream leaves the stage. Without a
  result, the start-time grace still applies.

</details>
