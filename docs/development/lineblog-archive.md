# LINE BLOG Archive Maintenance

The [LINE BLOG archive](../user/lineblog.md) page shows the 99 articles of
Kano's 2015–2019 LINE BLOG with their pictures and videos. The content comes
from a maintainer's local recovery archive, which was rebuilt from Internet
Archive captures. This page covers where the content lives, how it is
packaged, and what an instance needs to show it.

## Content and Media

- `src/lineblog/posts/<article number>.json` holds one article: its article
  number, Japan-time publication instant, title, original address, the
  Wayback Machine capture it was recovered from, and its body as typed blocks
  (paragraphs of text, links and images; YouTube embeds; link cards; rules),
  plus the media catalog entries of the files it references. Paragraphs carry
  the archive's Simplified Chinese and English translation paragraphs. The shapes are in
  `src/lineblog/types.ts`.
- `src/lineblog/post-index.json` lists every article, newest first, with its
  number, publication instant, titles and the media name of its first
  archived picture or video cover. The page's lists, month archive, article
  neighbours and links between articles read only this index.
- `src/lineblog/media-catalog.json` lists each referenced media file with its
  MIME type, size, SHA-256 and, for images, the displayed width and height.
  It is the complete catalog, including the design art; the page itself
  reads the entries carried by each article's file.
- The media files themselves (about 1.02 GiB: 305 images and 25 videos, two
  of them over 100 MiB) are ignored runtime data in `data/lineblog/`. They are
  too large for the repository and the image. The server reads them through
  `GET /media/lineblog/<name>`, which serves only a regular file directly
  inside that directory whose name is 12 lowercase hex digits plus `.jpg`,
  `.png` or `.mp4`, with a fixed type for the extension, `nosniff`, a
  sandboxing CSP, byte ranges and conditional requests. Images whose bytes
  match their name are immutable and videos revalidate
  ([Immutable fixed files](../architecture/media-cache.md#immutable-fixed-files)).
  Images also have smaller copies, made on demand in the runtime variant
  cache ([Resized copies](../architecture/media-cache.md#resized-copies)):
  the recent-post thumbnails, profile photo, article pictures, smartphone
  list pictures, link-card pictures and video covers request them through
  `srcset` (a video poster
  takes one 960-pixel copy), while the image viewer loads the original.
- `src/lineblog/comments/<article number>.json` holds the comments of one
  article as an archived copy of its page listed them, newest first: each
  comment's number, the writer's display name, the Japan-time minute as an
  ISO instant, the text with `\n` line breaks, and the old comment form's
  star or face rating when it had one. Articles without archived comments
  have no file. See [Comments](#comments).
- `src/lineblog/content.ts` derives the page views (pages of three articles,
  months in Japan time, single articles) from the index, and the media URLs.
  `src/lineblog/post-files.ts` loads article files on demand, and a single
  article's comments file with it: each one builds into its own
  content-hashed chunk, so a view loads only the articles it shows, and the
  server caches them as immutable build files. A comments file that fails to
  load shows the article without comments.
- The blog's own header art and profile photos, and the menu bar's Home icon,
  are archive media like the articles' pictures: nine JPEGs and one PNG in the
  recovery archive's `media/`, packaged into `data/lineblog/` and the media
  catalog. The menu shows the icon in place of the word Home, as the blog did;
  the word stays its alternative text. `src/lineblog/designs.ts`
  lists each design with its media names, the original address of each file
  and the Wayback Machine capture its bytes come from. The table follows every
  index, article and listing capture of the blog: four headers, five profile
  photos, three accent colours and two profile texts, which changed at
  different times. A design applies from the Japan date of the first capture
  that shows it (the 2016 art from its own `?v=0318` stamp), and a page uses
  the design of the newest article it shows. Two profile photos were never
  archived (one from 2018-01 to 2018-05, one shown on 2018-12-04); those
  periods keep the photo before or after them. Each design also carries the
  official-site address of the menu bar, which changed with the 2018-05-18
  design. A reader can pick one design in place
  of the automatic one: the picker offers each run of designs with the same
  header, profile photo and accent once, applying the run's latest design,
  and the browser remembers the choice.

Archive media are named by the first 12 hex digits of a SHA-256: an image by
its bytes, a downloaded YouTube video by its watch address
(`https://www.youtube.com/watch?v=<id>`). Images the recovery archive kept
only as remote addresses stay as those addresses and show as not archived; the
page never loads them. Embedded tweets keep only their text and links.

`src/lineblog/recovered.ts` lists the pictures recovered after the archive was
made, each with the address its bytes came from. Their files sit in the
recovery archive's `media/`, and packaging stores them as archive media with
that address as the image's source:

- **Store thumbnails** replace their remote addresses, from Wayback Machine
  captures of the same addresses. Amazon renders a thumbnail anew for each
  request, so each is the same picture at the same address, not necessarily
  the same bytes the blog showed.
- **Stand-ins** replace three pictures of the 2017-11-18 article whose
  originals were never archived: the same official artist photo and bonus
  pictures from a news report of the same week. One original can take several
  stand-ins. The page labels them as stand-ins under the pictures.
- **Tweet pictures** follow the paragraph of an embedded tweet in the
  2017-11-01 article, as the tweet showed them; the page labels them as the
  tweet's pictures.

Two pictures of the 2017-11-18 article stay not archived: one from a deleted
tweet and one photo with no public copy.

## Packaging

To rebuild the article files, index, media catalog and `data/lineblog/` from
the recovery archive, run:

```bash
npm run lineblog:package -- --from /path/to/Kano_LINE_Blog_Recovery
```

The archive holds `BLOG/` (Japanese originals), `BLOG_zh-cn/` and `BLOG_en/`
(the same articles with Simplified Chinese or English translation lines
inserted; both are machine translations of the Japanese),
`media/`, and `media-parts/`,
where each video over 100 MiB is a gzip-compressed tar split into numbered
parts. Packaging:

1. Parses every article and keeps the highest retained version of each one.
2. Matches each translation file's lines against the Japanese ones; inserted
   lines become that language's translation paragraphs of the block before
   them. A translation file of another version than the article counts for
   nothing, and an archive without a translation folder leaves that language
   out.
3. Records each `[Image source]` line on the image it follows, keeps no local
   file paths, and reduces Amazon links to their product path, dropping
   affiliate and API query parts.
4. Validates every referenced media file before writing anything: name
   format, size limit (15 MiB per image, 256 MiB per video), magic bytes and
   name identity. The design art in `src/lineblog/designs.ts` and the
   recovered pictures in `src/lineblog/recovered.ts` go through the same
   checks. A video missing from `media/` is restored from its parts.
5. Writes the media into `data/lineblog/` through staged renames, skipping
   files whose bytes already match, and then writes the article files
   (removing those of articles the archive no longer holds), the index and
   the media catalog.

Packaging performs no network requests and does not change the recovery
archive. It leaves `src/lineblog/comments/` alone. Review the written files
and the page together when updating them.

## Comments

The recovery archive holds no comments; they come from Wayback Machine
captures of the article pages, which listed every comment in the page's
`コメント一覧`. To rebuild `src/lineblog/comments/` from a local directory of
raw captures (`.html` files saved with the `id_` form of the capture
address), run:

```bash
npm run lineblog:comments -- --from /path/to/captures
```

The script:

1. Reads every `.html` file of the directory, in file-name order, and
   recognises an article page by its canonical `lineblog.me/kano/archives/`
   address, not by its file name. Other pages are skipped.
2. Takes from each comment its number, the display name (the text of the
   `a.comment-url` link, or the plain name), the `YYYY/MM/DD HH:mm` date read
   as Japan time, the text with `<br>` as line breaks, and the rating picture
   as an icon and value. The commenter's link is dropped and never written.
   The two picture emoji the old comment form put in comment text become
   Unicode emoji; any other markup in a comment stops the run.
3. Keeps, for each article, the capture listing the most comments. A request
   for one capture can return a later one, so file names do not decide.
4. Writes a file for each article of `post-index.json` that has comments and
   removes the files of articles that have none.

The script performs no network requests. Review the comments of changed
articles on the page before keeping the files.

The page shows the comments under the article in the blog's comment-list
style, in Japanese with `lang="ja"`; only the heading follows the interface
language. Comment text is never machine translated.

## Smartphone Template

LINE BLOG served phones a separate smartphone template, `lite2`, at the same
addresses, chosen by the browser's user agent. The page shows that template
in windows up to 600 pixels wide (the upright width of the blog's phones was
320 to 480 pixels, and the blog sent tablets of 600 pixels and more its PC
page) and the PC layout above that; both use the same query, views and data.

No smartphone page of Kano's blog was archived, and no archived smartphone
article or article list exists for any LINE BLOG or livedoor Blog. The
template follows what was archived:

- The platform's `css/lite2/common.css` (2016-10 and 2017-09) and LINE
  BLOG's `css/lite2/usr/lineblog.css` (2015-12 to 2018-04) give the colours,
  sizes and selectors: grey page with white boxes, the menu bar, the article
  list and article, the article pager, the comment list, the round bottom
  pager, the monthly pull-down and the footer.
- A LINE BLOG smartphone page of another blog (2015-12) gives the header
  frame: the header image, the sideways-scrolling menu bar with its current
  item in the blog's accent colour, the one-line profile and its popup.
- livedoor Blog smartphone comment pages on the same template give the exact
  comment list, bottom pager and footer markup.

What the page infers: the order of an article-list entry (title, picture,
opening words, footer with the date) from the skin's list selectors; the
article pager's middle ホーム label, which the skin draws without text; the
100-pixel profile photo in the popup, from the template's later profile
header; and the footer line's text, the blog's name. Kano's phone header
(`sp_design/header.jpg`) was never archived, so the header shows the middle
1040 of the PC art's 2600 pixels, which holds the art's drawn title and its
figure in every design. The list shows no comment counts, because an
article's comments load only with the article; the article shows its count.

Two parts depart from the skin on purpose. The footer line keeps its 9px
text but sits on the page's own surface instead of the skin's fixed dark
grey, so it follows the theme. The monthly pull-down keeps the skin's strip
(月別アーカイブ, or the month shown, in 12px grey with the pull-down arrow),
but opens the site's menu instead of the browser's list: the months grouped
under their years, each with its article count, opening at the month shown.

The template's icons are the original files, kept byte for byte under
`public/assets/lineblog/lite2/` at their original paths below
`parts.lineblog.me/img/lite2/`. Each came from a Wayback Machine capture:

| File                                | Capture          | Used for                                   |
| ----------------------------------- | ---------------- | ------------------------------------------ |
| `icon_comment02.png`                | `20180304080525` | comment counts                             |
| `usr/lineblog/icon_pager_first.png` | `20180304081032` | bottom pager                               |
| `usr/lineblog/icon_pager_prev.png`  | `20180304081032` | bottom pager                               |
| `usr/lineblog/icon_pager_next.png`  | `20180304081032` | bottom pager                               |
| `usr/lineblog/icon_pager_last.png`  | `20180304081033` | bottom pager                               |
| `usr/lineblog/arrow_pull.png`       | `20171220035523` | article pager, monthly pull-down, page top |
| `usr/lineblog/btn_close.png`        | `20180219103323` | profile popup                              |

The icons are grey on white or on transparency; the page multiplies them
onto the light theme and inverts and screens them onto the dark one. The
template's upward page-top arrow was never archived, so the pull-down arrow
turns up in its place.

## Deployments

`data/lineblog/` belongs to the ignored `data/` directory, so an instance shows
the articles' text from the image and their media once that directory is in
place: run the packaging command on the host, or copy `data/lineblog/` from a
packaged checkout, and preserve it with the rest of `data/`. To carry it as
one file, bundle it in a packaged checkout and extract it in the directory
that holds the instance's `data/` (beside `docker-compose.yml` for Docker):

```bash
tar -cf lineblog-media.tar data/lineblog/*.jpg data/lineblog/*.png data/lineblog/*.mp4
```

```bash
tar -xf lineblog-media.tar
```

The files only need to be readable by the server; it reads them on request,
so no restart is needed. A missing file
shows an unavailable state on the page while keeping the original links;
without the design art the header and profile keep a plain background.

## Verification

Run `make ci-frontend`, `make go-test`, `make docs-check` and
`make sensitive-check`. `tests/lineblog.test.ts` packages a synthetic archive
(translation alignment, image sources, YouTube merging, split-video
restoration, rejection of mismatched media, the index and article files),
extracts comments from synthetic article pages (both name forms, ratings,
dropped links, the capture with the most comments), and checks the tracked
index, article files, comments files and media catalog against each other,
including every design's art, and the smartphone list's opening words;
`internal/httpapi/lineblog_test.go` covers the media
route. Browser verification covers the article list, months, single
articles with the 前の記事/次の記事 pager and their comments, back and forward (including after a reload),
the image viewer, video
playback, the menu bar's and each article's translation switch in Chinese and English, the design picker and each design's header and profile,
three languages and themes, the PC layout at desktop and tablet widths, and
the smartphone template at 320, 375 and 414 pixels and on either side of
600 (profile popup, monthly pull-down, both pagers).

## Related Docs

- [LINE BLOG archive](../user/lineblog.md)
- [Frontend](../architecture/frontend.md)
- [Media cache](../architecture/media-cache.md)
