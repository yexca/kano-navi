# Media Cache

## Purpose

Platform media is runtime data, not a source-controlled frontend asset. X and
YouTube URLs can expire, change, or become rate-limited, and loading them from
the browser would make every page view a remote request. The media layer gives
each discovered remote URL a stable database identity and downloads it during
server-side synchronization.

## Runtime Layout

```text
data/
  database/
    kano.sqlite
  x/
    sha256/<first-two>/<content-sha256>.<extension>
  youtube/
    sha256/<first-two>/<content-sha256>.<extension>
  bilibili/
    sha256/<first-two>/<content-sha256>.<extension>
  avatar/
    avatar.<extension>
    banner.<extension>
    sha256/<first-two>/<content-sha256>.<extension>
  lineblog/
    <12-hex-name>.<jpg|png|mp4>
  cache/media/
    sha256/<first-two>/<content-sha256>.<extension>
  cache/variants/
    <first-two>/<content-sha256>-<width>.<jpg|png|orig>
```

The whole `data/` directory is ignored by Git. It is created on demand by the
server or synchronization command. Each write goes to a randomly named
temporary file in its target directory (`.download-*`, `.part-*` or
`.variant-*`) that is atomically renamed into place. The tracked
`public/assets/` directory is reserved for fixed fallback branding and the
reviewed history collection.

Discovered X and YouTube profile candidates stay in their source namespaces;
local uploads (`data/avatar/sha256/`) and the selected avatar and banner files
remain runtime data in `data/avatar/`. A `cache_path` that starts with
`sha256/` resolves under `data/cache/media/`; readers serve such rows, while
the downloaders write only the source namespaces. The curated history
collection ships as verified fixed files in `public/assets/history/`; it needs
no runtime database rows. See
[History archive maintenance](../development/history-archive.md) for packaging and
provenance details.

## LINE BLOG Archive Media

`data/lineblog/` holds the recovered LINE BLOG images and videos, written only
by the maintainer's packaging command
([LINE BLOG archive maintenance](../development/lineblog-archive.md)). They
have no database rows: the tracked `src/lineblog/media-catalog.json` lists
them. `GET /media/lineblog/<name>` (and `HEAD`) opens the directory as a root
and serves only a regular file directly inside it whose name is 12 lowercase
hex digits and `.jpg`, `.png` or `.mp4`; the type comes from the extension.
Responses carry `nosniff`, `default-src 'none'; sandbox`, an ETag of the name
and size, byte ranges and conditional requests; images whose bytes match
their name are immutable and everything else revalidates
([Immutable Fixed Files](#immutable-fixed-files)). Any
other name, a directory, a symlink or a missing file answers `404`. The
server never writes this directory. Its images have smaller copies (see
[Resized Copies](#resized-copies)); videos ignore `w`.

## Database Model

`media_assets` stores one row per normalized HTTP(S) source URL:

- `id`: the lowercase hex SHA-256 of the normalized source URL (HTTP or HTTPS,
  no userinfo, lowercase host, no fragment, an empty path becomes `/`), so a
  URL always maps to the same row. A row registered under a short
  `x-<32 hex>` identity is re-keyed to this identity, together with its links
  and cache metadata, when its URL is registered again;
- `source`, `source_url`: the platform adapter and original public URL;
- `status`: `pending`, `ready`, `missing`, or `failed`;
- `cache_path`: a path relative to the runtime `data/` directory, under `x/`,
  `youtube/`, `bilibili/` or `avatar/`, or a `sha256/` path under
  `cache/media/`; never an absolute path or one with `..`;
- `mime_type`, `extension`, `byte_size`, `sha256`, `width`, and `height`
  (read from the image header; `0×0` marks a header that could not be read,
  such as AVIF, and reads report it as unknown);
- fetch/check/seen timestamps, validators (`etag`, `last_modified`), and the
  last error for maintainer diagnosis.
- `retry_attempts` and `next_retry_at`: persisted failure backoff.

`media_links` associates an asset with a domain record without coupling the
media table to every content table. Its owner tuple is `(owner_type, owner_id,
role, position)`, for example `post / 209... / post-image / 0` or `video / abc /
thumbnail / 0`. A post's own images use `post-image`; the images of a status
it quotes use `quote-image` under the same post, so the board shows them in
the quote card and extraction never reads them (see
[Quoted posts](sources.md#quoted-posts)).

The `media_url`, `thumbnail_url`, and image URL columns of content tables are
source-snapshot fields and are not treated as safe browser URLs by the
dashboard query.

The cache accepts only AVIF, GIF, JPEG, PNG, and WebP. SVG and
arbitrary document MIME types are rejected so cached platform input cannot be
served as active same-origin content.

## Lifecycle

1. A source adapter discovers a remote image, and its snapshot write
   registers it inside the same transaction (`replaceMediaLink` in
   `internal/store/sqlite/source_write.go`).
2. The candidate is deduplicated by normalized source URL, recorded as
   `pending`, and linked to its owner. Registration does not download bytes.
3. The media step claims at most 20 assets during a synchronization.
   Never-checked assets come first, ordered by their newest owner (a video's
   scheduled or published time, or the publication time of the post or
   schedule post that links it), so thumbnails the board shows first are not
   left pending by a same-synchronization tie. Already checked assets and
   assets without a dated owner follow by creation or last-check time. It
   downloads within the [download bounds](#download-boundary), writes the
   bytes through a root confined to `data/` into
   `<source>/sha256/<first-two>/<content-sha256>.<extension>`, reuses an
   existing file there only when its hash agrees, and records the row as
   `ready` with the width and height its header declares
   (`SaveMediaDownload` in `internal/store/sqlite/media_commands.go`). The same
   step measures up to 50 ready files cached before dimensions were probed,
   reading each contained, hash-checked file's header only, and reports them
   as `measured`; a run that measures any advances the dashboard revision.
4. A failed or unavailable request updates the row to `failed` or `missing`.
   Existing ready files are retained; a failed synchronization never clears a
   known snapshot. Transient failures (`media_timeout`, `media_unavailable`,
   `media_body_unavailable` and `media_rate_limited`) back off from 30 minutes,
   doubling to a maximum of 24 hours. Missing files and other HTTP errors are
   retried after 24 hours. Rejected destinations, invalid images and oversized
   bodies are not retried automatically. Rediscovering the same URL preserves
   its retry deadline, including for ready files whose revalidation failed.
   A successful download or 304 revalidation clears the failure state.
5. A YouTube thumbnail that answers 404 for `maxresdefault` or `sddefault`
   (including their `_live` forms) is requested at the next smaller size,
   ending with `hqdefault.jpg`. The bytes are stored under the original URL, so
   the asset identity and links do not change.
6. A ready YouTube thumbnail linked to an upcoming video, or to one that
   started or was published within the last 24 hours, is revalidated at most
   every 30 minutes with its stored `ETag` and `Last-Modified`. So is the
   thumbnail of any video whose watch page was read after the file was last
   checked (a [follow-up check or refresh request](sources.md#follow-up-checks-and-refresh-requests)),
   whatever its age. A 304 only records the check; new bytes become the next
   content-addressed version, and only a changed hash advances the dashboard
   revision.
7. The file a revalidation replaces stays in the cache. `media_versions`
   keeps its hash, path, MIME type, size and fetch time under the asset ID,
   and every video linked to the asset records the replaced thumbnail in
   `video_revisions`. An operator can pin such a version, or the file of an
   earlier thumbnail URL, from the video's history.

### Download Boundary

`internal/mediadownload` accepts only HTTPS URLs on port 443 on X's
`*.twimg.com` media CDN subdomains, YouTube's `*.ytimg.com` thumbnail CDN
subdomains and Bilibili's `*.hdslb.com` cover CDN subdomains (`i0`, `i1`, `i2`
and so on). The suffix check requires a real subdomain boundary, so
lookalike hosts and the bare parent domains are rejected, as are userinfo,
fragments and query keys that look like credentials (`token`, `secret`,
`auth`, `api_key` and the like). Redirects are followed by hand: at most three,
each revalidated against the same policy. One ten-second deadline covers the
redirects, headers and body of a request (a smaller YouTube thumbnail size
gets its own). Bodies are capped at 10 MiB, and the sniffed magic bytes must
be a supported image whose type agrees with a declared image `Content-Type`.
Failures become fixed codes (`media_destination_rejected`,
`media_redirect_rejected`, `media_timeout`, `media_missing`,
`media_response_oversized`, `media_invalid_image` and so on); no upstream body
is stored.

The host policy is kept in code because this is also an SSRF boundary; the
broader privacy allowlist is not a download permission list. The cache
accepts image MIME types only, so X video URLs may be recognized as official
media while remaining unavailable to the image cache. A failed redownload
keeps the ready file, and versioned public reads never fetch media. Cached
files, including versions a revalidation replaced, are kept deliberately;
there is no garbage collection.

Content-addressed paths are deliberately preferred over layouts such as
`pics/x/<timestamp>` or `pics/youtube/<video-id>`. Ownership already lives in
`media_links`; storing bytes by hash deduplicates identical files, makes path
validation deterministic, and allows atomic replacement without trusting a
platform identifier as a path component.

## Selected Profile Media

Profile discovery reads at most 4 MiB of page bytes. Candidate downloads read
at most 15 MiB of image bytes, verify MIME and magic bytes, and follow at most
three independently allowlisted redirects under one deadline covering headers
and the complete body. Timeout/oversize failures cancel the response.

A candidate download and an explicit selection are separate operations. A failed
redownload records its outcome/error while keeping any ready candidate and
selected file available. A successful redownload updates the candidate hash and
MIME only; the selected file keeps its own active hash/MIME and versioned public
URL until the operator selects it. `/media/profile/:slot` validates and serves
that selected version, so its bytes, ETag, and URL hash agree.

Startup repairs selected hash/MIME from the selected file's bytes rather than
the candidate, whatever the stored active metadata says. This is
idempotent and retains the manual selection and candidate version. Reads require
both the stored path and its symlink-resolved target to stay inside `data/avatar/`,
a regular file under the 15 MiB profile limit, and the existing supported-image
magic-byte validation. Unavailable/illegal files clear only active hash/MIME,
leaving other snapshots and configuration intact. HTTP uses the same bounded
reader, checks its hash/MIME against active metadata, and sends those exact bytes;
an unexpected file replacement returns 404 until reconciled or selected again.

## HTTP Contract

The server exposes `GET /media/<id>` for ready assets only. The ID must match
the opaque 64-character format, the database row must be `ready`, and the
resolved file must remain under the cache root. The response never accepts a
filesystem path or a source URL from the request. Missing, unsafe, or unready
assets return `404`. A `v` that names a `media_versions` hash of a ready asset
serves that kept earlier file under the same containment and hash checks; any
other version answers `404`.

Dashboard media fields include the safe `...Url` (a
`/media/<id>?v=<content-sha256>` route or a tracked `/assets/...` fallback), the
`...Status`, the opaque ID, and the public source URL for traceability. The
version parameter changes whenever bytes change, so versioned responses can be
cached as immutable even though the source identity is stable. A bare media ID
is revalidated, and an unready remote URL is represented by `null`, so the
browser does not silently fall back to a CDN.

## Resized Copies

A `w` query parameter asks for a smaller copy of an image:

| Request                                         | Original                                                                  |
| ----------------------------------------------- | ------------------------------------------------------------------------- |
| `GET /media/<id>?v=<hash>&w=<width>`            | a ready asset                                                             |
| `GET /media/lineblog/<name>?w=<width>`          | a `.jpg` or `.png` file in `data/lineblog/`                               |
| `GET /assets/history/<file>?v=<hash>&w=<width>` | a `.jpg`, `.jpeg`, `.png` or `.webp` file of the built history collection |

`w` must be one of 320, 640, 960, 1280 or 1920; any other value, or a
repeated `w`, answers 404. A LINE BLOG video ignores a valid `w`, and other
static files ignore `w` entirely.

All three share one cache keyed by the SHA-256 of the original's bytes, so an
image found in several places is resized once. The archive and history files
have no database rows; the server reads such a file (at most 10 MiB, contained
in its directory like the original's own response) and remembers its hash by
path, size and modification time, so a cached copy is found without reading
the original again and a replaced file is hashed anew. A copy is stored only
when the bytes read match the hash it is stored under.

The first request decodes the original (JPEG, PNG or WebP no larger than 40
megapixels, at most two at a time; concurrent requests for the same copy wait
for the first and then read it from the cache), scales it to that width with
Catmull-Rom filtering, encodes JPEG at quality 82 (or PNG when the image has
transparency) and stores it atomically in
`data/cache/variants/<first-two>/<hash>-<width>.<jpg|png>`. When the original
is no wider than the request, is a GIF (possibly animated) or AVIF, cannot be
decoded, or the copy would not be smaller, an empty `.orig` marker records
that the original is served for that width, so it is not decoded again.

The response is always an image: a copy, or the original whenever a copy
cannot be made or stored (a read-only data directory only means the copy is
made again next time; a process without the built site, such as the Vite
development server's API, serves history files without copies). It carries
`nosniff`, `default-src 'none'; sandbox`, range and conditional handling and
the copy's own hash as its ETag. A `/media/<id>` copy is immutable when `v` is
given; archive and history copies are cached like their originals
([Immutable Fixed Files](#immutable-fixed-files)). Originals are never
changed; the variant directory is a derived cache that may be deleted at any
time.

Pages request copies through `srcset` and `sizes` with the widths below an
image's known width (every width when it is unknown) plus the original; the
full-image viewers keep loading the original. Profile media and other bundled
`/assets/` files have no copies.

## Immutable Fixed Files

LINE BLOG archive images and the fixed history files are addressed by their
bytes' SHA-256, so the server lets browsers keep them for a year without
revalidating (`public, max-age=31536000, immutable`) once it has checked that
the bytes match the address:

| Request                                             | Immutable when                                                          |
| --------------------------------------------------- | ----------------------------------------------------------------------- |
| `GET /media/lineblog/<name>.(jpg\|png)[?w=<width>]` | the file's SHA-256 starts with the name's 12 hex digits                 |
| `GET /assets/history/<file>?v=<hash>[&w=<width>]`   | there is exactly one `v`, 64 lowercase hex digits, equal to the SHA-256 |

Everything else answers `public, max-age=0, must-revalidate`: LINE BLOG
videos (named after their watch URL, not their bytes), an archive image whose
bytes do not match its name, a history file without `v`, or with a repeated,
malformed or different `v`. A copy (`w`) takes its original's verdict, since
the variant cache only stores a copy made from bytes with the hash it is
filed under. The server reads the file to hash it and remembers the hash by
path, size and modification time like the variant cache; a replaced file is
hashed anew, so its old addresses revalidate.

## Verification

Run `make go-test` for path, identity, registration, downloader-host,
MIME, and dashboard contract tests. `make check` runs those tests together with
the sensitive scan, documentation checks, and the frontend build. Tests use
synthetic response bodies; CI does not download live media.

## Complete Post Media Declarations

A successful complete post update replaces only its `post-image` and
`quote-image` links inside the post transaction, preserving declared order and
removing vanished positions; an X API window, which does not read quotes,
keeps the `quote-image` links.
An explicit empty media list clears the links and primary source URL; an omitted
media declaration retains the previous snapshot. Failed reads make no post write.
Unreferenced cached bytes may be retained, but neither post display nor extraction
uses those old links. Extraction waits for every declared image and its contained
regular cache file; one ready image cannot stand in for a multi-image post.
