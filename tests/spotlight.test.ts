import assert from "node:assert/strict"
import test from "node:test"
import type { Dashboard, ScheduleEvent, Video } from "../src/types.ts"
import {
  pickCompanion,
  pickSpotlight,
  spotlightIsLive,
  spotlightLink,
} from "../src/dashboard/spotlight.ts"
import {
  deriveDashboardAt,
  videoIsInFocus,
} from "../src/dashboard/derive-dashboard.ts"
import { eventIsLive, eventStatus } from "../src/dashboard/format.ts"

const now = Date.parse("2026-10-21T12:00:00Z")

const latestVideo: Video = {
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

const dateOnlyEvent: ScheduleEvent = {
  id: "event-date-only",
  source: "x",
  title: "Synthetic collab",
  detail: null,
  startsOn: "2026-10-24",
  startsAt: null,
  endsAt: null,
  timezone: "Asia/Tokyo",
  timePrecision: "unknown",
  status: null,
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
  assert.ok(item)
  assert.equal(item?.layout, "compact")
  assert.equal(item.link, "announcement")
  assert.equal(item.dateOnly, "2026-10-24")
  assert.equal(item.time, null)
  const companion = pickCompanion(summary, item)
  assert.ok(companion)
  assert.equal(companion.url, latestVideo.url)
  assert.equal(companion.thumbnailUrl, latestVideo.thumbnailUrl)
})

test("a reserved stream with artwork keeps the full ticket without a companion", () => {
  const nextStream: Video = {
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
  assert.ok(item)
  assert.equal(item?.layout, "full")
  assert.equal(item.link, "watch")
  assert.equal(item.url, nextStream.url)
  assert.equal(pickCompanion(summary, item), null)
})

test("the latest-video fallback stays a full ticket", () => {
  const summary = { nextEvent: null, nextStream: null, latestVideo }
  const item = pickSpotlight(summary, now)
  assert.ok(item)
  assert.equal(item.mode, "latest")
  assert.equal(item?.layout, "full")
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

function scheduled(overrides: Partial<ScheduleEvent>): ScheduleEvent {
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

function snapshot(events: ScheduleEvent[], videos: Video[] = []): Dashboard {
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
  assert.equal(derived.summary.nextEvent?.id, "collab")
  assert.equal(pickSpotlight(derived.summary, evening)?.event?.id, "collab")
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
  const at = (time: string) =>
    deriveDashboardAt(snapshot([earlier, later]), Date.parse(time)).summary
      .nextEvent?.id
  assert.equal(at("2026-10-24T11:20:00Z"), "earlier")
  assert.equal(at("2026-10-24T11:31:00Z"), "later")
  assert.equal(at("2026-10-24T12:05:00Z"), "later")
  // An explicit end is not presumed, so it keeps the stage until it passes.
  const ending = { ...earlier, endsAt: "2026-10-24T11:50:00Z" }
  const summary = deriveDashboardAt(
    snapshot([ending, later]),
    Date.parse("2026-10-24T11:40:00Z"),
  ).summary
  assert.equal(summary.nextEvent?.id, "earlier")
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
  assert.equal(item?.event?.id, "near")
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
  assert.equal(item?.video?.id, "synthetic04")
  assert.equal(item?.layout, "full")
})

const longAgo = "2026-10-24T06:00:00Z"

test("a confirmed live stream stays on air past the start-time grace", () => {
  const event = scheduled({ eventType: "stream", startsAt: longAgo })
  assert.equal(eventIsLive(event, evening), false)
  assert.equal(eventIsLive({ ...event, liveStatus: "live" }, evening), true)
  // A reported end still closes it.
  assert.equal(
    eventIsLive(
      { ...event, liveStatus: "live", endsAt: "2026-10-24T11:00:00Z" },
      evening,
    ),
    false,
  )
  const video = {
    ...latestVideo,
    id: "long-live",
    scheduledAt: longAgo,
    isUpcoming: false,
    endsAt: null,
  }
  assert.equal(videoIsInFocus(video, evening), false)
  assert.equal(videoIsInFocus({ ...video, liveStatus: "live" }, evening), true)
  const item = pickSpotlight(
    {
      nextStream: { ...video, liveStatus: "live" },
      nextEvent: null,
      latestVideo,
    },
    evening,
  )
  assert.equal(item?.video?.id, "long-live")
  assert.equal(spotlightIsLive(item, evening), true)
})

test("a confirmed end or waiting room is never on air", () => {
  const started = scheduled({
    eventType: "stream",
    startsAt: "2026-10-24T11:00:00Z",
  })
  // Within the grace a stream is presumed live until the probe says otherwise.
  assert.equal(eventIsLive(started, evening), true)
  assert.equal(eventIsLive({ ...started, liveStatus: "ended" }, evening), false)
  assert.equal(
    eventIsLive({ ...started, liveStatus: "upcoming" }, evening),
    false,
  )
  const video = {
    ...latestVideo,
    id: "late",
    scheduledAt: "2026-10-24T11:00:00Z",
    isUpcoming: false,
    endsAt: null,
  }
  assert.equal(
    videoIsInFocus({ ...video, liveStatus: "ended" }, evening),
    false,
  )
  // A late waiting room keeps its ticket but does not claim to be live.
  const waiting = { ...video, liveStatus: "upcoming" as const }
  assert.equal(videoIsInFocus(waiting, evening), true)
  const item = pickSpotlight(
    { nextStream: waiting, nextEvent: null, latestVideo },
    evening,
  )
  assert.equal(item?.video?.id, "late")
  assert.equal(spotlightIsLive(item, evening), false)
  // Without a probe result the start time is the fallback.
  assert.equal(
    spotlightIsLive(
      pickSpotlight(
        { nextStream: video, nextEvent: null, latestVideo },
        evening,
      ),
      evening,
    ),
    true,
  )
  const derived = deriveDashboardAt(snapshot([], [waiting]), evening)
  assert.equal(derived.videos[0].isUpcoming, true)
  assert.equal(derived.summary.latestVideo, null)
})

test("a confirmed live stream keeps the stage before a near reservation", () => {
  const live = scheduled({
    id: "live",
    eventType: "stream",
    startsAt: "2026-10-24T10:00:00Z",
  })
  const later = scheduled({
    id: "later",
    eventType: "stream",
    startsAt: "2026-10-24T11:40:00Z",
  })
  const lead = (events: ScheduleEvent[]) =>
    deriveDashboardAt(snapshot(events), evening).summary.nextEvent?.id
  assert.equal(lead([live, later]), "later")
  assert.equal(lead([{ ...live, liveStatus: "live" }, later]), "live")
})

test("the schedule reads On air exactly while the stage does", () => {
  const stream = scheduled({
    id: "youtube-synthetic05",
    eventType: "stream",
    startsAt: "2026-10-24T09:00:00Z",
    url: "https://www.youtube.com/watch?v=synthetic05",
  })
  const premiere = scheduled({
    id: "youtube-synthetic06",
    eventType: "release",
    startsAt: "2026-10-24T11:00:00Z",
    endsAt: "2026-10-24T11:30:00Z",
    url: "https://www.youtube.com/watch?v=synthetic06",
  })
  const onStage = (event: ScheduleEvent, at: number) => {
    const { summary } = deriveDashboardAt(snapshot([event]), at)
    return spotlightIsLive(pickSpotlight(summary, at), at)
  }
  const cases: { event: ScheduleEvent; time: string; status: string }[] = [
    { event: stream, time: "2026-10-24T08:59:00Z", status: "upcoming" },
    // Presumed live for the start-time grace, then ended.
    { event: stream, time: "2026-10-24T11:24:00Z", status: "live" },
    { event: stream, time: "2026-10-24T12:00:00Z", status: "done" },
    // A probe result decides either way.
    {
      event: { ...stream, liveStatus: "ended" },
      time: "2026-10-24T11:24:00Z",
      status: "done",
    },
    {
      event: { ...stream, liveStatus: "live" },
      time: "2026-10-24T13:00:00Z",
      status: "live",
    },
    // A late waiting room has not started, within the grace or after it.
    {
      event: { ...stream, liveStatus: "upcoming" },
      time: "2026-10-24T11:24:00Z",
      status: "upcoming",
    },
    {
      event: { ...stream, liveStatus: "upcoming" },
      time: "2026-10-24T13:00:00Z",
      status: "upcoming",
    },
    // A premiere is live only until its explicit end.
    { event: premiere, time: "2026-10-24T11:24:00Z", status: "live" },
    { event: premiere, time: "2026-10-24T11:30:00Z", status: "done" },
    // A cancellation is never on air.
    {
      event: { ...stream, cancellationStatus: "manual_confirmed" },
      time: "2026-10-24T11:24:00Z",
      status: "cancelled",
    },
    {
      event: { ...stream, cancellationStatus: "llm_suspected" },
      time: "2026-10-24T11:24:00Z",
      status: "cancellation_review",
    },
  ]
  for (const { event, time, status } of cases) {
    const at = Date.parse(time)
    const label = `${event.id} ${event.liveStatus ?? ""} at ${time}`
    assert.equal(eventStatus(event, at), status, label)
    assert.equal(onStage(event, at), status === "live", label)
  }
})

test("the latest-video fallback skips Shorts unless the operator featured one", () => {
  const short = (id: string, publishedAt: string): Video => ({
    ...latestVideo,
    id,
    url: `https://www.youtube.com/watch?v=${id}`,
    kind: "VIDEO",
    isShort: true,
    publishedAt,
  })
  const newer = short("shortnew001", "2026-10-21T10:00:00Z")
  const older = short("shortold001", "2026-10-19T10:00:00Z")
  // An unknown format stays eligible.
  const archive: Video = { ...latestVideo, isShort: null }
  const featuring = (videos: Video[], featuredVideoId: string | null) => {
    const value = snapshot([dateOnlyEvent], videos)
    value.meta.featuredVideoId = featuredVideoId
    return value
  }

  // A newer Short gives way to the next upload, on the full ticket ...
  const { summary } = deriveDashboardAt(
    featuring([newer, archive, older], null),
    now,
  )
  assert.equal(summary.latestVideo?.id, "latest")
  const full = pickSpotlight({ ...summary, nextEvent: null }, now)
  assert.equal(full?.mode, "latest")
  assert.equal(full?.video?.id, "latest")
  // ... and as the companion of a compact ticket.
  const compact = pickSpotlight(summary, now)
  assert.equal(compact?.layout, "compact")
  assert.equal(pickCompanion(summary, compact)?.video.id, "latest")

  // The operator's featured Short still shows.
  const featured = deriveDashboardAt(
    featuring([newer, archive], newer.id),
    now,
  ).summary
  assert.equal(
    pickSpotlight({ ...featured, nextEvent: null }, now)?.video?.id,
    newer.id,
  )
  assert.equal(
    pickCompanion(featured, pickSpotlight(featured, now))?.video.id,
    newer.id,
  )

  // When every recent upload is a Short, there is no latest video to show.
  const shortsOnly = deriveDashboardAt(
    featuring([newer, older], null),
    now,
  ).summary
  assert.equal(shortsOnly.latestVideo, null)
  assert.equal(pickSpotlight({ ...shortsOnly, nextEvent: null }, now), null)
  assert.equal(pickCompanion(shortsOnly, pickSpotlight(shortsOnly, now)), null)
})
