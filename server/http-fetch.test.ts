import assert from "node:assert/strict"
import test from "node:test"
import { fetchBounded } from "./http-fetch.ts"
import {
  initializeDatabase,
  getProfileMedia,
  upsertProfileMediaCandidate,
} from "./database.ts"
import { discoverProfileMedia, downloadProfileMedia } from "./profile-media.ts"

for (const bodyType of ["delayed", "endless"]) {
  test(`deadline cancels a ${bodyType} response after immediate headers`, async () => {
    let cancelled = false
    let signal: AbortSignal
    let timer
    const fetchImpl = async (_url, options) => {
      signal = options.signal
      return new Response(
        new ReadableStream({
          start(controller) {
            if (bodyType === "delayed")
              timer = setTimeout(() => {
                controller.enqueue(new Uint8Array([1]))
                controller.close()
              }, 200)
            else controller.enqueue(new Uint8Array([1]))
          },
          cancel() {
            cancelled = true
            clearTimeout(timer)
          },
        }),
      )
    }
    await assert.rejects(
      fetchBounded("https://source.example.invalid", {
        fetchImpl,
        timeoutMs: 20,
      }),
      /timed out/,
    )
    assert.equal(signal.aborted, true)
    assert.equal(cancelled, true)
  })
}
for (const declared of [true, false]) {
  test(`response size limit cancels ${declared ? "declared" : "streamed"} oversized bytes`, async () => {
    let cancelled = false
    const fetchImpl = async () =>
      new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(new Uint8Array(32))
          },
          cancel() {
            cancelled = true
          },
        }),
        { headers: declared ? { "content-length": "32" } : {} },
      )
    await assert.rejects(
      fetchBounded("https://source.example.invalid", {
        fetchImpl,
        maxBytes: 8,
      }),
      /size limit/,
    )
    assert.equal(cancelled, true)
  })
}
for (const action of ["discovery", "image"]) {
  test(`profile ${action} deadline covers a body that never ends and keeps the snapshot`, async () => {
    const database = initializeDatabase({ seed: false, filename: ":memory:" })
    let cancelled = false
    const fetchImpl = async () =>
      new Response(
        new ReadableStream({
          pull() {
            return new Promise(() => {})
          },
          cancel() {
            cancelled = true
          },
        }),
      )
    try {
      const candidate = upsertProfileMediaCandidate(database, {
        slot: "avatar",
        source: "x",
        sourceUrl: "https://pbs.twimg.com/synthetic-timeout.jpg",
      })
      if (action === "discovery")
        await assert.rejects(
          discoverProfileMedia(database, {
            slot: "avatar",
            source: "x",
            fetchImpl,
            timeoutMs: 20,
          }),
          /timed out/,
        )
      else {
        await assert.rejects(
          downloadProfileMedia(database, candidate.id, {
            fetchImpl,
            timeoutMs: 20,
          }),
          /timed out/,
        )
        assert.equal(getProfileMedia(database, candidate.id).status, "failed")
      }
      assert.equal(cancelled, true)
      assert.equal(
        database
          .prepare<unknown[], { count: number }>(
            "SELECT COUNT(*) AS count FROM profile_media",
          )
          .get().count,
        1,
      )
    } finally {
      database.close()
    }
  })
}
