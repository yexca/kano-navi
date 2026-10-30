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

</details>

<details>
<summary><strong>Operator console</strong></summary>

- A run whose media step left images queued explains how many remain, and the
  step's counts list them as Remaining with the bound that stopped it.

</details>
