# Unreleased

<details>
<summary><strong>Public pages</strong></summary>

- A new Line Blog page brings back Kano's 2015–2019 LINE BLOG: 99 articles in their
  original Japanese with their photos, pictographs and archived YouTube
  videos, laid out like the original blog with its own header art, profile photos
  and font, pages, a monthly archive and links between articles. Each page
  shows the design the blog had when its articles were posted. Simplified Chinese readers get each paragraph's
  translation, which a menu switch hides or shows. The About page credits
  the fan recovery project the archive is built on.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- `npm run lineblog:package -- --from <archive>` writes the article and media
  catalogs into `src/lineblog/` and the media into `data/lineblog/`, restoring
  split videos and validating every file before writing. The server serves
  them from `/media/lineblog/<name>`.

</details>
