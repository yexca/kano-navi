# Unreleased

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- With `YOUTUBE_API_KEY` set, a failed YouTube RSS feed no longer leaves new
  Shorts unmarked: the step reads one page of the channel's Shorts playlist
  through the Data API (one more quota unit) and marks the listed or stored
  uploads it names as Shorts. The feed's failure is still recorded, and only
  the feed marks a video as not a Short.
- RSS feed failures record their own codes (`youtube_feed_http_error`,
  `youtube_feed_upstream_5xx`, `youtube_feed_unavailable`, …) instead of the
  `youtube_*` codes the streams page, uploads tab and watch pages keep. A
  YouTube `recent` window whose reservation refresh fails reports
  `youtube_recent_unavailable`.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- The run view explains the new YouTube feed codes in English and Chinese.

</details>
