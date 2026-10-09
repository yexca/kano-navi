/** Match supported YouTube links using only their public video identity. */
export function streamIdentity(value: string) {
  try {
    const url = new URL(value)
    if (["http:", "https:"].includes(url.protocol)) {
      const id =
        url.hostname === "youtu.be"
          ? url.pathname.match(/^\/([^/]+)\/?$/u)?.[1]
          : /(?:^|\.)youtube\.com$/u.test(url.hostname)
            ? url.searchParams.get("v") ||
              url.pathname.match(/^\/(?:live|shorts)\/([^/]+)\/?$/u)?.[1]
            : null
      if (id && /^[A-Za-z0-9_-]+$/u.test(id)) return `youtube:${id}`
    }
  } catch {
    // Non-YouTube or unparseable links keep exact URL matching.
  }
  return `url:${value}`
}
