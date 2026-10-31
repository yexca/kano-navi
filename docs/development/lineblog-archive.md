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
  the recent-post thumbnails, profile photo, article pictures, link-card
  pictures and video covers request them through `srcset` (a video poster
  takes one 960-pixel copy), while the image viewer loads the original.
- `src/lineblog/content.ts` derives the page views (pages of three articles,
  months in Japan time, single articles) from the index, and the media URLs.
  `src/lineblog/post-files.ts` loads article files on demand: each one builds
  into its own content-hashed chunk, so a view loads only the articles it
  shows, and the server caches them as immutable build files.
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
  official-site address of the menu bar. A reader can pick one design in place
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
archive. Review the written files and the page together when updating them.

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
restoration, rejection of mismatched media, the index and article files) and
checks the tracked index, article files and media catalog against each
other, including every design's art;
`internal/httpapi/lineblog_test.go` covers the media
route. Browser verification covers the article list, months, single
articles with their neighbours, back and forward (including after a reload),
the image viewer, video
playback, the menu bar's and each article's translation switch in Chinese and English, the design picker and each design's header and profile,
three languages, themes, and desktop and mobile layouts.

## Related Docs

- [LINE BLOG archive](../user/lineblog.md)
- [Frontend](../architecture/frontend.md)
- [Media cache](../architecture/media-cache.md)
