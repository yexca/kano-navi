# Unreleased

<details>
<summary><strong>Public pages</strong></summary>

- A new Line Blog page brings back Kano's 2015–2019 LINE BLOG: 99 articles in their
  original Japanese with their photos, pictographs and archived YouTube
  videos, laid out like the original blog with its own header art, profile photos
  and font, pages, a monthly archive and links between articles. Each page
  shows the design the blog had when its articles were posted. Simplified Chinese and English readers get each
  paragraph's translation (the English ones are machine translations), which
  a menu switch hides or shows. The About page credits
  the fan recovery project the archive is built on.

</details>

<details>
<summary><strong>Runtime and development</strong></summary>

- `npm run lineblog:package -- --from <archive>` writes one file per article,
  an article index and the media catalog into `src/lineblog/` and the media
  into `data/lineblog/`, restoring split videos and validating every file
  before writing. The page loads only the articles it shows; the server
  serves the media from `/media/lineblog/<name>`.
- LINE BLOG archive images and the fixed history images get the same
  on-demand smaller copies as board media (`?w=` on `/media/lineblog/<name>`
  and `/assets/history/<file>`), kept in `data/cache/variants/`. Thumbnails
  and in-article pictures request them through `srcset`; image viewers keep
  the originals.

</details>
