import assert from "node:assert/strict"
import fs from "node:fs"
import test from "node:test"

import {
  getDashboard,
  initializeDatabase,
  upsertMediaAsset,
} from "./database.js"
import {
  mediaIdForSourceUrl,
  resolveMediaCachePath,
  writeMediaFileAtomic,
} from "./media-cache.js"

test("dashboard never exposes an unready remote URL", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    database.exec(`
      INSERT INTO posts (id, source, type, text, published_at, url, media_url)
      VALUES ('post-1', 'x', 'daily', 'hello', '2026-08-28T00:00:00Z', 'https://x.com/example/status/1', 'https://cdn.example.invalid/post.jpg')
    `)
    const post = getDashboard(database, {
      now: new Date("2026-08-28T01:00:00Z"),
    }).posts[0]
    assert.equal(post.mediaUrl, null)
    assert.equal(post.mediaStatus, "pending")
    assert.equal(post.mediaSourceUrl, "https://cdn.example.invalid/post.jpg")
  } finally {
    database.close()
  }
})

test("ready media keeps its opaque URL and rejects unsafe cache metadata", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let cachedFile = null
  try {
    const sourceUrl = "https://cdn.example.invalid/ready.jpg"
    const id = mediaIdForSourceUrl(sourceUrl)
    assert.throws(
      () =>
        upsertMediaAsset(database, {
          sourceUrl,
          status: "ready",
          cachePath: "../outside.jpg",
        }),
      /ready media asset requires a cache path|invalid media cache path/,
    )
    assert.throws(
      () => upsertMediaAsset(database, { sourceUrl, status: "unknown" }),
      /invalid media status/,
    )
    const written = await writeMediaFileAtomic({
      content: Buffer.from([0xff, 0xd8, 0xff, 0xd9]),
      extension: "jpg",
    })
    cachedFile = resolveMediaCachePath(written.relativePath)
    upsertMediaAsset(database, {
      id,
      sourceUrl,
      status: "ready",
      cachePath: written.relativePath,
      mimeType: "image/jpeg",
      sha256: written.sha256,
      byteSize: written.byteSize,
    })
    const row = database
      .prepare("SELECT id, status, cache_path FROM media_assets WHERE id = ?")
      .get(id)
    assert.deepEqual(row, {
      id,
      status: "ready",
      cache_path: written.relativePath,
    })
    assert.equal(
      resolveMediaCachePath(row.cache_path)?.includes("/data/cache/media/"),
      true,
    )

    database
      .prepare(
        `
      INSERT INTO posts (id, source, type, text, published_at, url, media_url)
      VALUES ('post-ready', 'x', 'daily', 'hello', '2026-08-28T00:00:00Z', 'https://x.com/example/status/2', ?)
    `,
      )
      .run(sourceUrl)
    const post = getDashboard(database, {
      now: new Date("2026-08-28T01:00:00Z"),
    }).posts[0]
    assert.equal(post.mediaUrl, `/media/${id}?v=${written.sha256}`)

    upsertMediaAsset(database, {
      sourceUrl,
      status: "failed",
      lastError: "temporary failure",
    })
    assert.equal(
      database.prepare("SELECT status FROM media_assets WHERE id = ?").get(id)
        .status,
      "ready",
    )
  } finally {
    database.close()
    if (cachedFile) fs.rmSync(cachedFile, { force: true })
  }
})
