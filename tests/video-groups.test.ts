import assert from "node:assert/strict"
import test from "node:test"
import type { Video } from "../src/types.ts"
import {
  groupVideos,
  splitVideos,
  videoIsShort,
  videoIsStream,
  videoMatchesFilter,
  videoPhase,
} from "../src/dashboard/video-groups.ts"
import {
  durationAttribute,
  formatDuration,
  videoKindLabel,
} from "../src/dashboard/format.ts"

const now = Date.parse("2026-10-25T12:00:00Z")
const hour = 3600 * 1000

function video(id: string, fields: Partial<Video> = {}): Video {
  return {
    id,
    source: "youtube",
    title: `Synthetic ${id}`,
    publishedAt: null,
    scheduledAt: null,
    url: `https://www.youtube.com/watch?v=${id}`,
    thumbnailUrl: `/media/${id}`,
    thumbnailStatus: "ready",
    kind: "LIVE ARCHIVE",
    isUpcoming: false,
    ...fields,
  }
}

const at = (offset: number) => new Date(now + offset).toISOString()

test("broadcasts are streams and other uploads are works", () => {
  for (const kind of ["LIVE ARCHIVE", "UPCOMING LIVE", "gameplay", "TALK"])
    assert.equal(videoIsStream(video("s", { kind })), true, kind)
  for (const kind of ["MUSIC", "VIDEO", "", "Vlog"])
    assert.equal(videoIsStream(video("w", { kind })), false, kind)
  assert.equal(videoMatchesFilter(video("s"), "streams"), true)
  assert.equal(videoMatchesFilter(video("s"), "works"), false)
  assert.equal(videoMatchesFilter(video("w", { kind: "MUSIC" }), "all"), true)
})

test("a reservation past its start is not an archive until it is over", () => {
  const reserved = { kind: "UPCOMING LIVE", isUpcoming: true }
  assert.equal(
    videoPhase(video("a", { ...reserved, scheduledAt: at(hour) }), now),
    "upcoming",
  )
  // Started without a confirmed state: presumed on air for the grace.
  assert.equal(
    videoPhase(video("b", { ...reserved, scheduledAt: at(-hour) }), now),
    "started",
  )
  assert.equal(
    videoPhase(video("c", { ...reserved, scheduledAt: at(-4 * hour) }), now),
    "past",
  )
  // A confirmed state decides before the start time.
  assert.equal(
    videoPhase(
      video("d", { scheduledAt: at(-4 * hour), liveStatus: "live" }),
      now,
    ),
    "live",
  )
  assert.equal(
    videoPhase(
      video("e", {
        scheduledAt: at(-hour),
        endsAt: at(-1000),
        liveStatus: "live",
      }),
      now,
    ),
    "past",
  )
  assert.equal(
    videoPhase(
      video("f", { scheduledAt: at(-hour), liveStatus: "upcoming" }),
      now,
    ),
    "upcoming",
  )
  assert.equal(
    videoPhase(
      video("g", { scheduledAt: at(-hour), liveStatus: "ended" }),
      now,
    ),
    "past",
  )
  // A premiere without an end is over once it starts.
  assert.equal(
    videoPhase(video("h", { kind: "MUSIC", scheduledAt: at(-hour) }), now),
    "past",
  )
  assert.equal(
    videoPhase(
      video("i", { ...reserved, scheduledAt: at(hour), isCancelled: true }),
      now,
    ),
    "cancelled",
  )
})

test("the newest cover leads and every video is listed once", () => {
  const cover = video("cover", {
    kind: "MUSIC",
    scheduledAt: at(-30 * hour),
    durationSeconds: 289,
  })
  const clip = video("clip", { kind: "VIDEO", publishedAt: at(-10 * hour) })
  const older = video("older", { kind: "MUSIC", publishedAt: at(-90 * hour) })
  const archiveNew = video("archive-new", { scheduledAt: at(-20 * hour) })
  const archiveOld = video("archive-old", { scheduledAt: at(-60 * hour) })
  const later = video("later", {
    kind: "UPCOMING LIVE",
    isUpcoming: true,
    scheduledAt: at(48 * hour),
  })
  const sooner = video("sooner", {
    kind: "UPCOMING LIVE",
    isUpcoming: true,
    scheduledAt: at(2 * hour),
  })
  const onAir = video("on-air", {
    scheduledAt: at(-hour),
    liveStatus: "live",
  })
  const cancelledPast = video("cancelled-past", {
    scheduledAt: at(-2 * hour),
    isCancelled: true,
  })
  const cancelledNext = video("cancelled-next", {
    kind: "UPCOMING LIVE",
    isUpcoming: true,
    scheduledAt: at(5 * hour),
    isCancelled: true,
  })
  const videos = [
    archiveOld,
    clip,
    later,
    cover,
    cancelledPast,
    older,
    onAir,
    archiveNew,
    sooner,
    cancelledNext,
  ]

  const groups = groupVideos(videos, null, now)
  assert.equal(groups.lead?.id, "cover")
  assert.equal(groups.leadIsFeatured, false)
  assert.deepEqual(
    groups.reservations.map((item) => item.id),
    ["on-air", "sooner", "cancelled-next", "later"],
  )
  assert.deepEqual(
    groups.works.map((item) => item.id),
    ["clip", "older"],
  )
  // A cancelled reservation stays listed until its start, then drops out.
  assert.deepEqual(
    groups.archives.map((item) => item.id),
    ["archive-new", "archive-old"],
  )

  // The operator's featured video leads instead and the cover stays a work.
  const featured = groupVideos(videos, "archive-old", now)
  assert.equal(featured.lead?.id, "archive-old")
  assert.equal(featured.leadIsFeatured, true)
  assert.deepEqual(
    featured.works.map((item) => item.id),
    ["clip", "cover", "older"],
  )
  assert.deepEqual(
    featured.archives.map((item) => item.id),
    ["archive-new"],
  )

  // Without a cover or premiere the newest upload leads; without works none.
  assert.equal(groupVideos([clip, archiveNew], null, now).lead?.id, "clip")
  assert.equal(groupVideos([archiveNew, sooner], null, now).lead, null)
  assert.deepEqual(
    splitVideos([cover, clip], now).works.map((item) => item.id),
    ["clip", "cover"],
  )
})

test("lengths read like the platforms show them", () => {
  assert.equal(formatDuration(289), "4:49")
  assert.equal(formatDuration(33), "0:33")
  assert.equal(formatDuration(25000), "6:56:40")
  assert.equal(formatDuration(3600), "1:00:00")
  for (const unknown of [null, undefined, 0, -5, Number.NaN])
    assert.equal(formatDuration(unknown), "")
  assert.equal(durationAttribute(25000), "PT6H56M40S")
  assert.equal(durationAttribute(33), "PT33S")
})

test("a reservation past its start is labelled a stream, not an archive", () => {
  const t = (key: string) => key
  const reserved = video("r", { kind: "UPCOMING LIVE", isUpcoming: true })
  assert.equal(videoKindLabel(reserved, t), "media.kind.upcoming")
  assert.equal(
    videoKindLabel({ ...reserved, isUpcoming: false }, t),
    "event.type.stream",
  )
  assert.equal(videoKindLabel(video("a"), t), "media.kind.liveArchive")
  assert.equal(
    videoKindLabel(video("v", { kind: "VIDEO" }), t),
    "media.kind.video",
  )
})

test("Shorts are their own group, never a work, an archive or the lead", () => {
  // The title classifier calls this Short a stream; its format decides.
  const streamShort = video("short-stream", {
    kind: "LIVE ARCHIVE",
    isShort: true,
    publishedAt: at(-2 * hour),
  })
  const songShort = video("short-song", {
    kind: "MUSIC",
    isShort: true,
    publishedAt: at(-hour),
  })
  // An unknown format is an ordinary upload, however brief.
  const brief = video("brief", {
    kind: "VIDEO",
    isShort: null,
    publishedAt: at(-3 * hour),
    durationSeconds: 30,
  })
  const regular = video("regular", {
    kind: "VIDEO",
    isShort: false,
    publishedAt: at(-5 * hour),
  })
  const archive = video("archive", {
    isShort: false,
    scheduledAt: at(-20 * hour),
  })
  // A Short that is still coming up stays with the reservations.
  const reserved = video("short-reserved", {
    kind: "UPCOMING LIVE",
    isShort: true,
    isUpcoming: true,
    scheduledAt: at(hour),
  })
  const videos = [streamShort, regular, songShort, archive, brief, reserved]

  const groups = groupVideos(videos, null, now)
  // The newest cover is a Short, so the newest ordinary upload leads.
  assert.equal(groups.lead?.id, "brief")
  assert.deepEqual(
    groups.shorts.map((item) => item.id),
    ["short-song", "short-stream"],
  )
  assert.deepEqual(
    groups.works.map((item) => item.id),
    ["regular"],
  )
  assert.deepEqual(
    groups.archives.map((item) => item.id),
    ["archive"],
  )
  assert.deepEqual(
    groups.reservations.map((item) => item.id),
    ["short-reserved"],
  )
  assert.equal(groupVideos([songShort, streamShort], null, now).lead, null)

  // The operator's featured Short still leads.
  const featured = groupVideos(videos, "short-song", now)
  assert.equal(featured.lead?.id, "short-song")
  assert.deepEqual(
    featured.shorts.map((item) => item.id),
    ["short-stream"],
  )

  assert.equal(videoIsShort(streamShort), true)
  assert.equal(videoIsShort(brief), false)
  assert.equal(videoIsShort(video("unset")), false)
  for (const [item, filter, expected] of [
    [streamShort, "shorts", true],
    [streamShort, "streams", false],
    [songShort, "works", false],
    [streamShort, "all", true],
    [archive, "shorts", false],
    [archive, "streams", true],
    [brief, "works", true],
    [brief, "shorts", false],
  ] as const)
    assert.equal(
      videoMatchesFilter(item, filter),
      expected,
      `${item.id}/${filter}`,
    )

  const t = (key: string) => key
  assert.equal(videoKindLabel(streamShort, t), "media.kind.short")
  assert.equal(videoKindLabel(brief, t), "media.kind.video")
})
