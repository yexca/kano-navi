import assert from "node:assert/strict"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"
import Database from "better-sqlite3"

import {
  deleteManualEvent,
  applyLlmCancellationJudgements,
  createManualEvent,
  findCancellationTargets,
  bumpDashboardRevision,
  getAppSetting,
  getEvent,
  getFeaturedVideoId,
  getDashboard,
  getScheduleExtractionConfig,
  initializeDatabase,
  openDatabase,
  listAdminEvents,
  listAdminEventsPage,
  requestPostLlmReprocess,
  getPostLlmState,
  replaceAutomaticEventsForSource,
  seedDatabase,
  setFeaturedVideoId,
  updateManualEvent,
  updateManualCancellation,
  upsertLlmProvider,
  upsertMediaAsset,
  upsertAssets,
  upsertScheduleAssetReview,
  updateScheduleAssetManualReview,
  getScheduleAssetReview,
  upsertEvents,
  upsertPosts,
  upsertVideos,
} from "./database.js"
import {
  mediaIdForSourceUrl,
  resolveMediaCachePath,
  writeMediaFileAtomic,
} from "./media-cache.js"

test("legacy provider retry values migrate to the three-request policy", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-retry-"))
  const filename = path.join(directory, "kano.sqlite")
  let database
  try {
    database = initializeDatabase({ seed: false, filename })
    upsertLlmProvider(database, {
      id: "legacy-retry-provider",
      name: "Legacy retry provider",
      baseUrl: "https://provider.example.invalid/v1",
      model: "model",
      maxRetries: 2,
    })
    database
      .prepare("UPDATE llm_providers SET max_retries = 0 WHERE id = ?")
      .run("legacy-retry-provider")
    database.close()
    database = null
    database = openDatabase({ filename })
    assert.equal(
      database
        .prepare(
          "SELECT max_retries AS maxRetries FROM llm_providers WHERE id = ?",
        )
        .get("legacy-retry-provider").maxRetries,
      2,
    )
  } finally {
    database?.close()
    fs.rmSync(directory, { recursive: true, force: true })
  }
})

test("dashboard never exposes an unready remote URL", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    database.exec(`
      INSERT INTO posts (id, source, type, text, published_at, url, media_url)
      VALUES ('post-1', 'x', 'daily', 'hello', '2026-08-28T00:00:00Z', 'https://x.com/example/status/1', 'https://cdn.example.invalid/post.jpg')
    `)
    const post = getDashboard(database, {
      now: new Date("2026-08-28T01:00:00Z"),
    }).posts[0]
    assert.equal(post.mediaUrl, null)
    assert.equal(post.mediaStatus, "pending")
    assert.equal(post.mediaSourceUrl, "https://cdn.example.invalid/post.jpg")
  } finally {
    database.close()
  }
})

test("dashboard hides stale schedule assets sourced from ordinary stream posts", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    upsertPosts(database, [
      {
        id: "post-stream-preview",
        source: "x",
        type: "daily",
        label: "DAILY / 近况",
        text: "今日は22時からゲームをする予定だよ！",
        published_at: "2026-08-28T00:00:00Z",
        url: "https://x.com/example/status/stream-preview",
      },
      {
        id: "post-weekly-board",
        source: "x",
        type: "notice",
        label: "SCHEDULE / 日程",
        text: "今週のスケジュール",
        published_at: "2026-08-27T00:00:00Z",
        url: "https://x.com/example/status/weekly-board",
      },
    ])
    upsertAssets(database, [
      {
        id: "stale-schedule",
        kind: "schedule",
        url: "https://cdn.example.invalid/stream-preview.jpg",
        source_url: "https://x.com/example/status/stream-preview",
        week_start: "2026-08-24",
      },
      {
        id: "valid-schedule",
        kind: "schedule",
        url: "https://cdn.example.invalid/weekly-board.jpg",
        source_url: "https://x.com/example/status/weekly-board",
        week_start: "2026-08-24",
      },
    ])
    upsertScheduleAssetReview(database, {
      assetId: "valid-schedule",
      llmStatus: "schedule",
      llmConfidence: 0.99,
      llmReason: "visible weekly board",
    })
    const dashboard = getDashboard(database, {
      now: new Date("2026-08-28T01:00:00Z"),
    })
    assert.deepEqual(
      dashboard.assets.map((asset) => asset.id),
      ["valid-schedule"],
    )
    assert.deepEqual(
      dashboard.scheduleImages.map((asset) => asset.id),
      [],
    )

    // An image-only verdict cannot publish an asset whose source post is an
    // ordinary stream announcement; an operator's label can.
    upsertScheduleAssetReview(database, {
      assetId: "stale-schedule",
      llmStatus: "schedule",
      llmConfidence: 0.9,
    })
    const stale = getScheduleAssetReview(database, "stale-schedule")
    assert.equal(stale.sourceMatchesBoard, false)
    assert.equal(stale.approved, false)
    updateScheduleAssetManualReview(database, "stale-schedule", {
      status: "schedule",
      reason: "operator checked the image",
    })
    assert.deepEqual(
      getDashboard(database, { now: new Date("2026-08-28T01:00:00Z") })
        .assets.map((asset) => asset.id)
        .sort(),
      ["stale-schedule", "valid-schedule"],
    )
  } finally {
    database.close()
  }
})

test("a schedule asset ID reused for a new image loses its earlier review", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const asset = {
    id: "schedule-2026-08-24",
    kind: "schedule",
    url: "https://cdn.example.invalid/board-a.jpg",
    source_url: "https://x.com/example/status/board-a",
    week_start: "2026-08-24",
  }
  try {
    upsertAssets(database, [asset])
    upsertScheduleAssetReview(database, {
      assetId: asset.id,
      llmStatus: "schedule",
      llmConfidence: 0.98,
      llmModel: "vision-model",
    })
    updateScheduleAssetManualReview(database, asset.id, {
      status: "schedule",
      reason: "operator checked board-a",
    })

    upsertAssets(database, [{ ...asset, alt: "same image, new alt text" }])
    const unchanged = getScheduleAssetReview(database, asset.id)
    assert.equal(unchanged.llmStatus, "schedule")
    assert.equal(unchanged.manualStatus, "schedule")
    assert.equal(unchanged.approved, true)

    upsertAssets(database, [
      {
        ...asset,
        url: "https://cdn.example.invalid/stream-thumbnail.jpg",
        source_url: "https://x.com/example/status/stream-thumbnail",
      },
    ])
    const replaced = getScheduleAssetReview(database, asset.id)
    assert.equal(replaced.llmStatus, "pending")
    assert.equal(replaced.llmModel, null)
    assert.equal(replaced.manualStatus, "unreviewed")
    assert.equal(replaced.manualReason, null)
    assert.equal(replaced.approved, false)
  } finally {
    database.close()
  }
})

test("ready media keeps its opaque URL and rejects unsafe cache metadata", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let cachedFile = null
  try {
    const sourceUrl = "https://cdn.example.invalid/ready.jpg"
    const id = mediaIdForSourceUrl(sourceUrl)
    assert.throws(
      () =>
        upsertMediaAsset(database, {
          sourceUrl,
          status: "ready",
          cachePath: "../outside.jpg",
        }),
      /ready media asset requires a cache path|invalid media cache path/,
    )
    assert.throws(
      () => upsertMediaAsset(database, { sourceUrl, status: "unknown" }),
      /invalid media status/,
    )
    const written = await writeMediaFileAtomic({
      content: Buffer.from([0xff, 0xd8, 0xff, 0xd9]),
      extension: "jpg",
    })
    cachedFile = resolveMediaCachePath(written.relativePath)
    upsertMediaAsset(database, {
      id,
      sourceUrl,
      status: "ready",
      cachePath: written.relativePath,
      mimeType: "image/jpeg",
      sha256: written.sha256,
      byteSize: written.byteSize,
    })
    const row = database
      .prepare("SELECT id, status, cache_path FROM media_assets WHERE id = ?")
      .get(id)
    assert.deepEqual(row, {
      id,
      status: "ready",
      cache_path: written.relativePath,
    })
    assert.equal(
      resolveMediaCachePath(row.cache_path)?.includes("/data/cache/media/"),
      true,
    )

    database
      .prepare(
        `
      INSERT INTO posts (id, source, type, text, published_at, url, media_url)
      VALUES ('post-ready', 'x', 'daily', 'hello', '2026-08-28T00:00:00Z', 'https://x.com/example/status/2', ?)
    `,
      )
      .run(sourceUrl)
    const post = getDashboard(database, {
      now: new Date("2026-08-28T01:00:00Z"),
    }).posts[0]
    assert.equal(post.mediaUrl, `/media/${id}?v=${written.sha256}`)

    upsertMediaAsset(database, {
      sourceUrl,
      status: "failed",
      lastError: "temporary failure",
    })
    assert.equal(
      database.prepare("SELECT status FROM media_assets WHERE id = ?").get(id)
        .status,
      "ready",
    )
  } finally {
    database.close()
    if (cachedFile) fs.rmSync(cachedFile, { force: true })
  }
})

test("legacy event and focus tables migrate without clearing snapshots", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-migration-"))
  const filename = path.join(directory, "legacy.sqlite")
  const legacy = new Database(filename)
  legacy.exec(`
    CREATE TABLE events (
      id TEXT PRIMARY KEY,
      source TEXT NOT NULL,
      title TEXT NOT NULL,
      detail TEXT,
      starts_at TEXT NOT NULL,
      ends_at TEXT,
      status TEXT,
      event_type TEXT DEFAULT 'event',
      url TEXT,
      is_upcoming INTEGER DEFAULT 0,
      raw_json TEXT
    );
    CREATE INDEX events_starts_at_idx ON events (starts_at);
    INSERT INTO events (
      id, source, title, starts_at, status, event_type, url, is_upcoming
    ) VALUES
      ('legacy-x', 'x', 'X schedule', '2026-09-01T11:00:00Z', '予定', 'event', 'https://x.com/example/status/1', 1),
      ('legacy-youtube', 'youtube', 'Live', '2026-09-02T12:00:00Z', '予約', 'stream', 'https://www.youtube.com/watch?v=abcdefghijk', 1);
    CREATE TABLE focus (
      id INTEGER PRIMARY KEY,
      date_label TEXT,
      title TEXT NOT NULL,
      description TEXT,
      image_url TEXT,
      url TEXT,
      source_url TEXT,
      updated_at TEXT,
      raw_json TEXT
    );
    INSERT INTO focus (id, title, url)
    VALUES (1, 'Live', 'https://www.youtube.com/watch?v=abcdefghijk');
  `)
  legacy.close()

  const database = initializeDatabase({ seed: false, filename })
  try {
    const xEvent = getEvent(database, "legacy-x")
    const youtubeEvent = getEvent(database, "legacy-youtube")
    assert.equal(xEvent.startsOn, "2026-09-01")
    assert.equal(xEvent.provenance, "manual")
    assert.equal(xEvent.manualLocked, 1)
    assert.equal(youtubeEvent.provenance, "automatic")
    assert.equal(youtubeEvent.manualLocked, 0)
    assert.equal(
      database.prepare("SELECT video_id AS videoId FROM focus").get().videoId,
      "abcdefghijk",
    )
    assert.equal(
      database
        .prepare(
          "SELECT COUNT(*) AS count FROM event_sources WHERE event_id IN ('legacy-x', 'legacy-youtube')",
        )
        .get().count,
      2,
    )
  } finally {
    database.close()
    fs.rmSync(directory, { recursive: true, force: true })
  }
})

test("manual edits and tombstones cannot be overwritten by automatic extraction", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const automatic = {
    id: "x-schedule-20260903",
    source_key: "2026-09-03T20:00:first",
    title: "Automatic title",
    starts_on: "2026-09-03",
    starts_at: "2026-09-03T11:00:00.000Z",
    event_type: "stream",
    status: "予定",
    url: "https://x.com/example/status/2",
  }
  try {
    replaceAutomaticEventsForSource(database, {
      source: "x",
      sourceItemId: "post-2",
      events: [automatic],
    })
    assert.equal(getEvent(database, automatic.id).provenance, "automatic")

    updateManualEvent(database, automatic.id, {
      ...automatic,
      title: "Human title",
    })
    replaceAutomaticEventsForSource(database, {
      source: "x",
      sourceItemId: "post-2",
      events: [{ ...automatic, title: "Changed by model" }],
    })
    const confirmed = getEvent(database, automatic.id)
    assert.equal(confirmed.title, "Human title")
    assert.equal(confirmed.provenance, "manual")
    assert.equal(confirmed.manualLocked, 1)

    assert.equal(deleteManualEvent(database, automatic.id), true)
    replaceAutomaticEventsForSource(database, {
      source: "x",
      sourceItemId: "post-2",
      events: [{ ...automatic, title: "Resurrected by model" }],
    })
    const tombstone = listAdminEvents(database, { includeDeleted: true }).find(
      (event) => event.id === automatic.id,
    )
    assert.equal(tombstone.title, "Human title")
    assert.equal(tombstone.manualLocked, true)
    assert.ok(tombstone.deletedAt)
    assert.equal(
      getDashboard(database).events.some((event) => event.id === automatic.id),
      false,
    )
  } finally {
    database.close()
  }
})

test("LLM cancellation is an evidence overlay that applies to manual events", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    const event = updateManualEvent(database, "manual-cancel-test", {
      id: "manual-cancel-test",
      title: "夜配信",
      startsOn: "2026-09-04",
      startsAt: "2026-09-04T12:00:00.000Z",
      eventType: "stream",
    })
    assert.equal(event, null)
    const created = database
      .prepare(
        `INSERT INTO events (
          id, source, source_item_id, source_key, title, starts_on, starts_at,
          timezone, time_precision, provenance, manual_locked, created_at, updated_at
        ) VALUES ('manual-cancel-test', 'manual', 'manual-cancel-test',
          'manual-cancel-test', '夜配信', '2026-09-04',
          '2026-09-04T12:00:00.000Z', 'Asia/Tokyo', 'exact', 'manual', 1, ?, ?)`,
      )
      .run(new Date().toISOString(), new Date().toISOString())
    assert.equal(created.changes, 1)
    const judgement = applyLlmCancellationJudgements(database, {
      sourceItemId: "cancel-post",
      reason: "取消の可能性",
      evidence: "中止になりました",
      confidence: 0.88,
      targets: [{ eventId: "manual-cancel-test" }],
    })
    assert.deepEqual(judgement, { applied: 1, candidates: 1 })
    const reviewed = getEvent(database, "manual-cancel-test")
    assert.equal(reviewed.provenance, "manual")
    assert.equal(reviewed.manualLocked, 1)
    assert.equal(reviewed.cancellationStatus, "llm_suspected")
    assert.equal(reviewed.cancellationReason, "取消の可能性")
    assert.equal(reviewed.cancellationEvidence, "中止になりました")
    updateManualCancellation(database, "manual-cancel-test", {
      status: "manual_confirmed",
      reason: "人工复核确认",
    })
    assert.deepEqual(
      applyLlmCancellationJudgements(database, {
        sourceItemId: "later-post",
        reason: "再次判断",
        evidence: "再次提及中止",
        targets: [{ eventId: "manual-cancel-test" }],
      }),
      { applied: 0, candidates: 1 },
    )
    assert.equal(
      getEvent(database, "manual-cancel-test").cancellationStatus,
      "manual_confirmed",
    )

    const postId = "llm-reprocess-post"
    upsertPosts(database, [
      {
        id: postId,
        source: "x",
        text: "再確認",
        published_at: "2026-09-04T00:00:00.000Z",
        url: "https://x.com/example/status/llm-reprocess-post",
      },
    ])
    const queued = requestPostLlmReprocess(database, postId, "schedule_message")
    assert.equal(queued.status, "queued")
    assert.equal(queued.reprocessRequested, true)
    assert.equal(getPostLlmState(database, postId).route, "schedule_message")
  } finally {
    database.close()
  }
})

test("manual saves keep, dismiss, or confirm an LLM cancellation explicitly", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    // Seeded and YouTube rows keep a +09:00 offset while extraction emits UTC.
    const event = createManualEvent(database, {
      id: "offset-stream",
      title: "夜配信",
      startsOn: "2026-09-04",
      startsAt: "2026-09-04T21:00:00+09:00",
      eventType: "stream",
      cancellationStatus: "llm_suspected",
    })
    assert.equal(event.cancellationStatus, "none")
    const targets = findCancellationTargets(database, [
      {
        title: "夜配信",
        starts_on: "2026-09-04",
        starts_at: "2026-09-04T12:00:00.000Z",
      },
    ])
    assert.deepEqual(
      targets.map((target) => target.eventId),
      ["offset-stream"],
    )
    assert.deepEqual(
      findCancellationTargets(database, [
        {
          title: "夜配信",
          starts_on: "2026-09-04",
          starts_at: "2026-09-04T13:00:00.000Z",
        },
      ]),
      [],
    )
    applyLlmCancellationJudgements(database, {
      sourceItemId: "cancel-post",
      reason: "本人が中止と告知",
      evidence: "今日の配信はお休み",
      confidence: 0.86,
      targets,
    })

    const form = {
      title: "夜配信（変更）",
      startsOn: "2026-09-04",
      startsAt: "2026-09-04T12:00:00.000Z",
      eventType: "stream",
    }
    const kept = updateManualEvent(database, "offset-stream", {
      ...form,
      cancellationStatus: "llm_suspected",
    })
    assert.equal(kept.title, "夜配信（変更）")
    assert.equal(kept.cancellationStatus, "llm_suspected")
    assert.equal(kept.cancellationEvidence, "今日の配信はお休み")
    assert.equal(kept.cancellationSourceItemId, "cancel-post")
    assert.equal(kept.cancellationConfidence, 0.86)

    const confirmed = updateManualEvent(database, "offset-stream", {
      ...form,
      cancellationStatus: "manual_confirmed",
      cancellationReason: "告知を確認",
    })
    assert.equal(confirmed.cancellationStatus, "manual_confirmed")
    assert.equal(confirmed.cancellationSource, "manual")
    assert.equal(confirmed.cancellationEvidence, null)
    const reconfirmed = updateManualEvent(database, "offset-stream", {
      ...form,
      cancellationStatus: "manual_confirmed",
      cancellationReason: "告知を確認",
    })
    assert.equal(reconfirmed.cancellationAt, confirmed.cancellationAt)

    // `llm_suspected` cannot be invented by a save without an overlay.
    const dismissed = updateManualEvent(database, "offset-stream", {
      ...form,
      cancellationStatus: "none",
    })
    assert.equal(dismissed.cancellationStatus, "none")
    assert.equal(dismissed.cancellationReason, null)
    assert.equal(
      updateManualEvent(database, "offset-stream", {
        ...form,
        cancellationStatus: "llm_suspected",
      }).cancellationStatus,
      "none",
    )
  } finally {
    database.close()
  }
})

test("dashboard aggregates X account sources and keeps Featured selection separate from focus copy", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    upsertPosts(database, [
      {
        id: "post-main",
        source: "x",
        account_handle: "kano_2525",
        text: "main post",
        published_at: "2026-08-28T12:00:00.000Z",
        url: "https://x.com/kano_2525/status/post-main",
      },
      {
        id: "post-sub",
        source: "x",
        account_handle: "_Kanotic",
        text: "sub post",
        published_at: "2026-08-28T13:00:00.000Z",
        url: "https://x.com/_Kanotic/status/post-sub",
      },
    ])
    upsertVideos(database, [
      {
        id: "video-focus",
        source: "youtube",
        title: "Original focus video",
        published_at: "2026-08-20T00:00:00.000Z",
        url: "https://www.youtube.com/watch?v=video-focus",
      },
      {
        id: "video-featured",
        source: "youtube",
        title: "Manually selected video",
        published_at: "2026-08-28T14:00:00.000Z",
        url: "https://www.youtube.com/watch?v=video-featured",
      },
    ])
    database
      .prepare(
        `INSERT INTO focus (id, video_id, title, updated_at)
         VALUES (1, 'video-focus', 'Original focus copy', '2026-08-20T00:00:00.000Z')`,
      )
      .run()
    database
      .prepare(
        `INSERT INTO app_settings (key, value, updated_at)
         VALUES ('x_accounts', '["kano_2525","_Kanotic"]', '2026-08-20T00:00:00.000Z')`,
      )
      .run()

    assert.equal(
      setFeaturedVideoId(database, "video-featured"),
      "video-featured",
    )
    assert.equal(getAppSetting(database, "featured_video_id"), "video-featured")
    assert.equal(getFeaturedVideoId(database), "video-featured")

    const dashboard = getDashboard(database, {
      days: 3,
      now: new Date("2026-08-29T00:00:00.000Z"),
    })
    assert.deepEqual(
      dashboard.posts.map((post) => [
        post.id,
        post.accountHandle,
        post.accountUrl,
      ]),
      [
        ["post-sub", "_Kanotic", "https://x.com/_Kanotic"],
        ["post-main", "kano_2525", "https://x.com/kano_2525"],
      ],
    )
    assert.deepEqual(dashboard.meta.xAccounts, ["kano_2525", "_Kanotic"])
    assert.equal(dashboard.meta.featuredVideoId, "video-featured")
    assert.equal(dashboard.focus.videoId, "video-focus")
    assert.equal(
      dashboard.videos.find((video) => video.id === "video-featured").title,
      "Manually selected video",
    )
  } finally {
    database.close()
  }
})

test("dashboard summary points at the next event, stream, and last post", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    upsertPosts(database, [
      {
        id: "post-old",
        source: "x",
        account_handle: "kano_2525",
        text: "older than the window",
        published_at: "2026-08-01T12:00:00.000Z",
        url: "https://x.com/kano_2525/status/post-old",
      },
    ])
    upsertEvents(database, [
      {
        id: "event-past",
        source: "manual",
        title: "Past stream",
        starts_on: "2026-08-27",
        starts_at: "2026-08-27T11:00:00.000Z",
        status: "done",
      },
      {
        id: "event-cancelled",
        source: "manual",
        title: "Cancelled stream",
        starts_on: "2026-08-29",
        starts_at: "2026-08-29T11:00:00.000Z",
        status: "cancelled",
      },
      {
        id: "event-next",
        source: "manual",
        title: "Next stream",
        starts_on: "2026-08-30",
        starts_at: "2026-08-30T11:00:00.000Z",
        status: "scheduled",
      },
    ])
    upsertVideos(database, [
      {
        id: "video-archive",
        source: "youtube",
        title: "Archive",
        published_at: "2026-08-27T12:00:00.000Z",
        url: "https://www.youtube.com/watch?v=video-archive",
      },
      {
        id: "video-reserved",
        source: "youtube",
        title: "Reserved",
        scheduled_at: "2026-08-31T12:00:00.000Z",
        url: "https://www.youtube.com/watch?v=video-reserved",
      },
    ])

    const { posts, summary } = getDashboard(database, {
      days: 3,
      now: new Date("2026-08-29T00:00:00.000Z"),
    })
    assert.equal(posts.length, 0)
    assert.equal(summary.latestPost.id, "post-old")
    assert.equal(summary.latestPost.accountHandle, "kano_2525")
    assert.equal(summary.nextEvent.id, "event-next")
    assert.equal(summary.nextStream.id, "video-reserved")
    assert.equal(summary.latestVideo.id, "video-archive")
    assert.deepEqual(summary.counts, {
      posts: 0,
      events: 3,
      upcomingEvents: 1,
      videos: 2,
      resources: 0,
    })
  } finally {
    database.close()
  }
})

test("schedule environment settings seed only the initial database defaults", () => {
  const originalEnabled = process.env.SCHEDULE_EXTRACTION_ENABLED
  const originalKeywords = process.env.SCHEDULE_KEYWORDS
  const originalMessageEnabled = process.env.SCHEDULE_MESSAGE_ENABLED
  process.env.SCHEDULE_EXTRACTION_ENABLED = "0"
  process.env.SCHEDULE_KEYWORDS = "WEEKLY_BOARD,配信予定"
  process.env.SCHEDULE_MESSAGE_ENABLED = "1"
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    seedDatabase(database)
    assert.equal(getAppSetting(database, "schedule_extraction_enabled"), "0")
    assert.equal(getAppSetting(database, "schedule_message_enabled"), "1")
    assert.deepEqual(getScheduleExtractionConfig(database), {
      enabled: true,
      keywords: ["WEEKLY_BOARD", "配信予定"],
    })
  } finally {
    database.close()
    if (originalEnabled == null) delete process.env.SCHEDULE_EXTRACTION_ENABLED
    else process.env.SCHEDULE_EXTRACTION_ENABLED = originalEnabled
    if (originalKeywords == null) delete process.env.SCHEDULE_KEYWORDS
    else process.env.SCHEDULE_KEYWORDS = originalKeywords
    if (originalMessageEnabled == null)
      delete process.env.SCHEDULE_MESSAGE_ENABLED
    else process.env.SCHEDULE_MESSAGE_ENABLED = originalMessageEnabled
  }
})

test("admin schedule pages support literal search, filters, and stable bounds", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    upsertEvents(database, [
      {
        id: "page-event-1",
        source: "x",
        title: "alpha 100%",
        detail: "first",
        starts_on: "2026-09-01",
        provenance: "automatic",
      },
      {
        id: "page-event-2",
        source: "youtube",
        title: "beta 100x",
        detail: "second",
        starts_on: "2026-09-02",
        provenance: "automatic",
      },
      {
        id: "page-event-3",
        source: "manual",
        title: "gamma",
        detail: "third",
        starts_on: "2026-09-03",
        provenance: "manual",
        manual_locked: 1,
      },
      {
        id: "page-event-4",
        source: "x",
        title: "delta",
        detail: "fourth",
        starts_on: "2026-09-04",
        provenance: "automatic",
      },
      {
        id: "page-event-5",
        source: "x",
        title: "epsilon",
        detail: "fifth",
        starts_on: "2026-09-05",
        provenance: "automatic",
      },
    ])
    assert.equal(deleteManualEvent(database, "page-event-4"), true)

    const first = listAdminEventsPage(database, { page: 1, pageSize: 2 })
    assert.deepEqual(
      first.items.map((event) => event.id),
      ["page-event-5", "page-event-3"],
    )
    assert.equal(first.total, 4)
    assert.equal(first.totalPages, 2)
    assert.equal(first.hasPrevious, false)
    assert.equal(first.hasNext, true)

    const clamped = listAdminEventsPage(database, { page: 99, pageSize: 2 })
    assert.equal(clamped.page, 2)
    assert.equal(clamped.hasPrevious, true)
    assert.equal(clamped.hasNext, false)

    const literal = listAdminEventsPage(database, {
      search: "100%",
      includeDeleted: true,
    })
    assert.deepEqual(
      literal.items.map((event) => event.id),
      ["page-event-1"],
    )

    const filtered = listAdminEventsPage(database, {
      provenance: "automatic",
      source: "x",
      from: "2026-09-01",
      to: "2026-09-05",
      includeDeleted: false,
      pageSize: 100,
    })
    assert.deepEqual(
      filtered.items.map((event) => event.id),
      ["page-event-5", "page-event-1"],
    )

    const empty = listAdminEventsPage(database, { search: "no-match" })
    assert.equal(empty.total, 0)
    assert.equal(empty.totalPages, 1)
    assert.deepEqual(empty.items, [])

    const revision = bumpDashboardRevision(database)
    assert.equal(bumpDashboardRevision(database), revision + 1)
  } finally {
    database.close()
  }
})
