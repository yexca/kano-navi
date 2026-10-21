import assert from "node:assert/strict"
import test from "node:test"
import {
  pickCompanion,
  pickSpotlight,
  spotlightLink,
} from "../src/dashboard/spotlight.ts"

const now = Date.parse("2026-10-21T12:00:00Z")

const latestVideo = {
  id: "latest",
  source: "youtube",
  title: "Synthetic archive",
  publishedAt: "2026-10-20T11:00:00Z",
  scheduledAt: null,
  url: "https://www.youtube.com/watch?v=synthetic01",
  thumbnailUrl: "/media/synthetic-thumb",
  thumbnailStatus: "ready",
  kind: "LIVE ARCHIVE",
  isUpcoming: false,
}

const dateOnlyEvent = {
  id: "event-date-only",
  source: "x",
  title: "Synthetic collab",
  detail: null,
  startsOn: "2026-10-24",
  startsAt: null,
  endsAt: null,
  timePrecision: "unknown",
  eventType: "stream",
  url: "https://x.com/example/status/1",
  provenance: "automatic",
  manualLocked: false,
  isUpcoming: true,
  cancellationStatus: "none",
}

test("a schedule entry without artwork gets the compact ticket and the latest video", () => {
  const summary = { nextEvent: dateOnlyEvent, nextStream: null, latestVideo }
  const item = pickSpotlight(summary, now)
  assert.equal(item.layout, "compact")
  assert.equal(item.link, "announcement")
  assert.equal(item.dateOnly, "2026-10-24")
  assert.equal(item.time, null)
  const companion = pickCompanion(summary, item)
  assert.equal(companion.url, latestVideo.url)
  assert.equal(companion.thumbnailUrl, latestVideo.thumbnailUrl)
})

test("a reserved stream with artwork keeps the full ticket without a companion", () => {
  const nextStream = {
    ...latestVideo,
    id: "reserved",
    url: "https://www.youtube.com/watch?v=synthetic02",
    scheduledAt: "2026-10-22T11:00:00Z",
    publishedAt: null,
    kind: "UPCOMING LIVE",
    isUpcoming: true,
  }
  const summary = { nextEvent: dateOnlyEvent, nextStream, latestVideo }
  const item = pickSpotlight(summary, now)
  assert.equal(item.layout, "full")
  assert.equal(item.link, "watch")
  assert.equal(item.url, nextStream.url)
  assert.equal(pickCompanion(summary, item), null)
})

test("the latest-video fallback stays a full ticket", () => {
  const summary = { nextEvent: null, nextStream: null, latestVideo }
  const item = pickSpotlight(summary, now)
  assert.equal(item.mode, "latest")
  assert.equal(item.layout, "full")
  assert.equal(pickCompanion(summary, item), null)
})

test("a compact ticket has no companion when there is no other video", () => {
  const summary = {
    nextEvent: dateOnlyEvent,
    nextStream: null,
    latestVideo: null,
  }
  assert.equal(pickCompanion(summary, pickSpotlight(summary, now)), null)
  const same = { ...dateOnlyEvent, url: latestVideo.url }
  const repeat = { nextEvent: same, nextStream: null, latestVideo }
  assert.equal(pickCompanion(repeat, pickSpotlight(repeat, now)), null)
})

test("links name what they open", () => {
  assert.equal(spotlightLink("https://x.com/example/status/1"), "announcement")
  assert.equal(
    spotlightLink("https://m.youtube.com/watch?v=synthetic01"),
    "watch",
  )
  assert.equal(spotlightLink("https://youtu.be/synthetic01"), "watch")
  assert.equal(spotlightLink("https://x.com.example.test/status/1"), "open")
  assert.equal(spotlightLink("https://media.example.test/page"), "open")
  assert.equal(spotlightLink("not a url"), "open")
  assert.equal(spotlightLink(null), null)
})
