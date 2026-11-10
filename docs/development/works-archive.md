# Works Collection Maintenance

The [Works](../user/works.md) page lists Kano's discography in three
sections: her own releases (solo and unit works), the works by other artists
she sang, wrote lyrics for or guested on, and her video-site uploads. The
records and cover images come from the maintainer's `kano_official`
reference collection. This page covers where that content lives, how it is
packaged, and how an instance gets its covers.

## Content and Media

- `src/works/personal.json` lists the personal works: Japan release date,
  title, format code (`album`, `single`, `ep` or `doujin`), whether it
  was released digitally, whether every edition was cancelled, the unit name
  for a unit work, label, catalog numbers, covers, the disc photo for the
  shelf and the cutout when the collection has them, the tracklist every
  edition shares (with disc names and tie-in notes such as the anime a song
  opens), the photos of the discs every edition shares, the editions, and the
  store bonuses for any edition.
- Each edition (such as a limited and a standard edition) has its name, its
  own Japan date when it came out after the release, whether it was
  cancelled, catalog number, JAN, price, format, packaging, covers, what
  comes in the package with its images, the store bonuses given with it
  (store, item and images), its full tracklist and the photos of its discs.
  An edition's tracklist is
  its own discs plus the shared discs whose number it does not replace, so a
  DVD adds a disc and an edition with a different CD replaces disc 1. A
  release sold in one plain version has no editions.
- A personal track also carries the videos of the uploads of the same
  recording: an upload whose albums name the release (with or without a
  trailing year) and whose song matches the track title, ignoring width,
  case, spacing, quotes and heart marks, and then parenthetical asides.
- `src/works/participation.json` lists the participation works: role codes
  (`vocal`, `lyrics`, `composition`, `voice`, `guest`), form codes (such as
  `digital`, `bonus`, `drama`), the group Kano belongs to when that group
  released the work, label, event, catalog number, the page the record
  follows, its cover, and every track with whether Kano is on it and any
  database credit for her (`lyrics`, `composition`, `arrangement`,
  `animation`, `other`).
- `src/works/uploads.json` lists the uploads: date, song, original artist,
  kind code (`cover`, `original`, `mv`, `project`, `compilation`, `game`,
  `composition`), collaborators, the niconico, YouTube, YouTube
  members-only and bilibili video
  IDs, the bilibili title, the albums that include the recording and, for a
  compilation, its songs. One recording posted to several sites is one
  record.
- `src/works/summary.json` holds the counts and the first and last years the
  page header shows before the longer sections load, and `updatedAt`, the
  Japan date the collection was checked through, which the header names
  under its caveat.
- `backend/internal/works/videos.json` is the server's copy of the upload
  list: `updatedAt` and each upload's ID, date and videos (`platform`
  `youtube`, `niconico` or `bilibili`, `id`, and `members` for a
  members-only YouTube video) in cover order: YouTube, YouTube
  members-only, niconico, then bilibili. The server embeds it to join the
  list with stored videos (see [Upload Covers](#upload-covers)).
- `src/works/media-catalog.json` lists each cover and bonus image with its
  MIME type, size, SHA-256, width and height.
- `src/works/types.ts` holds the shapes. Titles, credits, edition names,
  bonuses and packaging keep the collection's wording, in Japanese or
  Simplified Chinese; image captions are the collection's Simplified Chinese
  and the page marks them as such. Prices keep their wording, with yen and
  yuan amounts written with their signs. The interface translates only the
  codes.

Covers and bonus images are named by the first 12 hex
digits of the SHA-256 of their bytes plus `.jpg` or `.png`. The files (about 200 MiB) are ignored runtime data in
`data/works/`, too large for the repository and the image. The server reads
them through `GET /media/works/<name>`, which serves them like the LINE BLOG
archive media
([Immutable fixed files](../architecture/media-cache.md#immutable-fixed-files),
[Resized copies](../architecture/media-cache.md#resized-copies)): the grids,
rows and the release sheet request smaller copies through `srcset`, and the
image viewer loads the original. A cover that is not on the server shows an
unavailable state; the page never loads it from the address it came from.

The page bundles the personal works with its own code. The participation and
upload sections each load their code and data the first time a visitor opens
them, and a failed load offers to try again.

## Provenance

Personal-work covers and bonus images carry the collection manifest's
caption, the page the file was taken from and whether the file is a third-party copy (a
store, database or collector file of the release's own artwork) or the
publisher's file. The manifest's evidence class decides that flag; the
singer's own circle shop (bambi-no on BOOTH) and the label's
`whv-amusic.com/kano/` pages count as the publisher.

A photo from a second-hand marketplace listing (メルカリ, Yahoo!オークション,
Yahoo!フリマ) names the marketplace but does not link to the seller's
listing; the collection keeps the listing as evidence.

A file from a person's own copy names them instead of a page. The manifest's
`credit_role` is `scan`, `photo` or `fan`, with `credit_name` and an optional
`credit_url` (the person's profile, shown as a link on the name). A credited
file publishes no source page: its record's page is evidence for the
collection, and a fan's file keeps the site it came from private. A scan or
photo needs a name, a link needs a name, and any other role stops packaging.

Participation covers are third-party files; those the
collection matched against a music service or database name it. Release
dates are the first release's Japan date; doujin releases use the Comic
Market date.

## Upload Covers

Packaging publishes no video thumbnails; each instance caches them. The
upload list is the packaged one whatever an instance stored, and the server
joins it with its own data by video ID on every read
(`GET /api/works/uploads`, see
[Public API](../architecture/public-api.md#works-uploads)). An upload's cover
is the first of its videos, in cover order, with a ready image: the stored
video's own thumbnail, else the image the media step downloaded for the list
([Works upload covers](../architecture/media-cache.md#works-upload-covers)).
Until one is ready, and for an upload whose every video is gone, the page
prints a sleeve with its title. A stored YouTube or bilibili video also adds
its current title, length and who can watch it. The join never writes a video row or
a merge; a stored bilibili upload whose merge disagrees with the list's
pairing is listed for the operator
([Admin API](../architecture/admin-api.md#videos)).

## Uploads After the Collection Date

The packaged list stops at `updatedAt`. The server lists the uploads posted
after it on each read of `GET /api/works/uploads` (as `recent`, see
[Public API](../architecture/public-api.md#works-uploads)) and stores no
list of its own. A stored video is listed when all of these hold:

1. its Japan date, `COALESCE(scheduled_at, published_at)` in Asia/Tokyo, is
   after `updatedAt`;
2. public readers list it (a YouTube video, or an unmerged Bilibili upload
   whose form is settled, see
   [Video merging](../architecture/data-model.md#video-merging)), its shown
   availability is not `private` or `removed`, and it is no longer upcoming
   (a waiting premiere appears once it has aired);
3. it is not merged into another video (a merged Bilibili upload is its
   YouTube video's alternate);
4. its ID is not a video of `videos.json`, so a late re-upload of a listed
   work stays out;
5. the operator's Works decision (`videos.manual_work`) includes it, or,
   without a decision, its shown content tag is `song`, its form is
   `upload` or `premiere`, and it is not a Short.

The title inference asks about public Bilibili uploads as about YouTube
videos (see
[Video form and tags](../architecture/data-model.md#video-form-and-tags)),
so a Bilibili-only song upload can be listed without the operator.

The decision is the operator's override of rule 5. Include lists a song the
tag missed; exclude takes off a video tagged `song` that is not a work, such
as an album trailer or a crossfade. Neither overrides rules 1 to 4: a
decision on an earlier video stays stored and has no effect. The Site page
of the console lists the listed and excluded videos with their origin, and
the video editor sets the decision on any video
([Admin API](../architecture/admin-api.md#videos)); a change advances the
dashboard revision.

A release that moves `updatedAt` drops the earlier videos by rule 1, and
rule 4 drops those the new list names; nothing is cleaned up. Each read
scans the videos after `updatedAt` and returns the newest 48 listed ones
with the count of all, and the console reviews the newest 200; a list left
without a release for long therefore stays bounded, while the block names
how many it holds. Covers are the videos' cached thumbnails; no lookup or
download runs for them. The page shows the listed videos as their own block
above the packaged list, each marked as found automatically or added by
hand, outside the kind filters and the header's counts. The block shows two
rows of its grid (four rows of the phone list) until the reader opens the
rest. Works no source reads, such as a niconico-only
upload or one on another creator's channel, wait for the next collection
update.

## Store Listings

Where a release is streamed or sold as a download comes from the optional
`discography_stores.csv` (`date`, `release`, `edition`, `item`, `platform`,
`url`; the collection also keeps `checked_at` and `notes`, which are not
published). A row without an edition belongs to every edition; a row with
one belongs to that edition alone, such as a 先行配信 download. `item` names
a digital item beyond a release's or edition's main one, such as a TV-size
or instrumental single; an edition may show only one unnamed item between
its own rows and the release's. `platform` is one of `smart_link` (the
official page listing every store), `apple_music`, `spotify`,
`amazon_music`, `line_music`, `youtube_music`, `awa`, `itunes`, `mora`,
`recochoku`, `ototoy`, `qq_music`, `kugou` or `label_page`, the order the
page lists them in; `mora_hires` and `ototoy_hires` mark their store's
listing as also sold in high resolution. Each address must be HTTPS on its
platform's host with no query, fragment or credentials, except a YouTube
Music album playlist's `list`, so no referral or sharer's token is
published. Only listings whose label and rights line match the collection's
record belong in the table.

## Disc Photos and Cutouts

Disc photos come from the optional `discography_discs.csv` (`date`,
`release`, `edition`, `disc`, `image_file`), with each image in the
manifest. A row without an edition is a shared disc: every edition that keeps
that disc shows it, except a digital edition and an edition that replaces
the disc with its own tracks. A row with an edition is that edition's own
disc, such as a limited edition's DVD or a CD whose design differs. Packaging
stops at a row whose release, edition or disc does not exist. Disc photos
show beside their discs and leave the cover lists, unless a release has no
other image; then its disc is its cover.

A personal release's record on the shelf shows a photo of the real disc when
the collection has one: the file in an optional `disc_file` column of
`discography.csv`, else the first disc 1 photo (shared, then by edition),
else an image whose manifest caption is `盘面`. In a release's sheet, the
record is the chosen edition's disc 1 photo, and the sheet opens on the
edition with the most disc photos, the earlier one on a tie. A later edition
that holds every disc photo of the earlier pick plus more (a store set of
the limited edition with a bonus disc) bundles it and does not take over.
Without a disc photo, a record prints the
cover on a disc where no disc was pressed: a download or withdrawn edition,
and a release whose editions are all such, or a download sold in one
version. The optional `printed_disc` column of `discography.csv` asks for
the same with `封面` (any other value stops packaging). Other records are
drawn.

A figure cut out of a release's cover goes in `抠图/<release title>.png`: a
PNG with an alpha channel, named by the release's exact title. The page
header shows the cutouts of the acoustic cover trilogy. A file in `抠图/`
that names no personal release, or that is not a transparent PNG, stops
packaging.

## Published Fields

The collection's notes columns are the maintainer's checking record, so
packaging publishes none of them: no release, edition, participation or
upload notes and no participation track notes. The one fact it takes from an
edition note is a later edition date written as `YYYY-MM-DD 发售` alone. In the fields it does publish, packaging
removes the parentheticals that say where a fact was checked or where a
cover file came from (a database or shop name, "NAS" for the maintainer's
own discs, a file's size, how two official pages disagree), and drops a
personal track note that only records an open question and a package line
that only says there is no bonus. A catalog number or event the collection
marks as doubtful is left blank, and an event keeps only its name. Packaging stops
when checking wording remains in a published field. Share links that carry a
sharer's token (Taobao `m.tb.cn`) are dropped from records.

## Packaging

To rebuild the tables and `data/works/` from a local copy of the collection,
run:

```bash
npm run works:package -- --from /path/to/kano_official
```

`updatedAt` is the newest upload's date. When the collection was checked
through a later day, name it with `--through YYYY-MM-DD`; a date before the
newest upload stops packaging.

Packaging reads `discography.csv`, `discography_editions.csv`,
`discography_store_bonuses.csv`, `discography_tracks.csv`, `manifest.json`,
the optional `discography_discs.csv`, the optional
`discography_stores.csv`, the optional `抠图/` directory,
`参与专辑/participation.csv`,
`参与专辑/participation_tracks.csv` and `投稿作品/works.csv`. It reads the
whole selection before writing anything: every table must have its columns,
every date must be a Japan date, every category, kind, role, form, credit
and upload kind must map to a code, every edition, track and bonus row must
name a listed release and edition, every store listing must have a known
platform and a canonical address, every video ID must have its platform's
shape, and every image must be listed in the manifest and be a JPEG or PNG
inside the collection within the size limit. An image in another format
(such as a GIF) is left out and named in packaging's output. An upload row
whose optional `format` column is `Shorts` is a YouTube Short, which the page
does not list, so packaging leaves it out; any other non-empty `format` stops
packaging. Covers already in `data/works/` whose size and hash match are
left alone. Packaging also writes `backend/internal/works/videos.json`; a run
with another content directory must name its own index file. Packaging performs no network requests and does not change the
collection.

The [Journey](history-archive.md) page names releases by their `id` and
shows their covers, so `tests/history-media.test.ts` fails when packaging
changes the ID or covers of a named release; update the milestone with it.

When moving a deployment, preserve the ignored `data/` directory, or run the
packaging again on the new machine.

## Related Docs

- [Frontend](../architecture/frontend.md)
- [Media cache](../architecture/media-cache.md)
- [Testing](testing.md)
