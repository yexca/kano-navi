import type { Database as DatabaseConnection } from "better-sqlite3"

import { listMediaAssets, upsertMediaAsset } from "./database.ts"
import {
  extensionForMimeType,
  normalizeMediaMimeType,
  writeMediaFileAtomic,
} from "./media-cache.ts"

const defaultAllowedHosts = new Set([
  "pbs.twimg.com",
  "i.ytimg.com",
  "i1.ytimg.com",
  "i2.ytimg.com",
  "i3.ytimg.com",
  "i4.ytimg.com",
])
// X and YouTube publish media from additional CDN subdomains over time. Keep
// the suffixes explicit and require a real subdomain boundary, rather than
// accepting arbitrary lookalike hosts such as `twimg.com.attacker.invalid`.
const defaultAllowedHostSuffixes = new Set([".twimg.com", ".ytimg.com"])

export function isAllowedMediaHost(
  hostname,
  allowedHosts = defaultAllowedHosts,
  allowedHostSuffixes = defaultAllowedHostSuffixes,
) {
  const normalized = String(hostname || "")
    .toLowerCase()
    .replace(/\.$/u, "")
  if (allowedHosts.has(normalized)) return true
  return [...allowedHostSuffixes].some((suffix) => {
    const normalizedSuffix = String(suffix || "").toLowerCase()
    return (
      normalizedSuffix.startsWith(".") &&
      normalized.length > normalizedSuffix.length &&
      normalized.endsWith(normalizedSuffix)
    )
  })
}

function assertAllowedUrl(
  value,
  allowedHosts,
  allowedHostSuffixes = defaultAllowedHostSuffixes,
) {
  const url = new URL(value)
  if (
    url.protocol !== "https:" ||
    !isAllowedMediaHost(url.hostname, allowedHosts, allowedHostSuffixes)
  ) {
    throw new Error("media source host is not allowed")
  }
  return url
}

async function fetchWithRedirects(
  sourceUrl,
  {
    fetchImpl,
    signal,
    allowedHosts,
    maxRedirects = 3,
  }: { fetchImpl: any; signal: any; allowedHosts: any; maxRedirects?: number },
) {
  let url = assertAllowedUrl(sourceUrl, allowedHosts)
  for (let redirects = 0; redirects <= maxRedirects; redirects += 1) {
    const response = await fetchImpl(url, {
      redirect: "manual",
      signal,
      headers: {
        accept: "image/avif,image/webp,image/png,image/jpeg,image/gif;q=0.8",
        "user-agent": "kano-status-board/0.1 (+local media cache)",
      },
    })
    if (![301, 302, 303, 307, 308].includes(response.status)) return response
    const location = response.headers.get("location")
    if (!location || redirects === maxRedirects) {
      throw new Error("media redirect limit exceeded")
    }
    url = assertAllowedUrl(new URL(location, url).toString(), allowedHosts)
  }
  throw new Error("media redirect limit exceeded")
}

async function readBodyLimited(response, maxBytes) {
  const declaredSize = Number(response.headers.get("content-length"))
  if (Number.isFinite(declaredSize) && declaredSize > maxBytes) {
    throw new Error("media response exceeds the size limit")
  }
  if (!response.body) return Buffer.alloc(0)

  const reader = response.body.getReader()
  const chunks = []
  let size = 0
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    size += value.byteLength
    if (size > maxBytes) {
      await reader.cancel()
      throw new Error("media response exceeds the size limit")
    }
    chunks.push(Buffer.from(value))
  }
  return Buffer.concat(chunks, size)
}

export function sniffImageMimeType(body) {
  if (
    body.length >= 3 &&
    body[0] === 0xff &&
    body[1] === 0xd8 &&
    body[2] === 0xff
  ) {
    return "image/jpeg"
  }
  if (
    body.length >= 8 &&
    body
      .subarray(0, 8)
      .equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))
  ) {
    return "image/png"
  }
  if (
    body.length >= 6 &&
    /^GIF8[79]a$/u.test(body.subarray(0, 6).toString("ascii"))
  ) {
    return "image/gif"
  }
  if (
    body.length >= 12 &&
    body.subarray(0, 4).toString("ascii") === "RIFF" &&
    body.subarray(8, 12).toString("ascii") === "WEBP"
  ) {
    return "image/webp"
  }
  if (
    body.length >= 12 &&
    body.subarray(4, 8).toString("ascii") === "ftyp" &&
    ["avif", "avis"].includes(body.subarray(8, 12).toString("ascii"))
  ) {
    return "image/avif"
  }
  return null
}

export async function downloadMediaAsset(
  database: DatabaseConnection,
  asset,
  {
    fetchImpl = fetch,
    timeoutMs = 10_000,
    maxBytes = 10 * 1024 * 1024,
    allowedHosts = defaultAllowedHosts,
  }: {
    fetchImpl?: any
    timeoutMs?: number
    maxBytes?: any
    allowedHosts?: any
  } = {},
) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetchWithRedirects(asset.sourceUrl, {
      fetchImpl,
      signal: controller.signal,
      allowedHosts,
    })
    if (!response.ok) {
      const status =
        response.status === 404 || response.status === 410
          ? "missing"
          : "failed"
      upsertMediaAsset(database, {
        source: asset.source,
        sourceUrl: asset.sourceUrl,
        status,
        lastError: `HTTP ${response.status}`,
      })
      return { status, id: asset.id }
    }

    const body = await readBodyLimited(response, maxBytes)
    const sniffedMimeType = sniffImageMimeType(body)
    const declaredMimeType = normalizeMediaMimeType(
      response.headers.get("content-type"),
    )
    if (
      !sniffedMimeType ||
      (declaredMimeType && declaredMimeType !== sniffedMimeType)
    ) {
      throw new Error("media response is not a supported image")
    }
    const written = await writeMediaFileAtomic({
      content: body,
      extension: extensionForMimeType(sniffedMimeType),
      source: asset.source,
    })
    const ready = upsertMediaAsset(database, {
      source: asset.source,
      sourceUrl: asset.sourceUrl,
      status: "ready",
      cachePath: written.relativePath,
      mimeType: sniffedMimeType,
      sha256: written.sha256,
      byteSize: written.byteSize,
      etag: response.headers.get("etag"),
      lastModified: response.headers.get("last-modified"),
    })
    return { status: "ready", id: ready.id, byteSize: ready.byteSize }
  } catch (error) {
    upsertMediaAsset(database, {
      source: asset.source,
      sourceUrl: asset.sourceUrl,
      status: "failed",
      lastError:
        error.name === "AbortError" ? "media request timed out" : error.message,
    })
    return { status: "failed", id: asset.id, error: error.message }
  } finally {
    clearTimeout(timer)
  }
}

export async function downloadPendingMedia(
  database: DatabaseConnection,
  {
    limit = 20,
    sources = null,
    ...options
  }: { limit?: number; sources?: any; [key: string]: any } = {},
) {
  const sourceSet = sources ? new Set(sources.map(String)) : null
  const pending = listMediaAssets(database, { status: "pending" })
    .filter((asset) => !sourceSet || sourceSet.has(asset.source))
    .slice(0, Math.min(100, Math.max(0, Number(limit) || 0)))
  const summary = { attempted: pending.length, ready: 0, missing: 0, failed: 0 }
  for (const asset of pending) {
    const result = await downloadMediaAsset(database, asset, options)
    summary[result.status] = (summary[result.status] || 0) + 1
  }
  return summary
}

export { defaultAllowedHosts }
