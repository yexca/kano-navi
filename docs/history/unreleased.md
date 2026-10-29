# Unreleased

> [!NOTE]
> A YouTube premiere already stored as a stream stays one until an operator
> requests a refresh of that video in `/admin`; the next YouTube run then
> reads its watch page and turns it into a work.

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
- A YouTube premiere found before it starts is no longer stored as a stream
  for good. A Data API answer without a length, or a watch page that reads as
  live content, settles a stream verdict only once the broadcast is over, so a
  premiere becomes a work (`release` event, title kind) after it ends. An
  operator's refresh request reads the watch page even when the Data API
  describes the video, which corrects a premiere already stored as a stream.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- The run view explains the new YouTube feed codes in English and Chinese.

</details>
