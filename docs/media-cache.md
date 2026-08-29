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
  avatar/
    avatar.<extension>
    banner.<extension>
  .tmp/
    <temporary-file>.part
```

The whole `data/` directory is ignored by Git. It is created on demand by the
server or synchronization command. The tracked `public/assets/` directory is
reserved for fixed fallback branding. Discovered X and YouTube profile
candidates stay in their source namespaces; local uploads and the selected
avatar and banner files remain runtime data in `data/avatar/`.

## Database Model

`media_assets` stores one row per normalized HTTP(S) source URL:

- `id`: a 64-character SHA-256 identity derived from the source URL;
- `source`, `source_url`: the platform adapter and original public URL;
- `status`: `pending`, `ready`, `missing`, or `failed`;
- `cache_path`: a path relative to the runtime `data/` directory, normally
  `x/`, `youtube/`, or the legacy `cache/media/` namespace; never an absolute
  path;
- `mime_type`, `extension`, `byte_size`, `sha256`, `width`, and `height`;
- fetch/check/seen timestamps, validators (`etag`, `last_modified`), and the
  last error for maintainer diagnosis.

`media_links` associates an asset with a domain record without coupling the
media table to every content table. Its owner tuple is `(owner_type, owner_id,
role, position)`, for example `post / 209... / post-image / 0` or `video / abc /
thumbnail / 0`.

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
3. `downloadPendingMedia` claims a bounded batch during `npm run sync`. It
   downloads with a timeout and byte limit, verifies the image magic bytes and
   declared MIME type, writes a temporary file, atomically renames it into the
   source namespace's `sha256/` directory, and calls `upsertMediaAsset` with
   `status = ready`.
4. A failed or unavailable request updates the row to `failed` or `missing`.
   Existing ready files are retained; a failed synchronization never clears a
   known snapshot.
5. Retention and garbage collection can later use `last_seen_at` and
   `media_links` to remove unreferenced files deliberately.

The downloader accepts only HTTPS URLs on `pbs.twimg.com`, `i.ytimg.com`, and
YouTube's numbered thumbnail hosts from `i1.ytimg.com` through
`i4.ytimg.com`. It revalidates every redirect against the same exact allowlist.
The host policy is kept in code because this is also an SSRF boundary; the
broader privacy allowlist is not a download permission list. Retry scheduling,
image dimension probing, and garbage collection remain follow-up work.

Content-addressed paths are deliberately preferred over layouts such as
`pics/x/<timestamp>` or `pics/youtube/<video-id>`. Ownership already lives in
`media_links`; storing bytes by hash deduplicates identical files, makes path
validation deterministic, and allows atomic replacement without trusting a
platform identifier as a path component.

## HTTP Contract

The server exposes `GET /media/<id>` for ready assets only. The ID must match
the opaque 64-character format, the database row must be `ready`, and the
resolved file must remain under the cache root. The response never accepts a
filesystem path or a source URL from the request. Missing, unsafe, or unready
assets return `404`.

Dashboard media fields include the safe `...Url` (a
`/media/<id>?v=<content-sha256>` route or a tracked `/assets/...` fallback), the
`...Status`, the opaque ID, and the public source URL for traceability. The
version parameter changes whenever bytes change, so versioned responses can be
cached as immutable even though the source identity is stable. A bare media ID
is revalidated, and an unready remote URL is represented by `null`, so the
browser does not silently fall back to a CDN.

## Verification

Run `npm run test:server` for path, identity, registration, downloader-host,
MIME, and dashboard contract tests. `make ci` runs those tests together with
the sensitive scan, documentation checks, and the frontend build. Tests use
synthetic response bodies; CI does not download live media.
