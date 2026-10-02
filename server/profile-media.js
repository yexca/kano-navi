import fs from "node:fs"

import {
  getProfileMedia,
  listProfileMedia,
  markProfileMediaFailed,
  upsertProfileMediaCandidate,
  updateProfileMediaReady,
} from "./database.js"
import {
  extensionForMimeType,
  normalizeMediaMimeType,
  normalizeSourceUrl,
  writeMediaFileAtomic,
} from "./media-cache.js"
import { sniffImageMimeType } from "./media-downloader.js"

const profilePageHosts = new Set([
  "x.com",
  "www.x.com",
  "twitter.com",
  "www.twitter.com",
  "youtube.com",
  "www.youtube.com",
])
const imageHosts = new Set([
  "pbs.twimg.com",
  "abs.twimg.com",
  "i.ytimg.com",
  "i1.ytimg.com",
  "i2.ytimg.com",
  "i3.ytimg.com",
  "i4.ytimg.com",
  "yt3.ggpht.com",
  "yt3.googleusercontent.com",
  "lh3.googleusercontent.com",
])
const imageHostSuffixes = new Set([".twimg.com", ".ytimg.com"])

const maxProfileImageBytes = 15 * 1024 * 1024

function validSlot(value) {
  const slot = String(value || "")
    .trim()
    .toLowerCase()
  return slot === "avatar" || slot === "banner" ? slot : null
}

function validSource(value) {
  const source = String(value || "")
    .trim()
    .toLowerCase()
  return source === "x" || source === "youtube" || source === "upload"
    ? source
    : null
}

function hostAllowed(hostname, hosts, suffixes = new Set()) {
  const normalized = String(hostname || "").toLowerCase()
  return (
    hosts.has(normalized) ||
    [...suffixes].some(
      (suffix) =>
        normalized.length > suffix.length && normalized.endsWith(suffix),
    )
  )
}

function assertPageUrl(value, source) {
  const normalized = normalizeSourceUrl(value)
  if (!normalized) throw new Error("profile source URL is invalid")
  const url = new URL(normalized)
  if (!hostAllowed(url.hostname, profilePageHosts))
    throw new Error(`${source} profile page host is not allowed`)
  return normalized
}

function assertImageUrl(value) {
  const normalized = normalizeSourceUrl(value)
  if (!normalized) throw new Error("profile image URL is invalid")
  const url = new URL(normalized)
  if (
    url.protocol !== "https:" ||
    !hostAllowed(url.hostname, imageHosts, imageHostSuffixes)
  )
    throw new Error("profile image host is not allowed")
  return normalized
}

function decodeEscapedUrl(value) {
  return String(value || "")
    .replaceAll("\\/", "/")
    .replaceAll("\\u0026", "&")
    .replaceAll("&amp;", "&")
}

function metaImageUrls(html) {
  const urls = []
  const patterns = [
    /<meta[^>]+(?:property|name)=["'](?:og:image|twitter:image)["'][^>]+content=["']([^"']+)["']/giu,
    /<meta[^>]+content=["']([^"']+)["'][^>]+(?:property|name)=["'](?:og:image|twitter:image)["']/giu,
  ]
  for (const pattern of patterns) {
    for (const match of String(html || "").matchAll(pattern))
      urls.push(match[1])
  }
  return urls.map(decodeEscapedUrl)
}

function keyedImageUrls(html, slot) {
  const keys =
    slot === "banner"
      ? "profile_banner_url|profileBannerUrl|banner_url|bannerUrl|channelBanner"
      : "profile_image_url|profileImageUrl|avatar_url|avatarUrl|channelAvatar"
  const pattern = new RegExp(
    `(?:${keys})[^\\n]{0,120}?[=:]\\s*["'](https?:\\\\?/\\\\?/[^"']+)["']`,
    "giu",
  )
  return [...String(html || "").matchAll(pattern)].map((match) =>
    decodeEscapedUrl(match[1]),
  )
}

function readBodyLimited(response, maxBytes) {
  const declaredSize = Number(response.headers.get("content-length"))
  if (Number.isFinite(declaredSize) && declaredSize > maxBytes)
    throw new Error("profile image exceeds the size limit")
  if (!response.body)
    return response.arrayBuffer().then((body) => {
      if (body.byteLength > maxBytes)
        throw new Error("profile image exceeds the size limit")
      return Buffer.from(body)
    })
  return (async () => {
    const reader = response.body.getReader()
    const chunks = []
    let size = 0
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      size += value.byteLength
      if (size > maxBytes) {
        await reader.cancel()
        throw new Error("profile image exceeds the size limit")
      }
      chunks.push(Buffer.from(value))
    }
    return Buffer.concat(chunks, size)
  })()
}

async function fetchWithTimeout(
  url,
  {
    fetchImpl = fetch,
    timeoutMs = 10_000,
    headers = {},
    redirect = "follow",
  } = {},
) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    return await fetchImpl(url, {
      signal: controller.signal,
      redirect,
      headers: {
        "user-agent": "kano-status-board/0.1 (+local profile media)",
        ...headers,
      },
    })
  } catch (error) {
    if (controller.signal.aborted)
      throw new Error("profile media request timed out")
    throw error
  } finally {
    clearTimeout(timer)
  }
}

function dimensionsForImage(body, mimeType) {
  if (mimeType === "image/png" && body.length >= 24) {
    return { width: body.readUInt32BE(16), height: body.readUInt32BE(20) }
  }
  if (mimeType === "image/gif" && body.length >= 10) {
    return { width: body.readUInt16LE(6), height: body.readUInt16LE(8) }
  }
  if (mimeType === "image/jpeg") {
    let offset = 2
    while (offset + 9 < body.length) {
      if (body[offset] !== 0xff) {
        offset += 1
        continue
      }
      const marker = body[offset + 1]
      const length = body.readUInt16BE(offset + 2)
      if (length < 2 || offset + length + 2 > body.length) break
      if (marker >= 0xc0 && marker <= 0xc3) {
        return {
          width: body.readUInt16BE(offset + 7),
          height: body.readUInt16BE(offset + 5),
        }
      }
      offset += length + 2
    }
  }
  return { width: null, height: null }
}

function profilePageUrl(database, source) {
  const profile = database
    .prepare(
      "SELECT x_url AS xUrl, youtube_url AS youtubeUrl FROM profiles ORDER BY id LIMIT 1",
    )
    .get()
  if (source === "x") return profile?.xUrl || "https://x.com/kano_2525"
  return (
    profile?.youtubeUrl ||
    "https://www.youtube.com/channel/UCShXNLMXCfstmWKH_q86B8w"
  )
}

/** Discover public profile image URLs. This is only called by an admin action. */
export async function discoverProfileMedia(
  database,
  { slot, source, fetchImpl = fetch, timeoutMs = 10_000 } = {},
) {
  const normalizedSlot = validSlot(slot)
  const normalizedSource = validSource(source)
  if (!normalizedSlot) throw new Error("invalid profile media slot")
  if (!normalizedSource || normalizedSource === "upload")
    throw new Error("profile discovery source must be x or youtube")
  const pageUrl = assertPageUrl(
    profilePageUrl(database, normalizedSource),
    normalizedSource,
  )
  const fetchedPage = await fetchProfilePage(pageUrl, normalizedSource, {
    fetchImpl,
    timeoutMs,
  })
  const response = fetchedPage.response
  const resolvedPageUrl = fetchedPage.url
  if (!response.ok)
    throw new Error(`profile page returned HTTP ${response.status}`)
  const html = await response.text()
  const existing = database
    .prepare(
      `SELECT ${normalizedSlot === "avatar" ? "avatar_url" : "banner_url"} AS sourceUrl
       FROM profiles ORDER BY id LIMIT 1`,
    )
    .get()?.sourceUrl
  const discovered = [
    ...keyedImageUrls(html, normalizedSlot),
    ...metaImageUrls(html),
    existing,
  ]
  const candidates = []
  for (const value of discovered) {
    try {
      const imageUrl = assertImageUrl(value)
      if (candidates.includes(imageUrl)) continue
      candidates.push(imageUrl)
      upsertProfileMediaCandidate(database, {
        slot: normalizedSlot,
        source: normalizedSource,
        sourceRef: resolvedPageUrl,
        sourceUrl: imageUrl,
      })
    } catch {
      // A page can contain unrelated social images; ignore those safely.
    }
  }
  return {
    slot: normalizedSlot,
    source: normalizedSource,
    sourceUrl: resolvedPageUrl,
    candidates: listProfileMedia(database, { slot: normalizedSlot }),
  }
}

async function fetchImage(
  sourceUrl,
  { fetchImpl = fetch, timeoutMs = 10_000, maxRedirects = 3 } = {},
) {
  let current = assertImageUrl(sourceUrl)
  for (let redirect = 0; redirect <= maxRedirects; redirect += 1) {
    const response = await fetchWithTimeout(current, {
      fetchImpl,
      timeoutMs,
      redirect: "manual",
    })
    if (![301, 302, 303, 307, 308].includes(response.status)) return response
    const location = response.headers.get("location")
    if (!location || redirect === maxRedirects)
      throw new Error("profile image redirect limit exceeded")
    current = assertImageUrl(new URL(location, current).toString())
  }
  throw new Error("profile image redirect limit exceeded")
}

async function fetchProfilePage(
  sourceUrl,
  source,
  { fetchImpl = fetch, timeoutMs = 10_000, maxRedirects = 3 } = {},
) {
  let current = assertPageUrl(sourceUrl, source)
  for (let redirect = 0; redirect <= maxRedirects; redirect += 1) {
    const response = await fetchWithTimeout(current, {
      fetchImpl,
      timeoutMs,
      redirect: "manual",
      headers: { accept: "text/html,application/xhtml+xml" },
    })
    if (![301, 302, 303, 307, 308].includes(response.status)) {
      return { response, url: current }
    }
    const location = response.headers.get("location")
    if (!location || redirect === maxRedirects)
      throw new Error("profile page redirect limit exceeded")
    current = assertPageUrl(new URL(location, current).toString(), source)
  }
  throw new Error("profile page redirect limit exceeded")
}

/** Download one discovered candidate after an explicit admin request. */
export async function downloadProfileMedia(
  database,
  id,
  {
    fetchImpl = fetch,
    timeoutMs = 10_000,
    maxBytes = maxProfileImageBytes,
  } = {},
) {
  const candidate = getProfileMedia(database, id)
  if (!candidate) throw new Error("profile media candidate not found")
  if (!candidate.sourceUrl)
    throw new Error("profile media candidate has no source URL")
  try {
    const response = await fetchImage(candidate.sourceUrl, {
      fetchImpl,
      timeoutMs,
    })
    if (!response.ok)
      throw new Error(`profile image returned HTTP ${response.status}`)
    const body = await readBodyLimited(response, maxBytes)
    const sniffed = sniffImageMimeType(body)
    const declared = normalizeMediaMimeType(
      response.headers.get("content-type"),
    )
    if (!sniffed || (declared && declared !== sniffed))
      throw new Error("profile response is not a supported image")
    const written = await writeMediaFileAtomic({
      content: body,
      extension: extensionForMimeType(sniffed),
      source: candidate.source,
    })
    const dimensions = dimensionsForImage(body, sniffed)
    return updateProfileMediaReady(database, candidate.id, {
      cachePath: written.relativePath,
      mimeType: sniffed,
      sha256: written.sha256,
      byteSize: written.byteSize,
      ...dimensions,
    })
  } catch (error) {
    markProfileMediaFailed(database, candidate.id, error.message)
    throw error
  }
}

/** Store a locally uploaded original as a ready, but not yet active, candidate. */
export async function saveUploadedProfileMedia(
  database,
  slot,
  body,
  {
    mimeType = "",
    sourceRef = "admin-upload",
    maxBytes = maxProfileImageBytes,
  } = {},
) {
  const normalizedSlot = validSlot(slot)
  if (!normalizedSlot) throw new Error("invalid profile media slot")
  const content = Buffer.isBuffer(body) ? body : Buffer.from(body || "")
  if (!content.length || content.length > maxBytes)
    throw new Error("profile upload exceeds the size limit")
  const sniffed = sniffImageMimeType(content)
  const declared = normalizeMediaMimeType(mimeType)
  if (!sniffed || (declared && declared !== sniffed))
    throw new Error("uploaded file is not a supported image")
  const candidate = upsertProfileMediaCandidate(database, {
    slot: normalizedSlot,
    source: "upload",
    sourceRef,
  })
  const written = await writeMediaFileAtomic({
    content,
    extension: extensionForMimeType(sniffed),
    source: "upload",
  })
  const dimensions = dimensionsForImage(content, sniffed)
  return updateProfileMediaReady(database, candidate.id, {
    cachePath: written.relativePath,
    mimeType: sniffed,
    sha256: written.sha256,
    byteSize: written.byteSize,
    ...dimensions,
  })
}

export function profileImageHosts() {
  return new Set(imageHosts)
}

export function isProfileSlot(value) {
  return validSlot(value) !== null
}

export function isProfileSource(value) {
  return validSource(value) !== null
}

export { maxProfileImageBytes }
