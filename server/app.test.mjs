import assert from "node:assert/strict"
import fs from "node:fs"
import test from "node:test"

import { createApp } from "./app.js"
import { initializeDatabase, upsertMediaAsset } from "./database.js"
import { mediaIdForSourceUrl, resolveMediaCachePath, writeMediaFileAtomic } from "./media-cache.js"

test("media route serves only ready opaque-ID assets", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const body = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
  const sourceUrl = "https://cdn.example.invalid/route.png"
  const id = mediaIdForSourceUrl(sourceUrl)
  const written = await writeMediaFileAtomic({ content: body, extension: "png" })
  const cachedFile = resolveMediaCachePath(written.relativePath)
  let server

  try {
    upsertMediaAsset(database, {
      id,
      source: "test",
      sourceUrl,
      status: "ready",
      cachePath: written.relativePath,
      mimeType: "image/png",
      sha256: written.sha256,
      byteSize: written.byteSize,
    })
    const app = createApp({ database, databaseLabel: ":memory:" })
    server = await new Promise((resolve) => {
      const listeningServer = app.listen(0, "127.0.0.1", () => resolve(listeningServer))
    })
    const address = server.address()
    const origin = `http://127.0.0.1:${address.port}`

    const response = await fetch(`${origin}/media/${id}?v=${written.sha256}`)
    assert.equal(response.status, 200)
    assert.equal(response.headers.get("content-type"), "image/png")
    assert.equal(response.headers.get("x-content-type-options"), "nosniff")
    assert.match(response.headers.get("cache-control"), /immutable/u)
    assert.deepEqual(Buffer.from(await response.arrayBuffer()), body)

    const staleResponse = await fetch(`${origin}/media/${id}?v=${"0".repeat(64)}`)
    assert.equal(staleResponse.status, 404)

    const invalidResponse = await fetch(`${origin}/media/not-an-id`)
    assert.equal(invalidResponse.status, 404)
  } finally {
    if (server) await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
    database.close()
    if (cachedFile) fs.rmSync(cachedFile, { force: true })
  }
})
