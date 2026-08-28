import crypto from "node:crypto"
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const moduleDirectory = path.dirname(fileURLToPath(import.meta.url))
const projectDirectory = path.resolve(moduleDirectory, "..")

/**
 * Runtime media files deliberately live outside `public/` and the Vite output.
 * The database stores paths relative to this directory, never absolute paths.
 */
export const cacheDirectory = path.join(projectDirectory, "data", "cache")
export const mediaCacheDirectory = path.join(cacheDirectory, "media")
export const mediaCacheContentDirectory = path.join(mediaCacheDirectory, "sha256")
export const mediaCacheTempDirectory = path.join(mediaCacheDirectory, "tmp")
export const publicAssetsDirectory = path.join(projectDirectory, "public", "assets")

export const MEDIA_STATUS = Object.freeze({
  PENDING: "pending",
  READY: "ready",
  MISSING: "missing",
  FAILED: "failed",
})

export const mediaIdPattern = /^[a-f0-9]{64}$/u
const sha256Pattern = /^[a-f0-9]{64}$/u
const extensionPattern = /^[a-z0-9]{1,12}$/u

const mimeExtensions = new Map([
  ["image/avif", "avif"],
  ["image/gif", "gif"],
  ["image/jpeg", "jpg"],
  ["image/png", "png"],
  ["image/webp", "webp"],
])

/** Create the runtime directories used by a future downloader. */
export function ensureMediaCacheDirectories() {
  for (const directory of [cacheDirectory, mediaCacheDirectory, mediaCacheContentDirectory, mediaCacheTempDirectory]) {
    fs.mkdirSync(directory, { recursive: true })
  }
}

/**
 * Normalize and validate a source URL. Only public HTTP(S) URLs are cacheable;
 * local `/assets/...` paths are tracked static files and intentionally skipped.
 */
export function normalizeSourceUrl(value) {
  if (typeof value !== "string" || value.trim() === "") return null
  try {
    const url = new URL(value.trim())
    if (url.protocol !== "http:" && url.protocol !== "https:") return null
    url.hash = ""
    return url.toString()
  } catch {
    return null
  }
}

/** Return a stable opaque identity for a remote source URL. */
export function mediaIdForSourceUrl(sourceUrl) {
  const normalized = normalizeSourceUrl(sourceUrl)
  if (!normalized) return null
  return crypto.createHash("sha256").update(normalized).digest("hex")
}

export function isSafeMediaId(value) {
  return typeof value === "string" && mediaIdPattern.test(value)
}

export function isSafeContentHash(value) {
  return typeof value === "string" && sha256Pattern.test(value)
}

export function sha256ForContent(content) {
  return crypto.createHash("sha256").update(content).digest("hex")
}

export function sanitizeExtension(value, fallback = "bin") {
  const normalized = String(value || "").trim().toLowerCase().replace(/^\./u, "")
  return extensionPattern.test(normalized) ? normalized : fallback
}

export function normalizeMediaMimeType(mimeType) {
  const normalized = String(mimeType || "").split(";", 1)[0].trim().toLowerCase()
  return mimeExtensions.has(normalized) ? normalized : null
}

export function isAllowedMediaMimeType(mimeType) {
  return normalizeMediaMimeType(mimeType) !== null
}

export function extensionForMimeType(mimeType, fallback = "bin") {
  const normalized = normalizeMediaMimeType(mimeType)
  return normalized ? mimeExtensions.get(normalized) : fallback
}

export function extensionForSourceUrl(sourceUrl, fallback = "bin") {
  const normalized = normalizeSourceUrl(sourceUrl)
  if (!normalized) return fallback
  try {
    const extension = path.posix.extname(new URL(normalized).pathname)
    return sanitizeExtension(extension, fallback)
  } catch {
    return fallback
  }
}

/** Build a relative content-addressed path after validating its components. */
export function cacheRelativePathForHash(contentHash, extension = "bin") {
  const hash = String(contentHash || "").toLowerCase()
  if (!sha256Pattern.test(hash)) throw new Error("invalid media content hash")
  const safeExtension = sanitizeExtension(extension)
  return path.posix.join("sha256", hash.slice(0, 2), `${hash}.${safeExtension}`)
}

/**
 * Resolve a database path under the cache root. Invalid, absolute, and
 * traversal-containing paths return null so callers can answer with 404.
 */
export function resolveMediaCachePath(relativePath) {
  if (typeof relativePath !== "string" || relativePath.trim() === "") return null
  const normalized = relativePath.replaceAll("\\", "/")
  if (normalized.includes("\0") || path.posix.isAbsolute(normalized)) return null
  if (normalized.split("/").some((part) => part === "..")) return null

  const root = path.resolve(mediaCacheDirectory)
  const candidate = path.resolve(root, normalized)
  if (candidate !== root && !candidate.startsWith(`${root}${path.sep}`)) return null
  return candidate
}

export function publicMediaUrl(mediaId, contentHash = null) {
  if (!isSafeMediaId(mediaId)) return null
  if (contentHash == null) return `/media/${mediaId}`
  return isSafeContentHash(contentHash) ? `/media/${mediaId}?v=${contentHash}` : null
}

/** Resolve a tracked `/assets/...` URL without permitting traversal. */
export function resolvePublicAssetPath(assetUrl) {
  if (typeof assetUrl !== "string" || !assetUrl.startsWith("/assets/")) return null
  let relativePath
  try {
    relativePath = decodeURIComponent(assetUrl.slice("/assets/".length))
  } catch {
    return null
  }
  if (!relativePath || relativePath.includes("\0")) return null
  const root = path.resolve(publicAssetsDirectory)
  const candidate = path.resolve(root, relativePath)
  if (candidate !== root && !candidate.startsWith(`${root}${path.sep}`)) return null
  return candidate
}

export function publicAssetExists(assetUrl) {
  const filePath = resolvePublicAssetPath(assetUrl)
  if (!filePath) return false
  try {
    return fs.statSync(filePath).isFile()
  } catch {
    return false
  }
}

/**
 * Atomically write a downloaded media body. The caller may pass a known hash;
 * it is rechecked before the file is moved into the content-addressed store.
 */
export async function writeMediaFileAtomic({ content, contentHash, extension = "bin" }) {
  const body = Buffer.isBuffer(content) ? content : Buffer.from(content)
  const actualHash = sha256ForContent(body)
  const expectedHash = contentHash ? String(contentHash).toLowerCase() : actualHash
  if (!sha256Pattern.test(expectedHash) || expectedHash !== actualHash) {
    throw new Error("media content hash mismatch")
  }

  ensureMediaCacheDirectories()
  const relativePath = cacheRelativePathForHash(actualHash, extension)
  const destination = resolveMediaCachePath(relativePath)
  const temporaryName = `${actualHash}.${process.pid}.${crypto.randomUUID()}.part`
  const temporaryPath = resolveMediaCachePath(path.posix.join("tmp", temporaryName))
  if (!destination || !temporaryPath) throw new Error("unable to resolve media cache path")

  try {
    await fs.promises.writeFile(temporaryPath, body, { flag: "wx" })
    await fs.promises.mkdir(path.dirname(destination), { recursive: true })
    await fs.promises.rename(temporaryPath, destination)
  } catch (error) {
    try { await fs.promises.rm(temporaryPath, { force: true }) } catch { /* best effort cleanup */ }
    throw error
  }

  return { relativePath, sha256: actualHash, byteSize: body.byteLength }
}
