import fs from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"

import {
  initializeDatabase,
  registerMediaCandidates,
  upsertMediaAsset,
} from "../server/database.js"
import {
  extensionForMimeType,
  mediaIdForSourceUrl,
  sha256ForContent,
  writeMediaFileAtomic,
} from "../server/media-cache.js"
import { sniffImageMimeType } from "../server/media-downloader.js"
import { historyMedia } from "../src/history/media.js"

const MAX_IMAGE_BYTES = 10 * 1024 * 1024

function sourceAdapter(sourceUrl) {
  const host = new URL(sourceUrl).hostname
  if (host.endsWith(".twimg.com")) return "x"
  if (host.endsWith(".ytimg.com") || host === "www.youtube.com")
    return "youtube"
  return null
}

// Validate the entire selection before writing anything, so an incomplete or
// altered archive cannot partially replace a previously working snapshot.
export async function prepareHistoryMedia(directory, catalog = historyMedia) {
  const root = await fs.realpath(directory)
  const manifest = JSON.parse(
    (await fs.readFile(path.join(root, "manifest.json"), "utf8")).replace(
      /^\uFEFF/u,
      "",
    ),
  )
  if (!Array.isArray(manifest.images))
    throw new Error("invalid history archive manifest")
  const records = new Map(manifest.images.map((item) => [item.filename, item]))
  const prepared = []

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
    prepared.push({ item, content, adapter: sourceAdapter(item.sourceUrl) })
  }

  return prepared
}

export async function importHistoryMedia(
  database,
  directory,
  catalog = historyMedia,
) {
  const prepared = await prepareHistoryMedia(directory, catalog)
  const stored = []
  for (const { item, content, adapter } of prepared) {
    const file = await writeMediaFileAtomic({
      content,
      contentHash: item.sha256,
      extension: extensionForMimeType(item.mimeType),
      source: adapter,
    })
    stored.push({ item, file, source: adapter ?? "history" })
  }
  const timestamp = new Date().toISOString()
  database.transaction(() => {
    for (const { item, file, source } of stored) {
      upsertMediaAsset(database, {
        source,
        sourceUrl: item.sourceUrl,
        cachePath: file.relativePath,
        sha256: item.sha256,
        mimeType: item.mimeType,
        byteSize: item.byteSize,
        width: item.width,
        height: item.height,
        status: "ready",
        fetchedAt: timestamp,
        lastCheckedAt: timestamp,
        raw: { owner: "history", sourcePage: item.sourcePage },
      })
      registerMediaCandidates(database, [
        {
          source,
          sourceUrl: item.sourceUrl,
          ownerType: "history-image",
          ownerId: item.id,
          role: "archive-image",
          alt: item.title.en,
          raw: { sourcePage: item.sourcePage },
        },
      ])
    }
  })()
  return {
    imported: stored.length,
    bytes: stored.reduce((total, { item }) => total + item.byteSize, 0),
  }
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const args = process.argv.slice(2)
  if (args.length !== 2 || args[0] !== "--from") {
    console.error("Usage: npm run history:import -- --from <archive-directory>")
    process.exitCode = 1
  } else {
    const database = initializeDatabase()
    try {
      const result = await importHistoryMedia(database, args[1])
      console.log(
        `Imported ${result.imported} history images (${(result.bytes / 1024 / 1024).toFixed(2)} MiB).`,
      )
    } catch (error) {
      console.error(
        error.code
          ? "Unable to read the local history archive."
          : error.message,
      )
      process.exitCode = 1
    } finally {
      database.close()
    }
  }
}
