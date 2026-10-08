import assert from "node:assert/strict"
import path from "node:path"
import test from "node:test"

import {
  cacheRelativePathForHash,
  isSafeMediaId,
  mediaIdForSourceUrl,
  normalizeMediaMimeType,
  normalizeSourceUrl,
  resolveMediaCachePath,
} from "./media-cache.ts"
import {
  getDashboard,
  initializeDatabase,
  listMediaAssets,
  listMediaLinks,
  registerMediaCandidates,
  upsertMediaAsset,
} from "./database.ts"

test("media source identities are normalized and opaque", () => {
  const first = mediaIdForSourceUrl(
    "https://cdn.example.invalid/image.jpg#preview",
  )
  const second = mediaIdForSourceUrl("https://cdn.example.invalid/image.jpg")
  assert.equal(first, second)
  assert.equal(isSafeMediaId(first), true)
  assert.equal(normalizeSourceUrl("file:///private/image.jpg"), null)
  assert.equal(normalizeSourceUrl("/assets/kano-avatar.jpg"), null)
  assert.equal(
    normalizeMediaMimeType("image/jpeg; charset=binary"),
    "image/jpeg",
  )
  assert.equal(normalizeMediaMimeType("text/html"), null)
})

test("cache paths reject traversal and preserve content-addressed layout", () => {
  const hash = "a".repeat(64)
  assert.equal(cacheRelativePathForHash(hash, ".JPG"), `sha256/aa/${hash}.jpg`)
  assert.equal(resolveMediaCachePath("../outside.jpg"), null)
  assert.equal(resolveMediaCachePath("/outside.jpg"), null)
  assert.equal(
    resolveMediaCachePath("sha256/aa/file.jpg")?.endsWith(
      path.join("data", "cache", "media", "sha256", "aa", "file.jpg"),
    ),
    true,
  )
})

test("remote candidates create durable assets and polymorphic links", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    const sourceUrl = "https://cdn.example.invalid/post.jpg"
    const result = registerMediaCandidates(database, [
      {
        source: "x",
        sourceUrl,
        ownerType: "post",
        ownerId: "post-1",
        role: "post-image",
        alt: "test image",
      },
    ])
    assert.deepEqual(result, { registered: 1, linked: 1, skipped: 0 })
    const assets = listMediaAssets(database)
    assert.equal(assets.length, 1)
    assert.equal(assets[0].sourceUrl, sourceUrl)
    assert.equal(assets[0].status, "pending")
    assert.equal(
      listMediaLinks(database, { ownerType: "post", ownerId: "post-1" })[0]
        .role,
      "post-image",
    )

    upsertMediaAsset(database, {
      source: "x",
      sourceUrl,
      status: "failed",
      lastError: "temporary failure",
    })
    assert.equal(listMediaAssets(database)[0].status, "failed")
    registerMediaCandidates(database, [
      {
        source: "x",
        sourceUrl,
        ownerType: "post",
        ownerId: "post-1",
        role: "post-image",
      },
    ])
    assert.equal(listMediaAssets(database)[0].status, "pending")

    const replacementUrl = "https://cdn.example.invalid/post-replacement.jpg"
    registerMediaCandidates(database, [
      {
        source: "x",
        sourceUrl: replacementUrl,
        ownerType: "post",
        ownerId: "post-1",
        role: "post-image",
      },
    ])
    const ownerLinks = listMediaLinks(database, {
      ownerType: "post",
      ownerId: "post-1",
    })
    assert.equal(ownerLinks.length, 1)
    assert.equal(ownerLinks[0].mediaId, mediaIdForSourceUrl(replacementUrl))

    const dashboard = getDashboard(database)
    assert.deepEqual(dashboard.meta.mediaCache, { total: 2, pending: 2 })
  } finally {
    database.close()
  }
})
