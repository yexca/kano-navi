import assert from "node:assert/strict"
import fs from "node:fs"
import test from "node:test"

import {
  initializeDatabase,
  listMediaAssets,
  registerMediaCandidates,
} from "./database.js"
import { resolveMediaCachePath } from "./media-cache.js"
import { defaultAllowedHosts, downloadMediaAsset } from "./media-downloader.js"

function register(database, sourceUrl) {
  registerMediaCandidates(database, [
    {
      source: "x",
      sourceUrl,
      ownerType: "post",
      ownerId: "download-test",
      role: "post-image",
    },
  ])
  return listMediaAssets(database).find(
    (asset) => asset.sourceUrl === sourceUrl,
  )
}

test("downloader allows only the known YouTube thumbnail CDN hosts", () => {
  for (const hostname of [
    "i.ytimg.com",
    "i1.ytimg.com",
    "i2.ytimg.com",
    "i3.ytimg.com",
    "i4.ytimg.com",
  ]) {
    assert.equal(defaultAllowedHosts.has(hostname), true)
  }
  assert.equal(defaultAllowedHosts.has("i5.ytimg.com"), false)
})

test("downloader validates and stores an allowed image atomically", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const sourceUrl = "https://pbs.twimg.com/media/download-test.png"
  const body = Buffer.from([
    0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x01,
  ])
  let cachedFile
  try {
    const asset = register(database, sourceUrl)
    const result = await downloadMediaAsset(database, asset, {
      fetchImpl: async () =>
        new Response(body, {
          status: 200,
          headers: {
            "content-type": "image/png",
            "content-length": String(body.length),
          },
        }),
    })
    assert.equal(result.status, "ready")
    const ready = listMediaAssets(database).find(
      (item) => item.sourceUrl === sourceUrl,
    )
    assert.equal(ready.mimeType, "image/png")
    assert.equal(ready.byteSize, body.length)
    cachedFile = resolveMediaCachePath(ready.cachePath)
    assert.deepEqual(fs.readFileSync(cachedFile), body)
  } finally {
    database.close()
    if (cachedFile) fs.rmSync(cachedFile, { force: true })
  }
})

test("downloader rejects redirects outside the media host allowlist", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    const asset = register(
      database,
      "https://pbs.twimg.com/media/redirect-test.jpg",
    )
    let calls = 0
    const result = await downloadMediaAsset(database, asset, {
      fetchImpl: async () => {
        calls += 1
        return new Response(null, {
          status: 302,
          headers: { location: "https://example.invalid/image.jpg" },
        })
      },
    })
    assert.equal(result.status, "failed")
    assert.match(result.error, /host is not allowed/u)
    assert.equal(calls, 1)
    assert.equal(listMediaAssets(database)[0].status, "failed")
  } finally {
    database.close()
  }
})
