import assert from "node:assert/strict"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"
import Database from "better-sqlite3"

import {
  deleteManualEvent,
  getEvent,
  getDashboard,
  initializeDatabase,
  listAdminEvents,
  replaceAutomaticEventsForSource,
  updateManualEvent,
  upsertMediaAsset,
} from "./database.js"
import {
  mediaIdForSourceUrl,
  resolveMediaCachePath,
  writeMediaFileAtomic,
} from "./media-cache.js"

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
