import assert from "node:assert/strict"
import test from "node:test"
import type { Dashboard } from "../src/types.ts"
import {
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
    lastSync: { status: "success" },
    sourceFetchedAt: { x: recent, youtube: recent },
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
  assert.equal(syncStateFor(snapshot, null, now, t).label, "sync.snapshot")
  assert.equal(
    syncStateFor(snapshot, null, now + STALE_AFTER_MS, t).label,
    "sync.stale",
  )
  assert.equal(snapshot.sourceFetchedAt?.x, recent)
})

test("a recent overall sync cannot hide a stale or never-synced source", () => {
  const stale = syncStateFor(
    meta({ sourceFetchedAt: { x: "2025-12-30T00:00:00Z", youtube: recent } }),
    null,
    now,
    t,
  )
  assert.equal(stale.label, "sync.stale")
  assert.equal(stale.tone, "honey")
  assert.deepEqual(
    stale.sources.map(({ state }) => state),
    ["stale", "fresh"],
  )
  assert.equal(
    syncStateFor(
      meta({ sourceFetchedAt: { x: null, youtube: recent } }),
      null,
      now,
      t,
    ).label,
    "sync.partial",
  )
  const empty = syncStateFor(
    meta({ sourceFetchedAt: { x: null, youtube: null } }),
    null,
    now,
    t,
  )
  assert.equal(empty.label, "sync.waiting")
  assert.equal(empty.tone, "muted")
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
  assert.equal(invalid.label, "sync.waiting")
  assert.deepEqual(
    invalid.sources.map(({ timestamp }) => timestamp),
    [null, null],
  )
  assert.equal(
    syncStateFor(meta({ fetchedAt: null }), null, now, t).label,
    "sync.waiting",
  )
})

test("API and sync failures take precedence while retaining source times", () => {
  const snapshot = meta({ lastSync: { status: "failed" } })
  const failed = syncStateFor(snapshot, null, now, t)
  assert.equal(failed.label, "sync.needsReview")
  assert.equal(failed.tone, "danger")
  const offline = syncStateFor(snapshot, new Error("offline"), now, t)
  assert.equal(offline.label, "sync.apiUnavailable")
  assert.equal(offline.sources[0].timestamp, recent)
  assert.equal(
    syncStateFor(meta({ lastSync: { status: "partial" } }), null, now, t).label,
    "sync.partial",
  )
})

test("older snapshots fall back to their single timestamp without inventing source reads", () => {
  const snapshot = meta({ sourceFetchedAt: undefined })
  const fresh = syncStateFor(snapshot, null, now, t)
  assert.equal(fresh.label, "sync.snapshot")
  assert.deepEqual(fresh.sources, [])
  assert.equal(
    syncStateFor(snapshot, null, now + STALE_AFTER_MS, t).label,
    "sync.stale",
  )
})
