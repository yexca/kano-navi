import assert from "node:assert/strict"
import crypto from "node:crypto"
import fs from "node:fs/promises"
import os from "node:os"
import path from "node:path"
import test from "node:test"

import { importHistoryMedia } from "../scripts/import-history-media.mjs"
import { historyMedia } from "../src/history/media.js"
import { milestones } from "../src/history/milestones.js"
import {
  getMediaAsset,
  initializeDatabase,
  listMediaAssets,
  listMediaLinks,
} from "./database.js"
import {
  mediaIdForSourceUrl,
  resolveMediaCachePath,
  sha256ForContent,
} from "./media-cache.js"

async function fixture(t) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "kano-history-test-"))
  const database = initializeDatabase({ filename: ":memory:", seed: false })
  const content = Buffer.concat([
    Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j1WQAAAAASUVORK5CYII=",
      "base64",
    ),
    crypto.randomBytes(16),
  ])
  const sourceUrl = "https://images.example.invalid/history.png"
  const image = {
    id: "fixture-image",
    filename: "history.png",
    sourceUrl,
    sourcePage: "https://example.invalid/history",
    mediaId: mediaIdForSourceUrl(sourceUrl),
    sha256: sha256ForContent(content),
    byteSize: content.length,
    mimeType: "image/png",
    width: 1,
    height: 1,
    title: { en: "Synthetic archive image" },
  }
  await fs.writeFile(path.join(root, image.filename), content)
  const records = [{ filename: image.filename, file: image.filename }]
  const saveManifest = () =>
    fs.writeFile(
      path.join(root, "manifest.json"),
      JSON.stringify({ images: records }),
    )
  await saveManifest()
  t.after(async () => {
    for (const asset of listMediaAssets(database)) {
      if (asset.cachePath && asset.sha256 === image.sha256) {
        const cachePath = resolveMediaCachePath(asset.cachePath)
        assert.equal(path.basename(cachePath), `${image.sha256}.png`)
        await fs.rm(cachePath, { force: true })
      }
    }
    database.close()
    assert.equal(path.dirname(root), path.resolve(os.tmpdir()))
    assert.ok(path.basename(root).startsWith("kano-history-test-"))
    await fs.rm(root, { recursive: true, force: true })
  })
  return { root, database, content, image, records, saveManifest }
}

test("history catalog preserves distinct source identities, dates, and milestone references", () => {
  const milestoneIds = new Set(milestones.map((item) => item.id))
  assert.equal(
    new Set(historyMedia.map((item) => item.id)).size,
    historyMedia.length,
  )
  assert.equal(
    new Set(historyMedia.map((item) => item.mediaId)).size,
    historyMedia.length,
  )
  for (const item of historyMedia) {
    assert.equal(item.mediaId, mediaIdForSourceUrl(item.sourceUrl))
    assert.ok(!item.milestoneId || milestoneIds.has(item.milestoneId))
    assert.match(item.url, /^\/media\/[a-f0-9]{64}\?v=[a-f0-9]{64}$/u)
    assert.ok(item.title.en && item.title.ja && item.title["zh-CN"])
  }
  const frames = historyMedia.filter((item) => item.frameTime)
  assert.equal(frames.length, 4)
  assert.ok(
    frames.every(
      (item) =>
        item.sourceKind === "streamArchive" &&
        item.originalStream.includes("youtube.com/watch?") &&
        item.dateKind === "stream",
    ),
  )
  const portrait = historyMedia.find(
    (item) => item.id === "12_sona_2026_official_fullbody",
  )
  assert.equal(portrait.dateKind, "capture")
  assert.equal(portrait.date, "2026-09-30")
})

test("offline history import preserves original bytes and is idempotent", async (t) => {
  const { root, database, content, image } = await fixture(t)
  assert.deepEqual(await importHistoryMedia(database, root, [image]), {
    imported: 1,
    bytes: content.length,
  })
  await importHistoryMedia(database, root, [image])
  const asset = getMediaAsset(database, image.mediaId)
  assert.equal(asset.status, "ready")
  assert.equal(asset.source, "history")
  assert.deepEqual(
    await fs.readFile(resolveMediaCachePath(asset.cachePath)),
    content,
  )
  assert.equal(listMediaAssets(database).length, 1)
  assert.equal(
    listMediaLinks(database, { ownerType: "history-image", ownerId: image.id })
      .length,
    1,
  )
})

test("a changed archive is rejected before any partial database update", async (t) => {
  const { root, database, content, image, records, saveManifest } =
    await fixture(t)
  await importHistoryMedia(database, root, [image])
  const next = {
    ...image,
    id: "next-image",
    filename: "next.png",
    sourceUrl: "https://images.example.invalid/next.png",
  }
  next.mediaId = mediaIdForSourceUrl(next.sourceUrl)
  records.push({ filename: next.filename, file: next.filename })
  await saveManifest()
  await fs.writeFile(
    path.join(root, next.filename),
    Buffer.alloc(content.length),
  )
  await assert.rejects(
    importHistoryMedia(database, root, [image, next]),
    /identity mismatch/u,
  )
  assert.equal(listMediaAssets(database).length, 1)
  assert.equal(getMediaAsset(database, image.mediaId).status, "ready")
  assert.deepEqual(
    await fs.readFile(
      resolveMediaCachePath(getMediaAsset(database, image.mediaId).cachePath),
    ),
    content,
  )
})

test("offline history import rejects traversal, wrong MIME, and changed size", async (t) => {
  const { root, database, image, records, saveManifest } = await fixture(t)
  records[0].file = "../outside.png"
  await saveManifest()
  await assert.rejects(
    importHistoryMedia(database, root, [image]),
    /unsafe archive image path/u,
  )
  records[0].file = image.filename
  await saveManifest()
  await assert.rejects(
    importHistoryMedia(database, root, [{ ...image, mimeType: "image/gif" }]),
    /identity mismatch/u,
  )
  await assert.rejects(
    importHistoryMedia(database, root, [
      { ...image, byteSize: image.byteSize + 1 },
    ]),
    /size mismatch/u,
  )
  assert.equal(listMediaAssets(database).length, 0)
})
