import crypto from "node:crypto"
import fs from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"

import { historyMedia } from "../src/history/media.ts"

export interface HistoryCatalogItem {
  id: string
  filename: string
  sourceUrl: string
  mediaId: string
  sha256: string
  byteSize: number
  mimeType: string
}

// Untrusted manifest entries: fields are validated before use.
interface ArchiveManifestRecord {
  filename?: unknown
  file?: unknown
}

const MAX_IMAGE_BYTES = 10 * 1024 * 1024

const projectRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
)

export function sha256ForContent(content: Buffer) {
  return crypto.createHash("sha256").update(content).digest("hex")
}

/** The stable opaque identity the backend derives for a remote source URL. */
export function mediaIdForSourceUrl(sourceUrl: string) {
  try {
    const url = new URL(sourceUrl.trim())
    if (url.protocol !== "http:" && url.protocol !== "https:") return null
    url.hash = ""
    return crypto.createHash("sha256").update(url.toString()).digest("hex")
  } catch {
    return null
  }
}

function sniffImageMimeType(body: Buffer) {
  const ascii = (start: number, end: number) =>
    body.subarray(start, end).toString("ascii")
  if (
    body.length >= 3 &&
    body[0] === 0xff &&
    body[1] === 0xd8 &&
    body[2] === 0xff
  )
    return "image/jpeg"
  if (
    body.length >= 8 &&
    body
      .subarray(0, 8)
      .equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))
  )
    return "image/png"
  if (body.length >= 6 && /^GIF8[79]a$/u.test(ascii(0, 6))) return "image/gif"
  if (body.length >= 12 && ascii(0, 4) === "RIFF" && ascii(8, 12) === "WEBP")
    return "image/webp"
  if (
    body.length >= 12 &&
    ascii(4, 8) === "ftyp" &&
    ["avif", "avis"].includes(ascii(8, 12))
  )
    return "image/avif"
  return null
}

// Validate the entire selection before writing anything, so an incomplete or
// altered archive cannot partially replace a previously working collection.
export async function prepareHistoryMedia(
  directory: string,
  catalog: HistoryCatalogItem[] = historyMedia,
) {
  const root = await fs.realpath(directory)
  const manifest = JSON.parse(
    (await fs.readFile(path.join(root, "manifest.json"), "utf8")).replace(
      /^﻿/u,
      "",
    ),
  )
  if (!Array.isArray(manifest.images))
    throw new Error("invalid history archive manifest")
  const records = new Map<unknown, ArchiveManifestRecord>(
    manifest.images.map((item: ArchiveManifestRecord) => [item.filename, item]),
  )
  const prepared: { item: HistoryCatalogItem; content: Buffer }[] = []

  for (const item of catalog) {
    const record = records.get(item.filename)
    if (!record || typeof record.file !== "string")
      throw new Error(`missing archive image: ${item.id}`)
    const relative = record.file.replaceAll("\\", "/")
    if (
      path.isAbsolute(relative) ||
      path.win32.isAbsolute(relative) ||
      relative.split("/").includes("..")
    ) {
      throw new Error(`unsafe archive image path: ${item.id}`)
    }
    const fullPath = await fs.realpath(path.resolve(root, relative))
    const resolvedRelative = path.relative(root, fullPath)
    if (
      !resolvedRelative ||
      resolvedRelative.startsWith(`..${path.sep}`) ||
      resolvedRelative === ".." ||
      path.isAbsolute(resolvedRelative)
    ) {
      throw new Error(`archive image escapes its directory: ${item.id}`)
    }
    const stat = await fs.stat(fullPath)
    if (
      !stat.isFile() ||
      stat.size > MAX_IMAGE_BYTES ||
      stat.size !== item.byteSize
    ) {
      throw new Error(`archive image size mismatch: ${item.id}`)
    }
    const content = await fs.readFile(fullPath)
    if (
      sha256ForContent(content) !== item.sha256 ||
      sniffImageMimeType(content) !== item.mimeType ||
      mediaIdForSourceUrl(item.sourceUrl) !== item.mediaId
    ) {
      throw new Error(`archive image identity mismatch: ${item.id}`)
    }
    prepared.push({ item, content })
  }

  return prepared
}

export async function packageHistoryMedia(
  directory: string,
  destination = path.join(projectRoot, "public/assets/history"),
  catalog?: HistoryCatalogItem[],
) {
  const prepared = await prepareHistoryMedia(directory, catalog)
  for (const { item } of prepared) {
    if (!/^[a-zA-Z0-9_-]+\.(png|jpg|jpeg|webp|gif)$/u.test(item.filename))
      throw new Error(`unsafe packaged image filename: ${item.id}`)
  }
  await fs.mkdir(destination, { recursive: true })
  for (const { item, content } of prepared) {
    await fs.writeFile(path.join(destination, item.filename), content)
  }
  return {
    packaged: prepared.length,
    bytes: prepared.reduce((sum, { content }) => sum + content.length, 0),
  }
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const args = process.argv.slice(2)
  if (args.length !== 2 || args[0] !== "--from") {
    console.error(
      "Usage: npm run history:package -- --from <archive-directory>",
    )
    process.exitCode = 1
  } else {
    try {
      const result = await packageHistoryMedia(args[1])
      console.log(
        `Packaged ${result.packaged} history images (${(result.bytes / 1024 / 1024).toFixed(2)} MiB).`,
      )
    } catch (error) {
      console.error(
        error instanceof Error && "code" in error && error.code
          ? "Unable to read the local history archive."
          : error instanceof Error
            ? error.message
            : String(error),
      )
      process.exitCode = 1
    }
  }
}
