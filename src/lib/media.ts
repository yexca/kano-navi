// Smaller copies of cached images. The server makes them on first request at
// a fixed list of widths (backend-go/internal/mediavariant) and keeps the
// original untouched; full-image viewers keep loading the original URL.

/** The only widths the server accepts in a media URL's `w` parameter. */
export const MEDIA_VARIANT_WIDTHS = [320, 640, 960, 1280, 1920] as const

/** Matches a cached asset URL (`/media/<id>`), not profile media or assets. */
const CACHED_MEDIA = /^\/media\/[a-f0-9]{64}(?:\?|$)/

/** The URL of a smaller copy, or the URL itself when it has none. */
export function mediaVariantUrl(url: string, width: number): string {
  if (!CACHED_MEDIA.test(url)) return url
  return `${url}${url.includes("?") ? "&" : "?"}w=${width}`
}

/**
 * A srcset for a cached image. With a known original width it lists the
 * smaller copies and then the original; without one it lists every copy, and
 * the server answers a width the image cannot shrink to with the original.
 * Other URLs (bundled assets, profile media, missing images) get none.
 */
export function mediaSrcSet(
  url: string | null | undefined,
  originalWidth?: number | null,
): string | undefined {
  if (!url || !CACHED_MEDIA.test(url)) return undefined
  const known =
    typeof originalWidth === "number" && originalWidth > 0
      ? originalWidth
      : null
  const widths = MEDIA_VARIANT_WIDTHS.filter(
    (width) => known === null || width < known,
  )
  const candidates = widths.map(
    (width) => `${mediaVariantUrl(url, width)} ${width}w`,
  )
  if (known !== null) candidates.push(`${url} ${known}w`)
  return candidates.length ? candidates.join(", ") : undefined
}
