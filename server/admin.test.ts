import { listeningPort } from "./http-address.ts"
import assert from "node:assert/strict"
import test from "node:test"

import { createApp } from "./app.ts"
import {
  applyLlmCancellationJudgements,
  getEvent,
  getDashboard,
  initializeDatabase,
  upsertEvents,
  upsertVideos,
} from "./database.ts"

async function listen(app) {
  return new Promise((resolve) => {
    const server = app.listen(0, "127.0.0.1", () => resolve(server))
  })
}

async function close(server) {
  if (!server) return
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  )
}

test("production mutations accept same-origin browser requests behind HTTPS proxies", async () => {
  const database = initializeDatabase({ filename: ":memory:" })
  let server
  try {
    server = await listen(
      createApp({
        database,
        adminMode: "production",
        adminPassword: "not-a-real-password",
      }),
    )
    const backend = `http://127.0.0.1:${listeningPort(server)}`
    const proxyHeaders = {
      origin: "https://board.example.invalid",
      "x-forwarded-proto": "https",
      "sec-fetch-site": "same-origin",
      "content-type": "application/json",
    }
    const login = await fetch(`${backend}/api/admin/login`, {
      method: "POST",
      headers: proxyHeaders,
      body: JSON.stringify({ password: "not-a-real-password" }),
    })
    assert.equal(login.status, 200)
    assert.match(login.headers.get("set-cookie"), /; Secure/u)
    const cookie = login.headers.get("set-cookie").split(";", 1)[0]
    const headers = { ...proxyHeaders, cookie }
    const config = await fetch(`${backend}/api/admin/config`, { headers })
    assert.equal(config.status, 200)
    const payload = await config.json()
    assert.deepEqual(payload.providers, [])
    assert.deepEqual(payload.models, [])
    assert.ok(
      Object.values(payload.providerOrders as Record<string, string[]>).every(
        (order) => order.length === 0,
      ),
    )

    let eventId
    for (const method of ["POST", "PUT", "DELETE"]) {
      const result = await fetch(
        `${backend}/api/admin/events${method === "POST" ? "" : `/${eventId}`}`,
        {
          method,
          headers,
          ...(method === "DELETE"
            ? {}
            : {
                body: JSON.stringify({
                  id: "proxy-event",
                  title: "Proxy event",
                  startsOn: "2026-10-07",
                }),
              }),
        },
      )
      assert.equal(
        result.status,
        method === "POST" ? 201 : method === "DELETE" ? 204 : 200,
      )
      if (method === "POST") eventId = (await result.json()).event.id
    }
    for (const site of ["cross-site", "same-site", "none", ""]) {
      const rejected = await fetch(`${backend}/api/admin/config`, {
        method: "PUT",
        headers: { ...headers, "sec-fetch-site": site },
        body: JSON.stringify({ scheduleMessageEnabled: false }),
      })
      assert.equal(rejected.status, 403, site)
      assert.equal((await rejected.json()).error, "csrf_origin_mismatch")
    }
    const withoutSession = await fetch(`${backend}/api/admin/config`, {
      method: "PUT",
      headers: proxyHeaders,
      body: "{}",
    })
    assert.equal(withoutSession.status, 401)

    // Clients without Fetch Metadata must still match the observed origin.
    const direct = await fetch(`${backend}/api/admin/config`, {
      method: "PUT",
      headers: { cookie, origin: backend, "content-type": "application/json" },
      body: JSON.stringify({ scheduleMessageEnabled: false }),
    })
    assert.equal(direct.status, 200)
  } finally {
    await close(server)
    database.close()
  }
})

test("development admin API bypasses password authentication", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let server
  try {
    server = await listen(
      createApp({
        database,
        databaseLabel: ":memory:",
        adminMode: "development",
      }),
    )
    const { port } = server.address()
    const session = await fetch(
      `http://127.0.0.1:${port}/api/admin/session`,
    ).then((response) => response.json())
    assert.deepEqual(session, {
      mode: "development",
      requiresPassword: false,
      authenticated: true,
    })
    const configResponse = await fetch(
      `http://127.0.0.1:${port}/api/admin/config`,
    )
    assert.equal(configResponse.status, 200)
    assert.equal((await configResponse.json()).openAiKeyConfigured, false)
  } finally {
    await close(server)
    database.close()
  }
})

test("confirming an LLM-flagged event keeps the cancellation evidence", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let server
  try {
    upsertEvents(database, [
      {
        id: "x-flagged-stream",
        source: "x",
        source_item_id: "flagged-post",
        source_key: "2026-09-04T21:00",
        provenance: "automatic",
        title: "夜配信",
        starts_on: "2026-09-04",
        starts_at: "2026-09-04T12:00:00.000Z",
        event_type: "stream",
      },
    ])
    applyLlmCancellationJudgements(database, {
      sourceItemId: "cancel-post",
      reason: "本人が中止と告知",
      evidence: "今日の配信はお休み",
      confidence: 0.8,
      targets: [{ eventId: "x-flagged-stream" }],
    })
    server = await listen(
      createApp({
        database,
        databaseLabel: ":memory:",
        adminMode: "development",
      }),
    )
    const { port } = server.address()
    const confirmed = await fetch(
      `http://127.0.0.1:${port}/api/admin/events/x-flagged-stream/confirm`,
      { method: "POST" },
    )
    assert.equal(confirmed.status, 200)
    const event = getEvent(database, "x-flagged-stream")
    assert.equal(event.manualLocked, 1)
    assert.equal(event.cancellationStatus, "llm_suspected")
    assert.equal(event.cancellationEvidence, "今日の配信はお休み")
  } finally {
    await close(server)
    database.close()
  }
})

test("production admin login guards model settings and manual schedule CRUD", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let server
  try {
    assert.throws(
      () =>
        createApp({
          database,
          adminMode: "production",
          adminPassword: "not-a-real",
        }),
      /at least 12 characters/u,
    )

    upsertEvents(database, [
      {
        id: "youtube-abcdefghijk",
        source: "youtube",
        source_item_id: "abcdefghijk",
        source_key: "reservation",
        title: "Automatic live",
        starts_on: "2026-09-04",
        starts_at: "2026-09-04T11:00:00.000Z",
        event_type: "stream",
        status: "已预约",
        url: "https://www.youtube.com/watch?v=abcdefghijk",
      },
    ])
    upsertVideos(database, [
      {
        id: "video-admin-1",
        source: "youtube",
        title: "Admin video one",
        published_at: "2026-08-01T00:00:00.000Z",
        url: "https://www.youtube.com/watch?v=video-admin-1",
      },
      {
        id: "video-admin-2",
        source: "youtube",
        title: "Admin video two",
        published_at: "2026-08-02T00:00:00.000Z",
        url: "https://www.youtube.com/watch?v=video-admin-2",
      },
    ])
    server = await listen(
      createApp({
        database,
        databaseLabel: ":memory:",
        adminMode: "production",
        adminPassword: "not-a-real-password",
      }),
    )
    const { port } = server.address()
    const origin = `http://127.0.0.1:${port}`

    const unauthenticated = await fetch(`${origin}/api/admin/config`)
    assert.equal(unauthenticated.status, 401)
    const failedLogin = await fetch(`${origin}/api/admin/login`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ password: "not-a-real-wrong-password" }),
    })
    assert.equal(failedLogin.status, 401)

    const login = await fetch(`${origin}/api/admin/login`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ password: "not-a-real-password" }),
    })
    assert.equal(login.status, 200)
    const setCookie = login.headers.get("set-cookie")
    assert.match(setCookie, /HttpOnly/u)
    assert.match(setCookie, /SameSite=Strict/u)
    const cookie = setCookie.split(";", 1)[0]
    const authenticatedHeaders = {
      cookie,
      "content-type": "application/json",
    }

    const invalidEvent = await fetch(`${origin}/api/admin/events`, {
      method: "POST",
      headers: authenticatedHeaders,
      body: JSON.stringify({
        title: "Invalid end-only event",
        startsOn: "2026-09-05",
        startsAt: null,
        endsAt: "2026-09-05T12:00:00.000Z",
      }),
    })
    assert.equal(invalidEvent.status, 400)

    const savedConfig = await fetch(`${origin}/api/admin/config`, {
      method: "PUT",
      headers: authenticatedHeaders,
      body: JSON.stringify({
        llmModel: "gpt-4.1-mini",
        scheduleExtractionEnabled: false,
        scheduleMessageEnabled: true,
        scheduleKeywords: ["周表", "配信予定"],
        featuredVideoId: "video-admin-2",
      }),
    })
    assert.equal(savedConfig.status, 200)
    const savedConfigPayload = await savedConfig.json()
    assert.equal(savedConfigPayload.llmModel, "gpt-4.1-mini")
    assert.equal(savedConfigPayload.scheduleExtractionEnabled, true)
    assert.equal(savedConfigPayload.scheduleKeywordEnabled, false)
    assert.equal(savedConfigPayload.scheduleVisionEnabled, false)
    assert.equal(savedConfigPayload.scheduleMessageEnabled, true)
    assert.ok(Array.isArray(savedConfigPayload.providerOrders.schedule_message))
    assert.deepEqual(savedConfigPayload.scheduleKeywords, ["周表", "配信予定"])
    assert.equal(savedConfigPayload.featuredVideoId, "video-admin-2")
    assert.deepEqual(
      (
        await fetch(`${origin}/api/admin/videos`, {
          headers: { cookie },
        }).then((response) => response.json())
      ).videos.map((video) => video.id),
      ["video-admin-2", "video-admin-1"],
    )
    assert.equal(getDashboard(database).meta.featuredVideoId, "video-admin-2")

    const invalidFeatured = await fetch(`${origin}/api/admin/config`, {
      method: "PUT",
      headers: authenticatedHeaders,
      body: JSON.stringify({ featuredVideoId: "missing-video" }),
    })
    assert.equal(invalidFeatured.status, 400)

    const createdResponse = await fetch(`${origin}/api/admin/events`, {
      method: "POST",
      headers: authenticatedHeaders,
      body: JSON.stringify({
        title: "Manual event",
        detail: "Confirmed by an administrator",
        startsOn: "2026-09-05",
        startsAt: null,
        endsAt: null,
        timePrecision: "unknown",
        status: "待确认",
        eventType: "event",
        url: "https://example.invalid/event",
      }),
    })
    assert.equal(createdResponse.status, 201)
    const created = (await createdResponse.json()).event
    assert.equal(created.provenance, "manual")
    assert.equal(created.manualLocked, 1)

    const confirmedResponse = await fetch(
      `${origin}/api/admin/events/youtube-abcdefghijk/confirm`,
      { method: "POST", headers: { cookie } },
    )
    assert.equal(confirmedResponse.status, 200)
    assert.equal(getEvent(database, "youtube-abcdefghijk").provenance, "manual")

    const deletedResponse = await fetch(
      `${origin}/api/admin/events/${encodeURIComponent(created.id)}`,
      { method: "DELETE", headers: { cookie } },
    )
    assert.equal(deletedResponse.status, 204)
    assert.ok(getEvent(database, created.id).deletedAt)
  } finally {
    await close(server)
    database.close()
  }
})
