import assert from "node:assert/strict"
import test from "node:test"
import type { Dashboard } from "../src/types.ts"
import {
  CLOCK_SKEW_MS,
  freshnessAt,
  STALE_AFTER_MS,
  syncStateFor,
} from "../src/dashboard/freshness.ts"

const now = Date.parse("2026-01-02T00:00:00Z")
const recent = "2026-01-01T23:00:00Z"
const t = (key: string) => key

function meta(overrides: Partial<Dashboard["meta"]> = {}): Dashboard["meta"] {
  return {
    fetchedAt: recent,
    lastSync: { status: "success", startedAt: recent, finishedAt: recent },
    sourceFetchedAt: { x: recent, youtube: recent, bilibili: recent },
    postWindowDays: 3,
    xAccounts: [],
    featuredVideoId: null,
    ...overrides,
  }
}

test("freshness ages locally at the inclusive 24-hour boundary", () => {
  assert.equal(freshnessAt("2026-01-01T00:00:00Z", now), "stale")
  assert.equal(
    freshnessAt(new Date(now - STALE_AFTER_MS + 1).toISOString(), now),
    "fresh",
  )
  const snapshot = meta()
  const healthy = syncStateFor(snapshot, null, now, t)
  assert.equal(healthy.label, "sync.snapshot")
  assert.equal(healthy.runAt, recent)
  const aged = syncStateFor(snapshot, null, now + STALE_AFTER_MS, t)
  assert.equal(aged.label, "sync.stale")
  assert.deepEqual(
    aged.sources.map(({ state }) => state),
    ["stale", "stale", "stale"],
  )
})

test("the overall state follows the latest run while each source keeps its last success", () => {
  const running = "2026-01-01T23:30:00Z"
  const state = syncStateFor(
    meta({
      lastSync: { status: "success", startedAt: recent, finishedAt: running },
      sourceFetchedAt: {
        x: "2025-12-30T00:00:00Z",
        youtube: recent,
        bilibili: null,
      },
    }),
    null,
    now,
    t,
  )
  assert.equal(state.label, "sync.snapshot")
  assert.equal(state.tone, "leaf")
  assert.equal(state.runAt, running)
  assert.deepEqual(
    state.sources.map(({ source, state }) => [source, state]),
    [
      ["x", "stale"],
      ["youtube", "fresh"],
      ["bilibili", "waiting"],
    ],
  )
  // A run without a finish time is dated by its start.
  assert.equal(
    syncStateFor(
      meta({ lastSync: { status: "partial", startedAt: recent } }),
      null,
      now,
      t,
    ).runAt,
    recent,
  )
})

test("missing, invalid and future timestamps never look healthy", () => {
  for (const timestamp of [
    null,
    undefined,
    "invalid",
    "2026-01-03T00:00:00Z",
  ]) {
    assert.equal(freshnessAt(timestamp, now), "waiting")
  }
  const invalid = syncStateFor(
    meta({
      sourceFetchedAt: { x: "invalid", youtube: "2026-01-03T00:00:00Z" },
    }),
    null,
    now,
    t,
  )
  assert.deepEqual(
    invalid.sources.map(({ timestamp, state }) => [timestamp, state]),
    [
      [null, "waiting"],
      [null, "waiting"],
      [null, "waiting"],
    ],
  )
  const never = syncStateFor(meta({ lastSync: null }), null, now, t)
  assert.equal(never.label, "sync.waiting")
  assert.equal(never.tone, "muted")
  assert.equal(never.runAt, null)
  const future = syncStateFor(
    meta({
      lastSync: { status: "success", finishedAt: "2026-01-03T00:00:00Z" },
    }),
    null,
    now,
    t,
  )
  assert.equal(future.label, "sync.waiting")
  assert.equal(future.runAt, null)
  const empty = syncStateFor(meta({ sourceFetchedAt: undefined }), null, now, t)
  assert.deepEqual(
    empty.sources.map(({ source, timestamp }) => [source, timestamp]),
    [
      ["x", null],
      ["youtube", null],
      ["bilibili", null],
    ],
  )
})

test("a visitor clock slightly behind the server still shows fresh reads", () => {
  const ahead = new Date(now + 2 * 60 * 1000).toISOString()
  assert.equal(freshnessAt(ahead, now), "fresh")
  assert.equal(
    freshnessAt(new Date(now + CLOCK_SKEW_MS).toISOString(), now),
    "fresh",
  )
  assert.equal(
    freshnessAt(new Date(now + CLOCK_SKEW_MS + 1).toISOString(), now),
    "waiting",
  )
  const skewed = syncStateFor(
    meta({
      lastSync: { status: "success", finishedAt: ahead },
      sourceFetchedAt: { x: ahead, youtube: ahead, bilibili: ahead },
    }),
    null,
    now,
    t,
  )
  assert.equal(skewed.label, "sync.snapshot")
  assert.equal(skewed.runAt, ahead)
  assert.deepEqual(
    skewed.sources.map(({ timestamp }) => timestamp),
    [ahead, ahead, ahead],
  )
})

test("API and run failures take precedence while retaining run and source times", () => {
  const snapshot = meta({
    lastSync: { status: "failed", startedAt: recent, finishedAt: recent },
  })
  const failed = syncStateFor(snapshot, null, now, t)
  assert.equal(failed.label, "sync.needsReview")
  assert.equal(failed.tone, "danger")
  assert.equal(failed.runAt, recent)
  const offline = syncStateFor(snapshot, new Error("offline"), now, t)
  assert.equal(offline.label, "sync.apiUnavailable")
  assert.equal(offline.sources[0].timestamp, recent)
  const partial = syncStateFor(
    meta({ lastSync: { status: "partial", finishedAt: recent } }),
    null,
    now,
    t,
  )
  assert.equal(partial.label, "sync.partial")
  assert.equal(partial.tone, "honey")
})
