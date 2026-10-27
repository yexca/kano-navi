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
