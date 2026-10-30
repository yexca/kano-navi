# Unreleased

<details>
<summary><strong>Synchronization and extraction</strong></summary>

- The media step downloads the whole image queue in batches, up to 500 files
  or five minutes per run, instead of 20 files, so the images of a large X
  history fetch arrive within a few runs. A 429 answer ends the step early.
  The step records how many never-checked images are still `remaining` and
  which bound `stopped` it, and finishes `partial` while images remain.
- `MEDIA_DOWNLOAD_LIMIT` is removed from `.env.example` and the configuration
  page. The backend never read it; an existing `.env` entry can be deleted.
- YouTube recent sync asks the Data API about the whole detail queue, 50
  videos per call and at most five calls a run, so a 200-video count window
  is classified in one run instead of mostly waiting for watch pages.
- A watch page that asks the server to sign in (YouTube's bot check) records
  `youtube_watch_login_required` once and stops further watch page requests
  for that run, in recent sync and the live-status probe alike.

</details>

<details>
<summary><strong>Operator console</strong></summary>

- A run whose media step left images queued explains how many remain, and the
  step's counts list them as Remaining with the bound that stopped it.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- Release images are published for `linux/arm64` as well as `linux/amd64`
  under the same tags, so ARM hosts such as a Raspberry Pi 4/5 or an Apple
  silicon Mac pull a native image. The Dockerfiles cross-compile the Go
  backend for the target platform.

</details>
