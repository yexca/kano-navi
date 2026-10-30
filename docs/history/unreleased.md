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
- On phones and tablets, today's row in the weekly schedule shows its date in
  a berry disc with a "Today" label and an accent rule, matching the month
  view, and keeps its full date when nothing is scheduled. On a wide screen,
  a weekend today's date stays readable on the filled header.

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
- LINE BLOG archive images and history images requested by their hash, and
  their smaller copies, are cached by browsers as immutable once the server
  has checked their bytes against the hash; videos and other fixed files
  keep revalidating.
- The build writes Brotli and gzip copies of its HTML, JavaScript, CSS, JSON
  and SVG files, and the server sends the best one a browser accepts instead
  of compressing those files on the fly.
- The board loads as its own chunk like the other pages, with its copy in
  `src/dashboard/dashboard-i18n.ts`, so a direct visit to `/history`,
  `/lineblog` or `/about` no longer downloads its code (about 38 kB less
  gzip). On `/`, a script the build writes into `index.html` preloads the
  board's chunks alongside the entry.

</details>
