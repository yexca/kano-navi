import { listeningPort } from "./http-address.ts"
import assert from "node:assert/strict"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"

import { createApp } from "./app.ts"
import {
  finishSyncRun,
  initializeDatabase,
  startSyncRun,
  upsertEvents,
  upsertMediaAsset,
} from "./database.ts"
import {
  mediaIdForSourceUrl,
  resolveMediaCachePath,
  writeMediaFileAtomic,
} from "./media-cache.ts"

test("SPA routes serve a build inside a hidden parent directory", async () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-static-"))
  const staticDirectory = path.join(directory, ".build")
  fs.mkdirSync(staticDirectory)
  fs.writeFileSync(
    path.join(staticDirectory, "index.html"),
    '<div id="root">Snapshot board</div>',
  )
  fs.writeFileSync(path.join(staticDirectory, ".hidden.txt"), "Private fixture")
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const server = createApp({ database, staticDirectory }).listen(0, "127.0.0.1")
  await new Promise<void>((resolve) => server.once("listening", resolve))
  const origin = `http://127.0.0.1:${listeningPort(server)}`
  try {
    for (const route of ["/", "/history", "/about", "/admin"]) {
      const response = await fetch(`${origin}${route}`)
      assert.equal(response.status, 200)
      assert.match(await response.text(), /Snapshot board/u)
    }
    const hidden = await fetch(`${origin}/.hidden.txt`)
    assert.doesNotMatch(await hidden.text(), /Private fixture/u)
    assert.equal((await fetch(`${origin}/api/missing`)).status, 404)
  } finally {
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    )
    database.close()
    fs.rmSync(directory, { recursive: true, force: true })
  }
})

test("media route serves only ready opaque-ID assets", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const body = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
  const sourceUrl = "https://cdn.example.invalid/route.png"
  const id = mediaIdForSourceUrl(sourceUrl)
  const written = await writeMediaFileAtomic({
    content: body,
    extension: "png",
  })
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
      const listeningServer = app.listen(0, "127.0.0.1", () =>
        resolve(listeningServer),
      )
    })
    const address = server.address()
    const origin = `http://127.0.0.1:${address.port}`

    const response = await fetch(`${origin}/media/${id}?v=${written.sha256}`)
    assert.equal(response.status, 200)
    assert.equal(response.headers.get("content-type"), "image/png")
    assert.equal(response.headers.get("x-content-type-options"), "nosniff")
    assert.match(response.headers.get("cache-control"), /immutable/u)
    assert.deepEqual(Buffer.from(await response.arrayBuffer()), body)

    const staleResponse = await fetch(
      `${origin}/media/${id}?v=${"0".repeat(64)}`,
    )
    assert.equal(staleResponse.status, 404)

    const invalidResponse = await fetch(`${origin}/media/not-an-id`)
    assert.equal(invalidResponse.status, 404)
  } finally {
    if (server)
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      )
    database.close()
    if (cachedFile) fs.rmSync(cachedFile, { force: true })
  }
})

test("public dashboard omits operator sync counters and event identities", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let server
  try {
    upsertEvents(database, [
      {
        id: "event-public",
        source: "x",
        source_item_id: "status-private-detail",
        title: "Public stream",
        starts_on: "2099-01-01",
        starts_at: "2099-01-01T11:00:00.000Z",
        status: "scheduled",
      },
    ])
    const runId = startSyncRun(database, "manual")
    finishSyncRun(database, runId, {
      status: "partial",
      message: "kept snapshot",
      counts: { youtube: { error: "raw upstream detail" } },
    })
    const app = createApp({ database, databaseLabel: ":memory:" })
    server = await new Promise((resolve) => {
      const listeningServer = app.listen(0, "127.0.0.1", () =>
        resolve(listeningServer),
      )
    })
    const origin = `http://127.0.0.1:${listeningPort(server)}`
    const response = await fetch(`${origin}/api/dashboard?days=3`)
    assert.equal(response.status, 200)
    const payload = await response.json()
    assert.equal(payload.meta.lastSync.status, "partial")
    assert.equal(payload.meta.lastSync.counts, undefined)
    assert.equal(payload.meta.lastSync.jobId, undefined)
    assert.equal(payload.events[0].sourceItemId, undefined)
    assert.equal(payload.events[0].isUpcoming, true)
    assert.equal(payload.summary.nextEvent.id, "event-public")
    assert.equal(payload.summary.nextEvent.sourceItemId, undefined)
    assert.doesNotMatch(JSON.stringify(payload), /raw upstream detail/u)
  } finally {
    if (server)
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      )
    database.close()
  }
})
