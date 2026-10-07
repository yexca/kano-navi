# History and Visual Archive

`/history` contains a multilingual timeline of 44 milestones from 2010 onward
and 59 images from the maintainer's `kano_official` reference collection,
reviewed in October 2026. Visitors can filter by activity identity, jump to a
year, or browse the gallery by image type. Image dialogs show the full file,
dimensions, date basis, recorded credit, and original-source links.

## Content and Provenance

- `src/history/milestones.js` owns milestone copy, dates, identity, and evidence
  links. Year, month, day, and range precision is preserved.
- `src/history/media-catalog.json` contains public titles in three languages,
  related milestones, identities, image type, source page and file URLs,
  dimensions, MIME type, size, SHA-256, source identity, and date basis.
  Original local paths are excluded.
- `src/history/media.js` groups images and supplies versioned local
  `/media/<id>?v=<sha256>` URLs.
- `src/history/history-i18n.js` supplies image and provenance translations.

Release dates, Japan-time post dates, stream dates, depicted outfit reveals,
and acquisition dates are distinct. Unknown dates remain unknown. Mahoro's
first stream is May 29, 2021, while the character-introduction image is dated
to its May 30 post. The current agency full-body image is a September 30, 2026
website capture, separate from the August 22 debut stream.

Four official-stream frames are explicitly labelled as sourced from a
third-party archive, with original stream URLs and frame timestamps. Cover
art, product illustrations, promotional visuals, and model demonstrations
remain distinct. Illustrations alone do not establish a new streaming model.
Agency press-release images display their requested publisher credit.

## Offline Import

Image bytes remain ignored runtime data, outside Git and the frontend bundle.
With the reference collection available locally, run:

```bash
npm run history:import -- --from /path/to/kano_official
```

The importer locates files through `manifest.json` and validates the entire
selection before writing. It checks real paths stay inside the collection,
size limits, SHA-256 identities, and image magic bytes. It neither downloads
files nor changes the reference collection.

The existing atomic media writer stores X files in `data/x/`, archived stream
frames and video thumbnails in `data/youtube/`, and other publisher files in
the supported `data/cache/media/` root. Original bytes are preserved. Rows use
`media_assets` and `media_links.owner_type = history-image`. Database changes
are transactional; repeated imports reuse identities. Failed validation
retains the previous snapshot. No schema change or startup import is required.

Run imports in the server's environment. For Docker, make the collection
available inside the container and run the importer there. Avoid simultaneous
Windows/Linux access to the same SQLite file. Restart the development service
after an offline import if it holds an older database connection.

When moving a deployment, preserve the ignored `data/` directory, including
its database and media. A fresh checkout displays text and source links but
needs the import for images. Missing local files show an unavailable state
while retaining original-source links.

## Requests and Verification

The page has no data-fetching hook or external embeds. Gallery and timeline
images are lazy loaded from the guarded local media route; the introductory
portrait is loaded eagerly. Original websites open only when visitors follow
a link. Provenance hosts are documented in the privacy allowlist without
expanding the remote downloader's destination policy.

Run `npm run build`, `npm run test:server`, `make check-docs`, and
`make check-sensitive`. Importer tests use synthetic archives without network
requests. Browser verification covers identity/image-type filters, year
navigation, dialog focus and Escape, source links, three languages, themes,
and desktop/mobile layouts.
