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
  .tmp/
    <temporary-file>.part
  cache/media/
    sha256/<first-two>/<content-sha256>.<extension>
  cache/variants/
    <first-two>/<content-sha256>-<width>.<jpg|png|orig>
```

The whole `data/` directory is ignored by Git. It is created on demand by the
server or synchronization command. The tracked `public/assets/` directory is
reserved for fixed fallback branding and the reviewed history collection.
Discovered X and YouTube profile
candidates stay in their source namespaces; local uploads and the selected
avatar and banner files remain runtime data in `data/avatar/`. The curated
history collection ships as verified fixed files in `public/assets/history/`;
it needs no runtime database rows. The optional legacy importer still stores
original publisher files in `data/cache/media/`, with X and video sources in
their source namespaces. See [History and visual archive](../product/history.md) for
packaging and provenance details.

## Database Model

`media_assets` stores one row per normalized HTTP(S) source URL:

- `id`: a 64-character SHA-256 identity derived from the source URL;
- `source`, `source_url`: the platform adapter and original public URL;
- `status`: `pending`, `ready`, `missing`, or `failed`;
- `cache_path`: a path relative to the runtime `data/` directory, normally
  `x/`, `youtube/`, or the legacy `cache/media/` namespace; never an absolute
  path;
- `mime_type`, `extension`, `byte_size`, `sha256`, `width`, and `height`
  (read from the image header; `0×0` marks a header that could not be read,
  such as AVIF, and reads report it as unknown);
- fetch/check/seen timestamps, validators (`etag`, `last_modified`), and the
  last error for maintainer diagnosis.
- `retry_attempts` and `next_retry_at`: persisted failure backoff, added to
  existing snapshots without clearing cached assets or links.

`media_links` associates an asset with a domain record without coupling the
media table to every content table. Its owner tuple is `(owner_type, owner_id,
role, position)`, for example `post / 209... / post-image / 0` or `video / abc /
thumbnail / 0`. A post's own images use `post-image`; the images of a status
it quotes use `quote-image` under the same post, so the board shows them in
the quote card and extraction never reads them (see
[Quoted posts](sources.md#quoted-posts)).

The existing `media_url`, `thumbnail_url`, and image URL columns remain during
the migration. They are source-snapshot fields and are not treated as safe
browser URLs by the dashboard query.

The cache accepts only AVIF, GIF, JPEG, PNG, and WebP. SVG and
arbitrary document MIME types are rejected so cached platform input cannot be
served as active same-origin content.

## Lifecycle

1. A seed record or source adapter discovers a remote image and calls
   `registerMediaCandidates`.
2. The candidate is deduplicated by normalized source URL, recorded as
   `pending`, and linked to its owner. Registration does not download bytes.
3. The media step claims a bounded batch during a synchronization.
   Never-checked assets come first, ordered by their newest owner (a video's
   scheduled or published time, or the publication time of the post or
   schedule post that links it), so thumbnails the board shows first are not
   left pending by a same-synchronization tie. Previously checked assets and
   assets without a dated owner follow by creation or last-check time. It
   downloads with a timeout and byte limit, verifies the image magic bytes and
   declared MIME type, writes a temporary file, atomically renames it into the
   source namespace's `sha256/` directory, and calls `upsertMediaAsset` with
   `status = ready` and the width and height its header declares. The same
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
8. Retention and garbage collection can later use `last_seen_at`,
   `media_links`, `media_versions` and the operator pins to remove
   unreferenced files deliberately.

The downloader accepts only HTTPS URLs on X's `*.twimg.com` media CDN
subdomains, YouTube's `*.ytimg.com` thumbnail CDN subdomains and Bilibili's
`*.hdslb.com` cover CDN subdomains (`i0`, `i1`, `i2` and so on). It revalidates
every redirect against the same suffix-bounded allowlist, requiring a real
subdomain boundary; lookalike hosts and the bare parent domains are rejected.
The host policy is kept in code because this is also an SSRF boundary; the
broader privacy allowlist is not a download permission list. The cache still
accepts image MIME types only, so X video URLs may be recognized as official
media while remaining unavailable to the image cache. Cached files, including
versions a revalidation replaced, are kept deliberately; there is no garbage
collection.

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
the candidate, including earlier incorrect active-metadata upgrades. This is
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

`GET /media/<id>?v=<hash>&w=<width>` serves a smaller copy of a ready asset.
`w` must be one of 320, 640, 960, 1280 or 1920; any other value, or a
repeated `w`, answers 404. The first request decodes the original (JPEG, PNG
or WebP no larger than 40 megapixels, at most two at a time; concurrent
requests for the same copy wait for the first and then read it from the
cache), scales it to
that width with Catmull-Rom filtering, encodes JPEG at quality 82 (or PNG when
the image has transparency) and stores it atomically in
`data/cache/variants/<first-two>/<hash>-<width>.<jpg|png>`. When the original
is no wider than the request, is a GIF (possibly animated) or AVIF, cannot be
decoded, or the copy would not be smaller, an empty `.orig` marker records
that the original is served for that width, so it is not decoded again.

The response is always an image: a copy, or the original whenever a copy
cannot be made or stored (a read-only data directory only means the copy is
made again next time). It uses the same headers, range and conditional
handling as the original, with the copy's own hash as its ETag, and is
immutable when `v` is given. Originals are never changed; the variant
directory is a derived cache that may be deleted at any time.

The board requests copies through `srcset` with the widths below an image's
known width (every width when it is unknown) plus the original; the full-image
viewer keeps loading the original. Profile media and bundled `/assets/` files
have no copies.

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
