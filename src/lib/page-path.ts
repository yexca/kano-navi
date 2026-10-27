// The page a location shows. Keep in step with spaPages in
// backend-go/internal/httpapi/static.go, which answers the same paths 200.

/**
 * Trailing slashes are dropped, and /index.html, the file the server answers
 * for the board, is the board.
 */
export function pagePath(pathname: string): string {
  const path = pathname.replace(/\/+$/u, "") || "/"
  return path === "/index.html" ? "/" : path
}

/**
 * The address a same-tab link can show without a full page load: one of
 * `pages` on this origin. A fragment within the current page stays with the
 * browser, which already scrolls to it.
 */
export function inPlaceUrl(
  href: string,
  current: string,
  pages: readonly string[],
): URL | null {
  let url: URL
  try {
    url = new URL(href, current)
  } catch {
    return null
  }
  const here = new URL(current)
  if (url.origin !== here.origin || !pages.includes(pagePath(url.pathname))) {
    return null
  }
  const samePage = url.pathname === here.pathname && url.search === here.search
  return samePage && url.hash ? null : url
}
