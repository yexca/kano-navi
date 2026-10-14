import assert from "node:assert/strict"
import crypto from "node:crypto"
import fs from "node:fs/promises"
import os from "node:os"
import path from "node:path"
import test from "node:test"

import {
  mediaIdForSourceUrl,
  packageHistoryMedia,
  sha256ForContent,
} from "../scripts/package-history-media.ts"
import { historyMedia } from "../src/history/media.ts"
import { milestones } from "../src/history/milestones.ts"

async function fixture(t) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "kano-history-test-"))
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
    mediaId: mediaIdForSourceUrl(sourceUrl),
    sha256: sha256ForContent(content),
    byteSize: content.length,
    mimeType: "image/png",
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
    assert.equal(path.dirname(root), path.resolve(os.tmpdir()))
    assert.ok(path.basename(root).startsWith("kano-history-test-"))
    await fs.rm(root, { recursive: true, force: true })
  })
  return { root, content, image, records, saveManifest }
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
    assert.match(
      item.url,
      /^\/assets\/history\/[a-zA-Z0-9_.-]+\?v=[a-f0-9]{64}$/u,
    )
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

test("every fixed history image ships with its catalog bytes and identity", async () => {
  for (const item of historyMedia) {
    const content = await fs.readFile(
      new URL(`../public/assets/history/${item.filename}`, import.meta.url),
    )
    assert.equal(content.length, item.byteSize, item.id)
    assert.equal(sha256ForContent(content), item.sha256, item.id)
  }
})

test("history packaging validates the archive before publishing fixed images", async (t) => {
  const { root, content, image } = await fixture(t)
  const destination = path.join(root, "packaged")
  assert.deepEqual(await packageHistoryMedia(root, destination, [image]), {
    packaged: 1,
    bytes: content.length,
  })
  const packagedFile = path.join(destination, image.filename)
  assert.deepEqual(await fs.readFile(packagedFile), content)
  await fs.writeFile(
    path.join(root, image.filename),
    Buffer.alloc(content.length),
  )
  await assert.rejects(
    packageHistoryMedia(root, destination, [image]),
    /identity mismatch/u,
  )
  assert.deepEqual(await fs.readFile(packagedFile), content)
})

test("history packaging rejects traversal, wrong MIME, and changed size before writing", async (t) => {
  const { root, image, records, saveManifest } = await fixture(t)
  const destination = path.join(root, "packaged")
  records[0].file = "../outside.png"
  await saveManifest()
  await assert.rejects(
    packageHistoryMedia(root, destination, [image]),
    /unsafe archive image path/u,
  )
  records[0].file = image.filename
  await saveManifest()
  await assert.rejects(
    packageHistoryMedia(root, destination, [
      { ...image, mimeType: "image/gif" },
    ]),
    /identity mismatch/u,
  )
  await assert.rejects(
    packageHistoryMedia(root, destination, [
      { ...image, byteSize: image.byteSize + 1 },
    ]),
    /size mismatch/u,
  )
  await assert.rejects(fs.access(destination))
})
