import assert from "node:assert/strict"
import test from "node:test"
import type { Dashboard } from "../src/types.ts"
import {
  pickCompanion,
  pickSpotlight,
  spotlightLink,
} from "../src/dashboard/spotlight.ts"
import {
  deriveDashboardAt,
  eventIsLive,
} from "../src/dashboard/derive-dashboard.ts"

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

const evening = Date.parse("2026-10-24T11:24:00Z")

function scheduled(overrides) {
  return {
    ...dateOnlyEvent,
    source: "youtube",
    timePrecision: "exact",
    startsOn: "2026-10-24",
    endsAt: null,
    ...overrides,
  }
}

test("only streams are presumed live without an explicit end", () => {
  const started = { startsAt: "2026-10-24T10:24:00Z" }
  const release = scheduled({ ...started, eventType: "release" })
  assert.equal(eventIsLive(release, evening), false)
  assert.equal(
    eventIsLive({ ...release, endsAt: "2026-10-24T11:30:00Z" }, evening),
    true,
  )
  assert.equal(
    eventIsLive({ ...release, endsAt: "2026-10-24T11:00:00Z" }, evening),
    false,
  )
  assert.equal(
    eventIsLive(scheduled({ ...started, eventType: "stream" }), evening),
    true,
  )
})

function snapshot(events, videos = []): Dashboard {
  return {
    profile: {
      displayName: "Synthetic",
      romanizedName: "Synthetic",
      bio: "",
      avatarUrl: null,
      bannerUrl: null,
      xUrl: "https://x.com/example",
      youtubeUrl: "https://www.youtube.com/@example",
    },
    summary: {
      nextEvent: null,
      nextStream: null,
      latestVideo: null,
      latestPost: null,
      counts: null,
    },
    posts: [],
    events,
    videos,
    focus: null,
    timeline: [],
    resources: [],
    assets: [],
    scheduleImages: [],
    meta: {
      fetchedAt: null,
      lastSync: null,
      postWindowDays: 3,
      xAccounts: [],
      featuredVideoId: null,
    },
  }
}

test("a started premiere does not hold the stage before a later stream", () => {
  const premiere = scheduled({
    id: "premiere",
    eventType: "release",
    startsAt: "2026-10-24T09:00:00Z",
  })
  const collab = scheduled({
    id: "collab",
    eventType: "stream",
    startsAt: "2026-10-24T12:00:00Z",
  })
  const derived = deriveDashboardAt(snapshot([premiere, collab]), evening)
  assert.equal(derived.summary.nextEvent.id, "collab")
  assert.equal(pickSpotlight(derived.summary, evening).event.id, "collab")
})

test("a presumed-live stream yields once a later reservation is near", () => {
  const earlier = scheduled({
    id: "earlier",
    eventType: "stream",
    startsAt: "2026-10-24T11:00:00Z",
  })
  const later = scheduled({
    id: "later",
    eventType: "stream",
    startsAt: "2026-10-24T12:00:00Z",
  })
  const at = (time) =>
    deriveDashboardAt(snapshot([earlier, later]), Date.parse(time)).summary
      .nextEvent.id
  assert.equal(at("2026-10-24T11:20:00Z"), "earlier")
  assert.equal(at("2026-10-24T11:31:00Z"), "later")
  assert.equal(at("2026-10-24T12:05:00Z"), "later")
  // An explicit end is not presumed, so it keeps the stage until it passes.
  const ending = { ...earlier, endsAt: "2026-10-24T11:50:00Z" }
  const summary = deriveDashboardAt(
    snapshot([ending, later]),
    Date.parse("2026-10-24T11:40:00Z"),
  ).summary
  assert.equal(summary.nextEvent.id, "earlier")
})

test("the spotlight lets a presumed-live video yield to a near event", () => {
  const nextStream = {
    ...latestVideo,
    id: "presumed",
    url: "https://www.youtube.com/watch?v=synthetic03",
    scheduledAt: "2026-10-24T10:00:00Z",
    publishedAt: null,
    isUpcoming: false,
    endsAt: null,
  }
  const nextEvent = scheduled({
    id: "near",
    eventType: "stream",
    startsAt: "2026-10-24T11:40:00Z",
    url: "https://x.com/example/status/2",
  })
  const item = pickSpotlight({ nextStream, nextEvent, latestVideo }, evening)
  assert.equal(item.event.id, "near")
})

test("an event for the same YouTube video is not a second candidate", () => {
  const nextStream = {
    ...latestVideo,
    id: "synthetic04",
    url: "https://www.youtube.com/watch?v=synthetic04",
    scheduledAt: "2026-10-24T12:00:00Z",
    publishedAt: null,
    kind: "UPCOMING LIVE",
    isUpcoming: true,
  }
  const nextEvent = scheduled({
    id: "youtube-synthetic04",
    eventType: "stream",
    startsAt: "2026-10-24T12:00:00Z",
    url: "https://www.youtube.com/live/synthetic04?si=share",
  })
  const item = pickSpotlight({ nextStream, nextEvent, latestVideo }, evening)
  assert.equal(item.video.id, "synthetic04")
  assert.equal(item.layout, "full")
})
