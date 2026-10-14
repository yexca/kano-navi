# History and Visual Archive

`/history` contains a multilingual timeline of 44 milestones from 2010 onward
and 59 images from the maintainer's `kano_official` reference collection,
reviewed in October 2026. Visitors can filter by activity identity, jump to a
year from the sticky chronology strip (one dot per milestone, with empty years
folded into a gap), or browse the gallery by image type. Timeline cards show
compact thumbnails; image dialogs show the full file, dimensions, date basis,
recorded credit, and original-source links.

## Content and Provenance

- `src/history/milestones.ts` owns milestone copy, dates, identity, and evidence
  links. Year, month, day, and range precision is preserved.
- `src/history/media-catalog.json` contains public titles in three languages,
  related milestones, identities, image type, source page and file URLs,
  dimensions, MIME type, size, SHA-256, source identity, and date basis.
  Original local paths are excluded.
- `src/history/media.ts` groups images and supplies versioned local
  `/assets/history/<filename>?v=<sha256>` URLs.
- `public/assets/history/` contains the 59 verified original image files,
  copied by Vite into `dist/` and included in production Docker images.
- `src/history/history-i18n.ts` supplies image and provenance translations.

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

## Fixed Image Packaging

The reviewed history collection is fixed site content, tracked with the
project. A fresh checkout, production build, or container displays these images
without database media rows, runtime downloads, or an extra import. The
collection contains about 62 MiB of original bytes. Changing X/YouTube images
and operator-selected profile media remain ignored runtime data.

To update the fixed collection from the local reference archive, run:

```bash
npm run history:package -- --from /path/to/kano_official
```

Packaging validates the entire selection before writing: manifest entries, contained real paths, size limits, SHA-256
identities, and image magic bytes must match the catalog. Original bytes,
credits, and provenance are preserved. Packaging performs no network requests
and does not change the reference collection or runtime SQLite database.
Review the catalog and packaged assets together when updating the collection.

The runtime-cache importer of releases up to 0.1.3 (`history:import`), which
copied these images into `media_assets` for the legacy opaque-ID `/media`
URLs, was removed with the TypeScript backend. Rows it created in an existing
snapshot are kept and still served; `/history` does not use them.

When moving a deployment, preserve the ignored `data/` directory for source
snapshots, dynamic media, and provider configuration. History images are
supplied by the project and image. Missing fixed files show an unavailable
state while retaining original-source links.

## Requests and Verification

The page has no data-fetching hook or external embeds. Gallery and timeline
images are lazy loaded from the local static asset route; the introductory
portrait is loaded eagerly. Original websites open only when visitors follow
a link. Provenance hosts are documented in the privacy allowlist without
expanding the remote downloader's destination policy.

Run `make ci-frontend`, `make docs-check`, and `make sensitive-check`.
`tests/history-media.test.ts` checks the catalog, the shipped bytes and
packaging with synthetic archives and without network requests. Browser verification covers identity/image-type filters, year
navigation, dialog focus and Escape, source links, three languages, themes,
and desktop/mobile layouts.
