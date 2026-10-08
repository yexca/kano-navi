import assert from "node:assert/strict"
import test from "node:test"

import {
  deleteManualEvent,
  getDashboard,
  getEvent,
  initializeDatabase,
  listAdminEvents,
  listAdminEventsPage,
  replaceAutomaticEventsForSource,
  updateManualCancellation,
  updateManualEvent,
  upsertEvents,
  upsertPosts,
  upsertVideos,
} from "./database.ts"
import {
  extractSchedulePost,
  normalizeExtractedEvents,
} from "./schedule-extractor.ts"
import { sortEvents } from "../src/dashboard/format.ts"
import { pickSpotlight } from "../src/dashboard/spotlight.ts"

const morning = {
  title: "Morning stream",
  date: "2026-09-16",
  time: "10:00",
  endTime: null,
  timePrecision: "exact",
  eventType: "stream",
  confidence: 0.9,
  url: null,
}
const evening = { ...morning, title: "Evening stream", time: "21:00" }
const post = {
  id: "schedule-regression",
  source: "x",
  text: "Synthetic schedule",
  published_at: "2026-09-15T00:00:00Z",
  url: "https://example.invalid/schedule",
}
const now = new Date("2026-09-15T00:00:00Z")

test("hero picks the earlier instant across stream and event offsets without inventing date-only time", () => {
  const stream = {
    title: "20:00",
    scheduledAt: "2026-09-16T11:00:00Z",
    url: "https://example.invalid/video",
  }
  const event = {
    title: "19:00",
    startsAt: "2026-09-16T19:00:00+09:00",
    startsOn: morning.date,
    url: "https://example.invalid/event",
  }
  assert.equal(
    pickSpotlight({ nextStream: stream, nextEvent: event }, now.getTime())
      .event,
    event,
  )
  const dateOnly = { ...event, startsAt: null }
  assert.equal(
    pickSpotlight({ nextStream: stream, nextEvent: dateOnly }, now.getTime())
      .video,
    stream,
  )
  const picked = pickSpotlight({ nextEvent: dateOnly }, now.getTime())
  assert.equal(picked.time, null)
  assert.equal(picked.dateOnly, morning.date)
})

function replace(database, sourceItemId, events) {
  return replaceAutomaticEventsForSource(database, {
    source: "x",
    sourceItemId,
    events: normalizeExtractedEvents({ events }, { ...post, id: sourceItemId }),
  })
}

test("different same-day activities coexist while repeated and cross-post evidence merges", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    replace(database, "morning-post", [morning])
    const firstId = getDashboard(database, { now }).events[0].id
    replace(database, "evening-post", [evening])
    replace(database, "morning-post", [morning])
    replace(database, "another-account-post", [morning])
    assert.deepEqual(
      getDashboard(database, { now }).events.map((event) => event.title),
      [morning.title, evening.title],
    )
    assert.equal(getEvent(database, firstId).title, morning.title)
    replace(database, "morning-post", [])
    assert.equal(getDashboard(database, { now }).events.length, 2)
    replace(database, "another-account-post", [])
    assert.deepEqual(
      getDashboard(database, { now }).events.map((e) => e.title),
      [evening.title],
    )
  } finally {
    database.close()
  }
})

test("forced model extraction preserves identities through 1 to 2 to 1 and repeated results", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let modelEvents = [morning]
  try {
    upsertPosts(database, [post])
    const extract = () =>
      extractSchedulePost(database, post, {
        apiKey: "not-a-real-api-key",
        baseUrl: "https://provider.example.invalid/v1",
        model: "synthetic-model",
        force: true,
        fetchImpl: async () =>
          new Response(
            JSON.stringify({
              output_text: JSON.stringify({ events: modelEvents }),
            }),
            { status: 200 },
          ),
      })
    assert.equal((await extract()).status, "success")
    const firstId = getDashboard(database, { now }).events[0].id
    modelEvents = [morning, evening]
    assert.equal((await extract()).status, "success")
    assert.equal(getDashboard(database, { now }).events.length, 2)
    assert.equal(getEvent(database, firstId).deletedAt, null)
    modelEvents = [morning]
    assert.equal((await extract()).status, "success")
    assert.equal((await extract()).status, "success")
    assert.deepEqual(
      getDashboard(database, { now }).events.map((e) => e.id),
      [firstId],
    )
  } finally {
    database.close()
  }
})

test("re-extraction separates legacy date-ID collisions without moving the surviving activity", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const legacyId = "x-schedule-20260916"
  try {
    // Reproduce the old write pattern: two different posts attached evidence
    // to the same date ID, and the later activity replaced the earlier row.
    for (const [sourceItemId, activity] of [
      ["old-morning", morning],
      ["old-evening", evening],
    ] as const) {
      const event = normalizeExtractedEvents(
        { events: [activity] },
        { ...post, id: sourceItemId },
      )[0]
      replaceAutomaticEventsForSource(database, {
        source: "x",
        sourceItemId,
        events: [{ ...event, id: legacyId }],
      })
    }
    assert.equal(getDashboard(database, { now }).events.length, 1)
    assert.equal(getEvent(database, legacyId).title, evening.title)
    replace(database, "old-morning", [morning])
    replace(database, "old-evening", [evening])
    assert.deepEqual(
      getDashboard(database, { now }).events.map((event) => event.title),
      [morning.title, evening.title],
    )
    assert.equal(getEvent(database, legacyId).title, evening.title)
  } finally {
    database.close()
  }
})

for (const protection of ["automatic", "manual", "deleted"]) {
  test(`legacy date IDs retain identity and ${protection} protection after re-extraction`, () => {
    const database = initializeDatabase({ seed: false, filename: ":memory:" })
    const legacyId = "x-schedule-20260916"
    try {
      const normalized = normalizeExtractedEvents(
        { events: [morning] },
        post,
      )[0]
      replaceAutomaticEventsForSource(database, {
        source: "x",
        sourceItemId: post.id,
        events: [{ ...normalized, id: legacyId }],
      })
      if (protection !== "automatic")
        updateManualEvent(database, legacyId, {
          ...normalized,
          title: "Human title",
        })
      if (protection === "deleted") deleteManualEvent(database, legacyId)
      replace(database, post.id, [morning, evening])
      replace(database, "other-post", [morning])
      const rows = listAdminEvents(database, { includeDeleted: true })
      assert.equal(rows.length, 2)
      assert.equal(
        getEvent(database, legacyId).title,
        protection === "automatic" ? morning.title : "Human title",
      )
      assert.equal(
        Boolean(getEvent(database, legacyId).deletedAt),
        protection === "deleted",
      )
      replace(database, post.id, [morning])
      replace(database, post.id, [morning, evening])
      assert.equal(
        listAdminEvents(database, { includeDeleted: true }).length,
        2,
      )
    } finally {
      database.close()
    }
  })
}

test("source tuple upserts reuse legacy IDs and replacement failures roll back all event and evidence writes", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    const original = {
      id: "legacy",
      source: "x",
      source_item_id: "legacy-post",
      source_key: "morning",
      title: "original",
      starts_on: morning.date,
    }
    upsertEvents(database, [original])
    upsertEvents(database, [{ ...original, id: "new-id", title: "updated" }])
    assert.equal(getEvent(database, "legacy").title, "updated")
    assert.equal(getEvent(database, "new-id"), null)
    const snapshot = () => ({
      events: database.prepare("SELECT * FROM events ORDER BY id").all(),
      sources: database
        .prepare("SELECT * FROM event_sources ORDER BY id")
        .all(),
    })
    const before = snapshot()
    assert.throws(
      () =>
        replaceAutomaticEventsForSource(database, {
          source: "x",
          sourceItemId: "new-post",
          events: [
            { id: "valid", title: "valid", starts_on: morning.date },
            { id: "invalid", title: "invalid", starts_at: "invalid" },
          ],
        }),
      /requires starts_on/u,
    )
    assert.deepEqual(snapshot(), before)
  } finally {
    database.close()
  }
})

test("X evidence and YouTube reservation share one event and confirmed cancellation survives sync", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const videoId = "synthetic01"
  const id = `youtube-${videoId}`
  const reservation = {
    id,
    source: "youtube",
    source_item_id: videoId,
    source_key: "reservation",
    title: "Reserved",
    starts_on: morning.date,
    starts_at: "2026-09-16T01:00:00Z",
  }
  const read = () => getDashboard(database, { now })
  try {
    upsertVideos(database, [
      { id: videoId, title: "Reserved", scheduled_at: reservation.starts_at },
    ])
    upsertEvents(database, [reservation])
    replace(database, post.id, [
      { ...morning, url: `https://www.youtube.com/watch?v=${videoId}` },
    ])
    assert.equal(read().events.length, 1)
    database
      .prepare(
        "UPDATE events SET cancellation_status='llm_suspected' WHERE id=?",
      )
      .run(id)
    assert.equal(read().summary.nextStream.id, videoId)
    updateManualCancellation(database, id, {
      status: "manual_confirmed",
      reason: "Operator confirmed",
      evidence: "Synthetic notice",
    })
    assert.equal(read().summary.nextEvent, null)
    assert.equal(read().summary.nextStream, null)
    assert.equal(read().events[0].cancellationReason, "Operator confirmed")
    assert.equal(read().events[0].cancellationEvidence, "Synthetic notice")
    upsertEvents(database, [reservation])
    assert.equal(read().summary.nextStream, null)
    assert.equal(read().events[0].cancellationStatus, "manual_confirmed")
  } finally {
    database.close()
  }
})

test("dashboard, streams, admin pagination and frontend sort mixed ISO offsets by instant with stable ties", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    upsertEvents(database, [
      {
        id: "later",
        title: "20:00",
        starts_on: morning.date,
        starts_at: "2026-09-16T11:00:00Z",
      },
      { id: "date-only", title: "Unknown time", starts_on: morning.date },
      {
        id: "tie-b",
        title: "19:00",
        starts_on: morning.date,
        starts_at: "2026-09-16T10:00:00.000Z",
      },
      {
        id: "tie-a",
        title: "19:00",
        starts_on: morning.date,
        starts_at: "2026-09-16T19:00:00+09:00",
      },
    ])
    upsertVideos(database, [
      { id: "later", title: "20:00", scheduled_at: "2026-09-16T11:00:00Z" },
      { id: "tie-b", title: "19:00", scheduled_at: "2026-09-16T10:00:00.000Z" },
      {
        id: "tie-a",
        title: "19:00",
        scheduled_at: "2026-09-16T19:00:00+09:00",
      },
    ])
    const dashboard = getDashboard(database, { now })
    assert.deepEqual(
      dashboard.events.map((e) => e.id),
      ["tie-a", "tie-b", "later", "date-only"],
    )
    assert.equal(dashboard.summary.nextEvent.id, "tie-a")
    assert.equal(dashboard.summary.nextStream.id, "tie-a")
    assert.deepEqual(
      sortEvents([...dashboard.events].reverse()).map((e) => e.id),
      ["tie-a", "tie-b", "later", "date-only"],
    )
    assert.deepEqual(
      listAdminEventsPage(database).items.map((e) => e.id),
      ["date-only", "later", "tie-a", "tie-b"],
    )
    assert.equal(
      dashboard.events.find((e) => e.id === "date-only").startsAt,
      null,
    )
    assert.deepEqual(
      getDashboard(database, { now: new Date("2026-09-16T10:30:00Z") }).summary
        .nextEvent.id,
      "later",
    )
  } finally {
    database.close()
  }
})
