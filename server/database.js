import crypto from "node:crypto"
import fs from "node:fs"
import path from "node:path"
import Database from "better-sqlite3"
import { fileURLToPath } from "node:url"
import { seedData } from "./seed-data.js"
import {
  MEDIA_STATUS,
  cacheRelativePathForHash,
  ensureMediaCacheDirectories,
  extensionForMimeType,
  isSafeContentHash,
  isSafeMediaId,
  mediaIdForSourceUrl,
  normalizeMediaMimeType,
  normalizeSourceUrl,
  publicAssetExists,
  publicMediaUrl,
  resolveMediaCachePath,
} from "./media-cache.js"

const moduleDir = path.dirname(fileURLToPath(import.meta.url))
const projectDir = path.resolve(moduleDir, "..")

export const dataDirectory = path.join(projectDir, "data")
export const databasePath = path.join(dataDirectory, "kano.sqlite")

const schema = `
  PRAGMA foreign_keys = ON;

  CREATE TABLE IF NOT EXISTS profiles (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    romanized_name TEXT,
    bio TEXT,
    avatar_url TEXT,
    banner_url TEXT,
    x_url TEXT,
    youtube_url TEXT,
    updated_at TEXT,
    raw_json TEXT
  );

  CREATE TABLE IF NOT EXISTS posts (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    account_handle TEXT,
    type TEXT NOT NULL DEFAULT 'daily',
    label TEXT,
    text TEXT NOT NULL,
    published_at TEXT NOT NULL,
    url TEXT NOT NULL,
    likes INTEGER DEFAULT 0,
    reposts INTEGER DEFAULT 0,
    replies INTEGER DEFAULT 0,
    media_url TEXT,
    media_alt TEXT,
    raw_json TEXT
  );

  CREATE INDEX IF NOT EXISTS posts_published_at_idx ON posts (published_at DESC);

  CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    source_item_id TEXT,
    source_key TEXT,
    title TEXT NOT NULL,
    detail TEXT,
    starts_on TEXT NOT NULL,
    starts_at TEXT,
    ends_at TEXT,
    timezone TEXT NOT NULL DEFAULT 'Asia/Tokyo',
    time_precision TEXT NOT NULL DEFAULT 'exact',
    status TEXT,
    event_type TEXT DEFAULT 'event',
    url TEXT,
    is_upcoming INTEGER DEFAULT 0,
    provenance TEXT NOT NULL DEFAULT 'automatic',
    manual_locked INTEGER NOT NULL DEFAULT 0,
    confidence REAL,
    extraction_id INTEGER,
    deleted_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    raw_json TEXT
  );

  CREATE TABLE IF NOT EXISTS event_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    source_item_id TEXT NOT NULL,
    source_key TEXT NOT NULL,
    url TEXT,
    raw_json TEXT,
    deleted_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );

  CREATE UNIQUE INDEX IF NOT EXISTS event_sources_identity_idx
    ON event_sources (source, source_item_id, source_key);
  CREATE INDEX IF NOT EXISTS event_sources_event_idx ON event_sources (event_id);

  CREATE TABLE IF NOT EXISTS videos (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    title TEXT NOT NULL,
    published_at TEXT,
    scheduled_at TEXT,
    url TEXT NOT NULL,
    thumbnail_url TEXT,
    kind TEXT,
    is_upcoming INTEGER DEFAULT 0,
    raw_json TEXT
  );

  CREATE INDEX IF NOT EXISTS videos_date_idx ON videos (COALESCE(scheduled_at, published_at) DESC);

  CREATE TABLE IF NOT EXISTS focus (
    id INTEGER PRIMARY KEY,
    video_id TEXT,
    date_label TEXT,
    title TEXT NOT NULL,
    description TEXT,
    image_url TEXT,
    url TEXT,
    source_url TEXT,
    updated_at TEXT,
    raw_json TEXT
  );

  CREATE TABLE IF NOT EXISTS timeline (
    id INTEGER PRIMARY KEY,
    year TEXT NOT NULL,
    title TEXT NOT NULL,
    detail TEXT,
    sort_order INTEGER DEFAULT 0,
    raw_json TEXT
  );

  CREATE TABLE IF NOT EXISTS resources (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    detail TEXT,
    icon TEXT,
    tone TEXT,
    url TEXT NOT NULL,
    sort_order INTEGER DEFAULT 0,
    raw_json TEXT
  );

  CREATE TABLE IF NOT EXISTS assets (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    url TEXT NOT NULL,
    source_url TEXT,
    alt TEXT,
    updated_at TEXT,
    raw_json TEXT
  );

  CREATE TABLE IF NOT EXISTS media_assets (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    source_url TEXT NOT NULL,
    cache_path TEXT,
    mime_type TEXT,
    extension TEXT,
    byte_size INTEGER,
    sha256 TEXT,
    width INTEGER,
    height INTEGER,
    status TEXT NOT NULL DEFAULT 'pending',
    fetched_at TEXT,
    last_checked_at TEXT,
    last_seen_at TEXT,
    etag TEXT,
    last_modified TEXT,
    last_error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    raw_json TEXT
  );

  CREATE UNIQUE INDEX IF NOT EXISTS media_assets_source_url_idx ON media_assets (source_url);
  CREATE INDEX IF NOT EXISTS media_assets_status_idx ON media_assets (status, updated_at DESC);

  CREATE TABLE IF NOT EXISTS media_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_id TEXT NOT NULL REFERENCES media_assets (id) ON DELETE CASCADE,
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    role TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    alt TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );

  CREATE UNIQUE INDEX IF NOT EXISTS media_links_owner_position_idx ON media_links (owner_type, owner_id, role, position);
  CREATE INDEX IF NOT EXISTS media_links_media_idx ON media_links (media_id);

  CREATE TABLE IF NOT EXISTS sync_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    status TEXT NOT NULL,
    message TEXT,
    counts_json TEXT
  );

  CREATE INDEX IF NOT EXISTS sync_runs_finished_at_idx ON sync_runs (finished_at DESC);

  CREATE TABLE IF NOT EXISTS sync_state (
    source TEXT NOT NULL,
    account_id TEXT NOT NULL,
    cursor_id TEXT,
    cursor_time TEXT,
    last_success_at TEXT,
    metadata_json TEXT,
    PRIMARY KEY (source, account_id)
  );

  CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );

  CREATE TABLE IF NOT EXISTS schedule_extractions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source TEXT NOT NULL,
    source_item_id TEXT NOT NULL,
    content_fingerprint TEXT NOT NULL,
    extractor_version TEXT NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL,
    result_json TEXT,
    error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );

  CREATE UNIQUE INDEX IF NOT EXISTS schedule_extractions_identity_idx
    ON schedule_extractions (source, source_item_id, content_fingerprint, extractor_version);
`

const JAPAN_TIME_ZONE = "Asia/Tokyo"
export const DEFAULT_X_HANDLES = ["kano_2525", "_Kanotic"]
export const DEFAULT_SCHEDULE_KEYWORDS = [
  "schedule",
  "スケジュール",
  "配信予定",
  "週間予定",
  "今週の予定",
  "予定",
]
const japanDateFormatter = new Intl.DateTimeFormat("en-CA", {
  timeZone: JAPAN_TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
})

function dateKeyInJapan(value) {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return null
  const parts = japanDateFormatter
    .formatToParts(date)
    .reduce((result, part) => {
      result[part.type] = part.value
      return result
    }, {})
  return `${parts.year}-${parts.month}-${parts.day}`
}

function youtubeIdFromUrl(value) {
  if (!value) return null
  try {
    const url = new URL(value)
    if (url.hostname === "youtu.be") return url.pathname.slice(1) || null
    if (url.hostname.endsWith("youtube.com")) {
      return (
        url.searchParams.get("v") ||
        url.pathname.match(/\/(?:live|shorts)\/([^/]+)/u)?.[1] ||
        null
      )
    }
  } catch {
    return null
  }
  return null
}

function normalizeXHandle(value) {
  const handle = String(value || "")
    .trim()
    .replace(/^@/u, "")
  return /^[A-Za-z0-9_]{1,30}$/u.test(handle) ? handle : null
}

function xHandleFromUrl(value) {
  if (!value) return null
  try {
    const url = new URL(value)
    if (
      !/(?:^|\.)x\.com$/iu.test(url.hostname) &&
      !/(?:^|\.)twitter\.com$/iu.test(url.hostname)
    ) {
      return null
    }
    const firstSegment = decodeURIComponent(
      url.pathname.split("/").filter(Boolean)[0] || "",
    )
    if (
      ["i", "intent", "share", "search", "hashtag"].includes(
        firstSegment.toLowerCase(),
      )
    )
      return null
    return normalizeXHandle(firstSegment)
  } catch {
    return null
  }
}

function parseListSetting(value, fallback, { fallbackOnEmpty = true } = {}) {
  if (value == null || value === "") return fallbackOnEmpty ? [...fallback] : []
  let values = value
  try {
    const parsed = JSON.parse(String(value))
    if (Array.isArray(parsed)) values = parsed
  } catch {
    values = String(value).split(/[\n,，]+/u)
  }
  const normalized = [
    ...new Set(values.map((item) => String(item).trim()).filter(Boolean)),
  ]
  return normalized.length || !fallbackOnEmpty ? normalized : [...fallback]
}

function configuredXHandles(database) {
  const stored = database
    .prepare("SELECT value FROM app_settings WHERE key = 'x_accounts'")
    .get()?.value
  const environment = process.env.X_HANDLES || process.env.X_HANDLE
  const candidates = parseListSetting(environment || stored, DEFAULT_X_HANDLES)
    .map(normalizeXHandle)
    .filter(Boolean)
  return [...new Set(candidates.map((handle) => handle.toLowerCase()))].map(
    (lower) => candidates.find((handle) => handle.toLowerCase() === lower),
  )
}

function tableColumns(database, table) {
  return new Set(
    database
      .prepare(`PRAGMA table_info(${table})`)
      .all()
      .map((row) => row.name),
  )
}

function migrateLegacyEvents(database) {
  const columns = tableColumns(database, "events")
  if (columns.has("starts_on")) return

  const rows = database.prepare("SELECT * FROM events ORDER BY id").all()
  const timestamp = nowIso()
  database.exec("DROP TABLE IF EXISTS event_sources")
  database.exec("DROP INDEX IF EXISTS events_starts_at_idx")
  database.exec("ALTER TABLE events RENAME TO events_legacy")
  database.exec(`
    CREATE TABLE events (
      id TEXT PRIMARY KEY,
      source TEXT NOT NULL,
      source_item_id TEXT,
      source_key TEXT,
      title TEXT NOT NULL,
      detail TEXT,
      starts_on TEXT NOT NULL,
      starts_at TEXT,
      ends_at TEXT,
      timezone TEXT NOT NULL DEFAULT 'Asia/Tokyo',
      time_precision TEXT NOT NULL DEFAULT 'exact',
      status TEXT,
      event_type TEXT DEFAULT 'event',
      url TEXT,
      is_upcoming INTEGER DEFAULT 0,
      provenance TEXT NOT NULL DEFAULT 'automatic',
      manual_locked INTEGER NOT NULL DEFAULT 0,
      confidence REAL,
      extraction_id INTEGER,
      deleted_at TEXT,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL,
      raw_json TEXT
    )
  `)
  const insert = database.prepare(`
    INSERT INTO events (
      id, source, source_item_id, source_key, title, detail, starts_on,
      starts_at, ends_at, timezone, time_precision, status, event_type, url,
      is_upcoming, provenance, manual_locked, confidence, extraction_id,
      deleted_at, created_at, updated_at, raw_json
    ) VALUES (
      @id, @source, @source_item_id, @source_key, @title, @detail, @starts_on,
      @starts_at, @ends_at, @timezone, @time_precision, @status, @event_type,
      @url, @is_upcoming, @provenance, @manual_locked, @confidence,
      @extraction_id, @deleted_at, @created_at, @updated_at, @raw_json
    )
  `)
  const migrate = database.transaction(() => {
    for (const row of rows) {
      const manuallyCurated = row.source === "x"
      insert.run({
        ...row,
        source_item_id: row.id,
        source_key: row.id,
        starts_on:
          dateKeyInJapan(row.starts_at) || String(row.starts_at).slice(0, 10),
        timezone: JAPAN_TIME_ZONE,
        time_precision: "exact",
        provenance: manuallyCurated ? "manual" : "automatic",
        manual_locked: manuallyCurated ? 1 : 0,
        confidence: null,
        extraction_id: null,
        deleted_at: null,
        created_at: timestamp,
        updated_at: timestamp,
      })
    }
  })
  migrate()
  database.exec("DROP TABLE events_legacy")
  database.exec(`
    CREATE INDEX events_starts_at_idx ON events (starts_on, starts_at);
    CREATE UNIQUE INDEX events_source_key_idx
      ON events (source, source_item_id, source_key)
      WHERE source_item_id IS NOT NULL AND source_key IS NOT NULL;
    CREATE TABLE event_sources (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      event_id TEXT NOT NULL REFERENCES events (id) ON DELETE CASCADE,
      source TEXT NOT NULL,
      source_item_id TEXT NOT NULL,
      source_key TEXT NOT NULL,
      url TEXT,
      raw_json TEXT,
      deleted_at TEXT,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL
    );
    CREATE UNIQUE INDEX event_sources_identity_idx
      ON event_sources (source, source_item_id, source_key);
    CREATE INDEX event_sources_event_idx ON event_sources (event_id);
  `)
  const insertSource = database.prepare(`
    INSERT INTO event_sources (
      event_id, source, source_item_id, source_key, url, raw_json, deleted_at,
      created_at, updated_at
    ) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?)
  `)
  const addSources = database.transaction(() => {
    for (const row of rows) {
      insertSource.run(
        row.id,
        row.source,
        row.id,
        row.id,
        row.url || null,
        row.raw_json || null,
        timestamp,
        timestamp,
      )
    }
  })
  addSources()
}

function migrateSchema(database) {
  migrateLegacyEvents(database)
  database.exec(`
    CREATE INDEX IF NOT EXISTS events_starts_at_idx
      ON events (starts_on, starts_at);
    CREATE UNIQUE INDEX IF NOT EXISTS events_source_key_idx
      ON events (source, source_item_id, source_key)
      WHERE source_item_id IS NOT NULL AND source_key IS NOT NULL;
  `)
  const focusColumns = tableColumns(database, "focus")
  if (!focusColumns.has("video_id")) {
    database.exec("ALTER TABLE focus ADD COLUMN video_id TEXT")
    const rows = database.prepare("SELECT id, url FROM focus").all()
    const update = database.prepare(
      "UPDATE focus SET video_id = ? WHERE id = ?",
    )
    for (const row of rows) update.run(youtubeIdFromUrl(row.url), row.id)
  }

  const postColumns = tableColumns(database, "posts")
  if (!postColumns.has("account_handle")) {
    database.exec("ALTER TABLE posts ADD COLUMN account_handle TEXT")
  }
  const legacyPosts = database
    .prepare(
      "SELECT id, url FROM posts WHERE account_handle IS NULL OR account_handle = ''",
    )
    .all()
  if (legacyPosts.length) {
    const updatePost = database.prepare(
      "UPDATE posts SET account_handle = ? WHERE id = ?",
    )
    const migratePosts = database.transaction(() => {
      for (const post of legacyPosts) {
        const handle = xHandleFromUrl(post.url)
        if (handle) updatePost.run(handle, post.id)
      }
    })
    migratePosts()
  }
  database.exec(
    "CREATE INDEX IF NOT EXISTS posts_account_handle_idx ON posts (account_handle, published_at DESC)",
  )
}

function json(value) {
  return value == null ? null : JSON.stringify(value)
}

function numberOrZero(value) {
  const number = Number(value)
  return Number.isFinite(number) ? number : 0
}

function nullable(value) {
  return value == null || value === "" ? null : value
}

function nowIso() {
  return new Date().toISOString()
}

function asIntegerOrNull(value) {
  if (value == null || value === "") return null
  const number = Number(value)
  return Number.isInteger(number) && number >= 0 ? number : null
}

function mediaStatus(value, fallback = MEDIA_STATUS.PENDING) {
  if (value == null || value === "") return fallback
  const status = String(value)
  if (!Object.values(MEDIA_STATUS).includes(status))
    throw new Error("invalid media status")
  return status
}

function mediaSource(value) {
  return value?.source_url ?? value?.sourceUrl ?? value?.url ?? null
}

function mediaOwner(value) {
  const ownerType = value?.owner_type ?? value?.ownerType
  const ownerId = value?.owner_id ?? value?.ownerId
  if (
    ownerType == null ||
    ownerId == null ||
    String(ownerType).trim() === "" ||
    String(ownerId).trim() === ""
  ) {
    return null
  }
  return { ownerType: String(ownerType), ownerId: String(ownerId) }
}

function mediaRole(value) {
  return String(value?.role || "image").trim() || "image"
}

function normalizedMediaCandidate(candidate) {
  const sourceUrl = normalizeSourceUrl(mediaSource(candidate))
  if (!sourceUrl) return null
  const id = mediaIdForSourceUrl(sourceUrl)
  if (!id) return null
  const owner = mediaOwner(candidate)
  return {
    id,
    source: String(candidate?.source || "unknown"),
    sourceUrl,
    owner,
    role: mediaRole(candidate),
    position: Math.max(0, Math.trunc(Number(candidate?.position ?? 0) || 0)),
    alt: nullable(
      candidate?.alt ?? candidate?.media_alt ?? candidate?.mediaAlt,
    ),
    raw: candidate?.raw ?? candidate,
  }
}

function insertRegisteredMediaAsset(database, candidate, timestamp = nowIso()) {
  const row = normalizedMediaCandidate(candidate)
  if (!row) return null
  database
    .prepare(
      `
    INSERT INTO media_assets (
      id, source, source_url, status, last_seen_at, created_at, updated_at, raw_json
    ) VALUES (@id, @source, @source_url, @status, @last_seen_at, @created_at, @updated_at, @raw_json)
    ON CONFLICT(id) DO UPDATE SET
      source=excluded.source,
      source_url=excluded.source_url,
      last_seen_at=excluded.last_seen_at,
      updated_at=excluded.updated_at,
      raw_json=COALESCE(excluded.raw_json, media_assets.raw_json),
      status=CASE WHEN media_assets.status = @ready_status THEN @ready_status ELSE @pending_status END
  `,
    )
    .run({
      id: row.id,
      source: row.source,
      source_url: row.sourceUrl,
      status: MEDIA_STATUS.PENDING,
      last_seen_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp,
      raw_json: json(row.raw),
      ready_status: MEDIA_STATUS.READY,
      pending_status: MEDIA_STATUS.PENDING,
    })
  return row
}

function insertMediaLink(database, row, timestamp = nowIso()) {
  if (!row?.mediaId || !row?.owner) return false
  database
    .prepare(
      `
    INSERT INTO media_links (
      media_id, owner_type, owner_id, role, position, alt, created_at, updated_at
    ) VALUES (@media_id, @owner_type, @owner_id, @role, @position, @alt, @created_at, @updated_at)
    ON CONFLICT(owner_type, owner_id, role, position) DO UPDATE SET
      media_id=excluded.media_id,
      alt=COALESCE(excluded.alt, media_links.alt),
      updated_at=excluded.updated_at
  `,
    )
    .run({
      media_id: row.mediaId,
      owner_type: row.owner.ownerType,
      owner_id: row.owner.ownerId,
      role: row.role,
      position: row.position,
      alt: row.alt,
      created_at: timestamp,
      updated_at: timestamp,
    })
  return true
}

export function openDatabase({ filename = databasePath } = {}) {
  if (filename !== ":memory:")
    fs.mkdirSync(path.dirname(path.resolve(filename)), { recursive: true })
  ensureMediaCacheDirectories()
  const database = new Database(filename)
  database.pragma("journal_mode = WAL")
  database.pragma("foreign_keys = ON")
  database.exec(schema)
  migrateSchema(database)
  return database
}

function insertProfile(database, profile, overwrite) {
  const sql = overwrite
    ? `INSERT INTO profiles (id, display_name, romanized_name, bio, avatar_url, banner_url, x_url, youtube_url, updated_at, raw_json)
       VALUES (@id, @display_name, @romanized_name, @bio, @avatar_url, @banner_url, @x_url, @youtube_url, @updated_at, @raw_json)
       ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name, romanized_name=excluded.romanized_name,
       bio=excluded.bio, avatar_url=excluded.avatar_url, banner_url=excluded.banner_url, x_url=excluded.x_url,
       youtube_url=excluded.youtube_url, updated_at=excluded.updated_at, raw_json=excluded.raw_json`
    : `INSERT OR IGNORE INTO profiles (id, display_name, romanized_name, bio, avatar_url, banner_url, x_url, youtube_url, updated_at, raw_json)
       VALUES (@id, @display_name, @romanized_name, @bio, @avatar_url, @banner_url, @x_url, @youtube_url, @updated_at, @raw_json)`
  database.prepare(sql).run({
    id: profile.id,
    display_name: profile.display_name,
    romanized_name: nullable(profile.romanized_name),
    bio: nullable(profile.bio),
    avatar_url: nullable(profile.avatar_url),
    banner_url: nullable(profile.banner_url),
    x_url: nullable(profile.x_url),
    youtube_url: nullable(profile.youtube_url),
    updated_at: nullable(profile.updated_at),
    raw_json: json(profile),
  })
}

function insertPost(database, post, overwrite) {
  const sql = overwrite
    ? `INSERT INTO posts (id, source, account_handle, type, label, text, published_at, url, likes, reposts, replies, media_url, media_alt, raw_json)
       VALUES (@id, @source, @account_handle, @type, @label, @text, @published_at, @url, @likes, @reposts, @replies, @media_url, @media_alt, @raw_json)
       ON CONFLICT(id) DO UPDATE SET source=excluded.source, account_handle=excluded.account_handle, type=excluded.type, label=excluded.label, text=excluded.text,
       published_at=excluded.published_at, url=excluded.url, likes=excluded.likes, reposts=excluded.reposts,
       replies=excluded.replies, media_url=excluded.media_url, media_alt=excluded.media_alt, raw_json=excluded.raw_json`
    : `INSERT OR IGNORE INTO posts (id, source, account_handle, type, label, text, published_at, url, likes, reposts, replies, media_url, media_alt, raw_json)
       VALUES (@id, @source, @account_handle, @type, @label, @text, @published_at, @url, @likes, @reposts, @replies, @media_url, @media_alt, @raw_json)`
  const source = post.source || "x"
  const url = post.url || ""
  const accountHandle =
    source === "x"
      ? normalizeXHandle(
          post.account_handle ?? post.accountHandle ?? xHandleFromUrl(url),
        )
      : null
  database.prepare(sql).run({
    id: String(post.id),
    source,
    account_handle: nullable(accountHandle),
    type: post.type || "daily",
    label: nullable(post.label),
    text: post.text || "",
    published_at: post.published_at || new Date().toISOString(),
    url,
    likes: numberOrZero(post.likes),
    reposts: numberOrZero(post.reposts),
    replies: numberOrZero(post.replies),
    media_url: nullable(post.media_url),
    media_alt: nullable(post.media_alt),
    raw_json: json(post),
  })
}

function insertEvent(database, event, overwrite) {
  const timestamp = nowIso()
  const startsAt = nullable(event.starts_at ?? event.startsAt)
  const startsOn =
    nullable(event.starts_on ?? event.startsOn) || dateKeyInJapan(startsAt)
  if (!startsOn) throw new Error("event requires starts_on or starts_at")
  const provenance = event.provenance === "manual" ? "manual" : "automatic"
  const manualLocked =
    event.manual_locked ??
    event.manualLocked ??
    (provenance === "manual" ? 1 : 0)
  const sql = overwrite
    ? `INSERT INTO events (
         id, source, source_item_id, source_key, title, detail, starts_on, starts_at,
         ends_at, timezone, time_precision, status, event_type, url, is_upcoming,
         provenance, manual_locked, confidence, extraction_id, deleted_at,
         created_at, updated_at, raw_json
       ) VALUES (
         @id, @source, @source_item_id, @source_key, @title, @detail, @starts_on,
         @starts_at, @ends_at, @timezone, @time_precision, @status, @event_type,
         @url, @is_upcoming, @provenance, @manual_locked, @confidence,
         @extraction_id, @deleted_at, @created_at, @updated_at, @raw_json
       )
       ON CONFLICT(id) DO UPDATE SET
         source=excluded.source, source_item_id=excluded.source_item_id,
         source_key=excluded.source_key, title=excluded.title, detail=excluded.detail,
         starts_on=excluded.starts_on, starts_at=excluded.starts_at,
         ends_at=excluded.ends_at, timezone=excluded.timezone,
         time_precision=excluded.time_precision, status=excluded.status,
         event_type=excluded.event_type, url=excluded.url,
         is_upcoming=excluded.is_upcoming, provenance=excluded.provenance,
         manual_locked=excluded.manual_locked, confidence=excluded.confidence,
         extraction_id=excluded.extraction_id, deleted_at=excluded.deleted_at,
         updated_at=excluded.updated_at, raw_json=excluded.raw_json
       WHERE events.manual_locked = 0`
    : `INSERT OR IGNORE INTO events (
         id, source, source_item_id, source_key, title, detail, starts_on, starts_at,
         ends_at, timezone, time_precision, status, event_type, url, is_upcoming,
         provenance, manual_locked, confidence, extraction_id, deleted_at,
         created_at, updated_at, raw_json
       ) VALUES (
         @id, @source, @source_item_id, @source_key, @title, @detail, @starts_on,
         @starts_at, @ends_at, @timezone, @time_precision, @status, @event_type,
         @url, @is_upcoming, @provenance, @manual_locked, @confidence,
         @extraction_id, @deleted_at, @created_at, @updated_at, @raw_json
       )`
  const values = {
    id: String(event.id),
    source: event.source || "manual",
    source_item_id:
      nullable(event.source_item_id ?? event.sourceItemId) || String(event.id),
    source_key:
      nullable(event.source_key ?? event.sourceKey) || String(event.id),
    title: event.title || "未命名活动",
    detail: nullable(event.detail),
    starts_on: startsOn,
    starts_at: startsAt,
    ends_at: nullable(event.ends_at ?? event.endsAt),
    timezone: event.timezone || JAPAN_TIME_ZONE,
    time_precision:
      event.time_precision ??
      event.timePrecision ??
      (startsAt ? "exact" : "unknown"),
    status: nullable(event.status),
    event_type: event.event_type ?? event.eventType ?? "event",
    url: nullable(event.url),
    is_upcoming: (event.is_upcoming ?? event.isUpcoming) ? 1 : 0,
    provenance,
    manual_locked: manualLocked ? 1 : 0,
    confidence:
      event.confidence == null || !Number.isFinite(Number(event.confidence))
        ? null
        : Number(event.confidence),
    extraction_id: asIntegerOrNull(event.extraction_id ?? event.extractionId),
    deleted_at: nullable(event.deleted_at ?? event.deletedAt),
    created_at: nullable(event.created_at ?? event.createdAt) || timestamp,
    updated_at: nullable(event.updated_at ?? event.updatedAt) || timestamp,
    raw_json: json(event),
  }
  const result = database.prepare(sql).run(values)
  return { changes: result.changes, values }
}

function insertEventSource(database, event, eventId, timestamp = nowIso()) {
  const source = String(event.source || "manual")
  const sourceItemId = String(
    event.source_item_id ?? event.sourceItemId ?? eventId,
  )
  const sourceKey = String(event.source_key ?? event.sourceKey ?? eventId)
  database
    .prepare(
      `
      INSERT INTO event_sources (
        event_id, source, source_item_id, source_key, url, raw_json, deleted_at,
        created_at, updated_at
      ) VALUES (
        @event_id, @source, @source_item_id, @source_key, @url, @raw_json,
        NULL, @created_at, @updated_at
      )
      ON CONFLICT(source, source_item_id, source_key) DO UPDATE SET
        event_id=excluded.event_id,
        url=COALESCE(excluded.url, event_sources.url),
        raw_json=COALESCE(excluded.raw_json, event_sources.raw_json),
        deleted_at=NULL,
        updated_at=excluded.updated_at
    `,
    )
    .run({
      event_id: String(eventId),
      source,
      source_item_id: sourceItemId,
      source_key: sourceKey,
      url: nullable(event.url),
      raw_json: json(event),
      created_at: timestamp,
      updated_at: timestamp,
    })
}

function insertVideo(database, video, overwrite) {
  const sql = overwrite
    ? `INSERT INTO videos (id, source, title, published_at, scheduled_at, url, thumbnail_url, kind, is_upcoming, raw_json)
       VALUES (@id, @source, @title, @published_at, @scheduled_at, @url, @thumbnail_url, @kind, @is_upcoming, @raw_json)
       ON CONFLICT(id) DO UPDATE SET source=excluded.source, title=excluded.title,
       published_at=COALESCE(excluded.published_at, videos.published_at),
       scheduled_at=COALESCE(excluded.scheduled_at, videos.scheduled_at),
       url=excluded.url, thumbnail_url=COALESCE(excluded.thumbnail_url, videos.thumbnail_url),
       kind=COALESCE(excluded.kind, videos.kind),
       is_upcoming=CASE
         WHEN excluded.scheduled_at IS NULL AND videos.scheduled_at IS NOT NULL THEN videos.is_upcoming
         ELSE excluded.is_upcoming
       END,
       raw_json=excluded.raw_json`
    : `INSERT OR IGNORE INTO videos (id, source, title, published_at, scheduled_at, url, thumbnail_url, kind, is_upcoming, raw_json)
       VALUES (@id, @source, @title, @published_at, @scheduled_at, @url, @thumbnail_url, @kind, @is_upcoming, @raw_json)`
  database.prepare(sql).run({
    id: String(video.id),
    source: video.source || "youtube",
    title: video.title || "未命名视频",
    published_at: nullable(video.published_at),
    scheduled_at: nullable(video.scheduled_at),
    url: video.url || `https://www.youtube.com/watch?v=${video.id}`,
    thumbnail_url: nullable(video.thumbnail_url),
    kind: nullable(video.kind),
    is_upcoming: video.is_upcoming ? 1 : 0,
    raw_json: json(video),
  })
}

function insertFocus(database, focus, overwrite) {
  const sql = overwrite
    ? `INSERT INTO focus (id, video_id, date_label, title, description, image_url, url, source_url, updated_at, raw_json)
       VALUES (@id, @video_id, @date_label, @title, @description, @image_url, @url, @source_url, @updated_at, @raw_json)
       ON CONFLICT(id) DO UPDATE SET video_id=excluded.video_id, date_label=excluded.date_label, title=excluded.title, description=excluded.description,
       image_url=excluded.image_url, url=excluded.url, source_url=excluded.source_url, updated_at=excluded.updated_at,
       raw_json=excluded.raw_json`
    : `INSERT OR IGNORE INTO focus (id, video_id, date_label, title, description, image_url, url, source_url, updated_at, raw_json)
       VALUES (@id, @video_id, @date_label, @title, @description, @image_url, @url, @source_url, @updated_at, @raw_json)`
  database.prepare(sql).run({
    id: Number(focus.id || 1),
    video_id: nullable(
      focus.video_id ?? focus.videoId ?? youtubeIdFromUrl(focus.url),
    ),
    date_label: nullable(focus.date_label),
    title: focus.title || "最近焦点",
    description: nullable(focus.description),
    image_url: nullable(focus.image_url),
    url: nullable(focus.url),
    source_url: nullable(focus.source_url),
    updated_at: nullable(focus.updated_at),
    raw_json: json(focus),
  })
}

function insertTimeline(database, item, overwrite) {
  const sql = overwrite
    ? `INSERT INTO timeline (id, year, title, detail, sort_order, raw_json)
       VALUES (@id, @year, @title, @detail, @sort_order, @raw_json)
       ON CONFLICT(id) DO UPDATE SET year=excluded.year, title=excluded.title, detail=excluded.detail,
       sort_order=excluded.sort_order, raw_json=excluded.raw_json`
    : `INSERT OR IGNORE INTO timeline (id, year, title, detail, sort_order, raw_json)
       VALUES (@id, @year, @title, @detail, @sort_order, @raw_json)`
  database.prepare(sql).run({
    id: Number(item.id),
    year: String(item.year),
    title: item.title || "",
    detail: nullable(item.detail),
    sort_order: numberOrZero(item.sort_order),
    raw_json: json(item),
  })
}

function insertResource(database, resource, overwrite) {
  const sql = overwrite
    ? `INSERT INTO resources (id, title, detail, icon, tone, url, sort_order, raw_json)
       VALUES (@id, @title, @detail, @icon, @tone, @url, @sort_order, @raw_json)
       ON CONFLICT(id) DO UPDATE SET title=excluded.title, detail=excluded.detail, icon=excluded.icon,
       tone=excluded.tone, url=excluded.url, sort_order=excluded.sort_order, raw_json=excluded.raw_json`
    : `INSERT OR IGNORE INTO resources (id, title, detail, icon, tone, url, sort_order, raw_json)
       VALUES (@id, @title, @detail, @icon, @tone, @url, @sort_order, @raw_json)`
  database.prepare(sql).run({
    id: Number(resource.id),
    title: resource.title || "资料",
    detail: nullable(resource.detail),
    icon: nullable(resource.icon),
    tone: nullable(resource.tone),
    url: resource.url || "",
    sort_order: numberOrZero(resource.sort_order),
    raw_json: json(resource),
  })
}

function insertAsset(database, asset, overwrite) {
  const sql = overwrite
    ? `INSERT INTO assets (id, kind, url, source_url, alt, updated_at, raw_json)
       VALUES (@id, @kind, @url, @source_url, @alt, @updated_at, @raw_json)
       ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, url=excluded.url, source_url=excluded.source_url,
       alt=excluded.alt, updated_at=excluded.updated_at, raw_json=excluded.raw_json`
    : `INSERT OR IGNORE INTO assets (id, kind, url, source_url, alt, updated_at, raw_json)
       VALUES (@id, @kind, @url, @source_url, @alt, @updated_at, @raw_json)`
  database.prepare(sql).run({
    id: String(asset.id),
    kind: asset.kind || "image",
    url: asset.url || "",
    source_url: nullable(asset.source_url),
    alt: nullable(asset.alt),
    updated_at: nullable(asset.updated_at),
    raw_json: json(asset),
  })
}

function mediaCandidatesFromProfile(profile) {
  return [
    {
      source: "profile",
      source_url: profile?.avatar_url,
      owner_type: "profile",
      owner_id: profile?.id,
      role: "avatar",
      position: 0,
    },
    {
      source: "profile",
      source_url: profile?.banner_url,
      owner_type: "profile",
      owner_id: profile?.id,
      role: "banner",
      position: 0,
    },
  ]
}

function mediaCandidatesFromPost(post) {
  const urls = Array.isArray(post?.media_urls)
    ? post.media_urls
    : [post?.media_url]
  return urls.map((source_url, position) => ({
    source: post?.source || "x",
    source_url,
    owner_type: "post",
    owner_id: post?.id,
    role: "post-image",
    position,
    alt: post?.media_alt,
  }))
}

function mediaCandidatesFromVideo(video) {
  const urls = Array.isArray(video?.thumbnail_urls)
    ? video.thumbnail_urls
    : [video?.thumbnail_url]
  return urls.map((source_url, position) => ({
    source: video?.source || "youtube",
    source_url,
    owner_type: "video",
    owner_id: video?.id,
    role: "thumbnail",
    position,
  }))
}

function mediaCandidatesFromFocus(focus) {
  return [
    {
      source: "focus",
      source_url: focus?.image_url,
      owner_type: "focus",
      owner_id: focus?.id || 1,
      role: "focus-image",
      position: 0,
      alt: focus?.title,
    },
  ]
}

function mediaCandidatesFromAsset(asset) {
  return [
    {
      source: asset?.source || asset?.kind || "asset",
      source_url: asset?.url,
      owner_type: "asset",
      owner_id: asset?.id,
      role: asset?.kind || "image",
      position: 0,
      alt: asset?.alt,
    },
  ]
}

function collectSeedMediaCandidates(data) {
  return [
    ...mediaCandidatesFromProfile(data?.profile),
    ...(data?.posts || []).flatMap(mediaCandidatesFromPost),
    ...(data?.videos || []).flatMap(mediaCandidatesFromVideo),
    ...(data?.focus ? mediaCandidatesFromFocus(data.focus) : []),
    ...(data?.assets || []).flatMap(mediaCandidatesFromAsset),
  ]
}

/**
 * Register remote media discovered by a source adapter. Registration is
 * intentionally separate from downloading: the sync pipeline can claim pending
 * rows after all source snapshots have been registered.
 */
export function registerMediaCandidates(database, candidates = []) {
  const timestamp = nowIso()
  const register = database.transaction((rows) => {
    let registered = 0
    let linked = 0
    let skipped = 0
    for (const candidate of rows) {
      const row = insertRegisteredMediaAsset(database, candidate, timestamp)
      if (!row) {
        skipped += 1
        continue
      }
      registered += 1
      if (insertMediaLink(database, { ...row, mediaId: row.id }, timestamp))
        linked += 1
    }
    return { registered, linked, skipped }
  })
  return register(candidates)
}

/** Upsert metadata written by the downloader after a successful fetch. */
export function upsertMediaAsset(database, asset) {
  const sourceUrl = normalizeSourceUrl(mediaSource(asset))
  if (!sourceUrl) throw new Error("media asset requires an HTTP(S) source URL")
  const id = mediaIdForSourceUrl(sourceUrl)
  if (!id) throw new Error("media asset requires a valid source URL")
  if (asset?.id && String(asset.id) !== id)
    throw new Error("media asset ID does not match its source URL")
  const timestamp = nowIso()
  const status = mediaStatus(asset?.status, MEDIA_STATUS.READY)
  const requestedCachePath = asset?.cache_path ?? asset?.cachePath
  if (status === MEDIA_STATUS.READY && !requestedCachePath) {
    throw new Error("ready media asset requires a cache path")
  }
  if (
    requestedCachePath &&
    !resolveMediaCachePath(String(requestedCachePath))
  ) {
    throw new Error("invalid media cache path")
  }
  const requestedHash =
    asset?.sha256 == null ? null : String(asset.sha256).toLowerCase()
  if (
    requestedHash != null &&
    !/^[a-f0-9]{64}$/iu.test(String(requestedHash))
  ) {
    throw new Error("invalid media content hash")
  }
  const normalizedMimeType = normalizeMediaMimeType(
    asset?.mime_type ?? asset?.mimeType,
  )
  let readyStat = null
  if (status === MEDIA_STATUS.READY) {
    if (!normalizedMimeType)
      throw new Error("ready media asset requires an allowed image MIME type")
    if (!requestedHash)
      throw new Error("ready media asset requires a content hash")
    const expectedPath = cacheRelativePathForHash(
      requestedHash,
      extensionForMimeType(normalizedMimeType),
    )
    if (String(requestedCachePath) !== expectedPath)
      throw new Error("ready media cache path does not match its content hash")
    const fullPath = resolveMediaCachePath(expectedPath)
    try {
      readyStat = fs.statSync(fullPath)
    } catch {
      readyStat = null
    }
    if (!readyStat?.isFile())
      throw new Error("ready media cache file does not exist")
  }
  const values = {
    id,
    source: String(asset?.source || "unknown"),
    source_url: sourceUrl,
    cache_path: nullable(asset?.cache_path ?? asset?.cachePath),
    mime_type: normalizedMimeType,
    extension: nullable(
      asset?.extension ??
        (normalizedMimeType ? extensionForMimeType(normalizedMimeType) : null),
    ),
    byte_size:
      asIntegerOrNull(asset?.byte_size ?? asset?.byteSize) ??
      readyStat?.size ??
      null,
    sha256: requestedHash,
    width: asIntegerOrNull(asset?.width),
    height: asIntegerOrNull(asset?.height),
    status,
    fetched_at: nullable(
      asset?.fetched_at ??
        asset?.fetchedAt ??
        (status === MEDIA_STATUS.READY ? timestamp : null),
    ),
    last_checked_at: nullable(
      asset?.last_checked_at ?? asset?.lastCheckedAt ?? timestamp,
    ),
    last_seen_at: nullable(
      asset?.last_seen_at ?? asset?.lastSeenAt ?? timestamp,
    ),
    etag: nullable(asset?.etag),
    last_modified: nullable(asset?.last_modified ?? asset?.lastModified),
    last_error: nullable(asset?.last_error ?? asset?.lastError),
    created_at: timestamp,
    updated_at: timestamp,
    raw_json: json(asset),
  }
  database
    .prepare(
      `
    INSERT INTO media_assets (
      id, source, source_url, cache_path, mime_type, extension, byte_size, sha256,
      width, height, status, fetched_at, last_checked_at, last_seen_at, etag,
      last_modified, last_error, created_at, updated_at, raw_json
    ) VALUES (
      @id, @source, @source_url, @cache_path, @mime_type, @extension, @byte_size, @sha256,
      @width, @height, @status, @fetched_at, @last_checked_at, @last_seen_at, @etag,
      @last_modified, @last_error, @created_at, @updated_at, @raw_json
    )
    ON CONFLICT(id) DO UPDATE SET
      source=excluded.source,
      source_url=excluded.source_url,
      cache_path=COALESCE(excluded.cache_path, media_assets.cache_path),
      mime_type=COALESCE(excluded.mime_type, media_assets.mime_type),
      extension=COALESCE(excluded.extension, media_assets.extension),
      byte_size=COALESCE(excluded.byte_size, media_assets.byte_size),
      sha256=COALESCE(excluded.sha256, media_assets.sha256),
      width=COALESCE(excluded.width, media_assets.width),
      height=COALESCE(excluded.height, media_assets.height),
      status=CASE
        WHEN media_assets.status = 'ready' AND excluded.status != 'ready' THEN media_assets.status
        ELSE excluded.status
      END,
      fetched_at=COALESCE(excluded.fetched_at, media_assets.fetched_at),
      last_checked_at=COALESCE(excluded.last_checked_at, media_assets.last_checked_at),
      last_seen_at=COALESCE(excluded.last_seen_at, media_assets.last_seen_at),
      etag=COALESCE(excluded.etag, media_assets.etag),
      last_modified=COALESCE(excluded.last_modified, media_assets.last_modified),
      last_error=excluded.last_error,
      updated_at=excluded.updated_at,
      raw_json=COALESCE(excluded.raw_json, media_assets.raw_json)
  `,
    )
    .run(values)
  return getMediaAsset(database, id)
}

const mediaAssetColumns = `
  id, source, source_url AS sourceUrl, cache_path AS cachePath,
  mime_type AS mimeType, extension, byte_size AS byteSize, sha256,
  width, height, status, fetched_at AS fetchedAt, last_checked_at AS lastCheckedAt,
  last_seen_at AS lastSeenAt, etag, last_modified AS lastModified,
  last_error AS lastError, created_at AS createdAt, updated_at AS updatedAt
`

export function getMediaAsset(database, id) {
  if (!isSafeMediaId(id)) return null
  return (
    database
      .prepare(`SELECT ${mediaAssetColumns} FROM media_assets WHERE id = ?`)
      .get(id) || null
  )
}

export function listMediaAssets(database, { status } = {}) {
  if (status) {
    return database
      .prepare(
        `SELECT ${mediaAssetColumns} FROM media_assets WHERE status = ? ORDER BY updated_at DESC, id ASC`,
      )
      .all(status)
  }
  return database
    .prepare(
      `SELECT ${mediaAssetColumns} FROM media_assets ORDER BY updated_at DESC, id ASC`,
    )
    .all()
}

export function listMediaLinks(database, { mediaId, ownerType, ownerId } = {}) {
  const clauses = []
  const values = []
  if (mediaId) {
    clauses.push("media_id = ?")
    values.push(mediaId)
  }
  if (ownerType) {
    clauses.push("owner_type = ?")
    values.push(ownerType)
  }
  if (ownerId) {
    clauses.push("owner_id = ?")
    values.push(String(ownerId))
  }
  const where = clauses.length ? `WHERE ${clauses.join(" AND ")}` : ""
  return database
    .prepare(
      `SELECT id, media_id AS mediaId, owner_type AS ownerType, owner_id AS ownerId, role, position, alt, created_at AS createdAt, updated_at AS updatedAt FROM media_links ${where} ORDER BY owner_type, owner_id, role, position`,
    )
    .all(...values)
}

function mediaReferenceForAsset(asset) {
  if (!asset) return null
  const cachePath = asset.cachePath
    ? resolveMediaCachePath(asset.cachePath)
    : null
  const ready =
    asset.status === MEDIA_STATUS.READY &&
    cachePath &&
    isSafeContentHash(asset.sha256) &&
    fs.existsSync(cachePath)
  return {
    id: asset.id,
    status: ready
      ? MEDIA_STATUS.READY
      : asset.status === MEDIA_STATUS.READY
        ? MEDIA_STATUS.MISSING
        : asset.status || MEDIA_STATUS.MISSING,
    sourceUrl: asset.sourceUrl,
    publicUrl: ready ? publicMediaUrl(asset.id, asset.sha256) : null,
    mimeType: asset.mimeType || null,
    byteSize: asset.byteSize ?? null,
    width: asset.width ?? null,
    height: asset.height ?? null,
  }
}

/** Map a stored URL to a safe browser URL while retaining source/status data. */
export function resolveMediaReference(database, sourceUrl, bySourceUrl = null) {
  if (!sourceUrl)
    return {
      id: null,
      status: MEDIA_STATUS.MISSING,
      sourceUrl: null,
      publicUrl: null,
    }
  if (String(sourceUrl).startsWith("/assets/")) {
    const exists = publicAssetExists(String(sourceUrl))
    return {
      id: null,
      status: exists ? MEDIA_STATUS.READY : MEDIA_STATUS.MISSING,
      sourceUrl: null,
      publicUrl: exists ? String(sourceUrl) : null,
    }
  }
  const normalized = normalizeSourceUrl(sourceUrl)
  if (!normalized)
    return {
      id: null,
      status: MEDIA_STATUS.MISSING,
      sourceUrl: null,
      publicUrl: null,
    }
  const asset =
    bySourceUrl?.get(normalized) ||
    database
      .prepare(
        `SELECT ${mediaAssetColumns} FROM media_assets WHERE source_url = ?`,
      )
      .get(normalized)
  return (
    mediaReferenceForAsset(asset) || {
      id: mediaIdForSourceUrl(normalized),
      status: MEDIA_STATUS.PENDING,
      sourceUrl: normalized,
      publicUrl: null,
    }
  )
}

export function seedDatabase(
  database,
  data = seedData,
  { overwrite = false } = {},
) {
  const seed = database.transaction(() => {
    insertProfile(database, data.profile, overwrite)
    for (const post of data.posts || []) insertPost(database, post, overwrite)
    for (const event of data.events || []) {
      insertEvent(database, event, overwrite)
      insertEventSource(database, event, event.id)
    }
    for (const video of data.videos || [])
      insertVideo(database, video, overwrite)
    if (data.focus) insertFocus(database, data.focus, overwrite)
    for (const item of data.timeline || [])
      insertTimeline(database, item, overwrite)
    for (const item of data.resources || [])
      insertResource(database, item, overwrite)
    for (const asset of data.assets || [])
      insertAsset(database, asset, overwrite)
  })
  seed()
  registerMediaCandidates(database, collectSeedMediaCandidates(data))
  if (getAppSetting(database, "x_accounts", null) == null) {
    const seedHandles = [
      xHandleFromUrl(data?.profile?.x_url),
      ...DEFAULT_X_HANDLES,
    ].filter(Boolean)
    setAppSetting(
      database,
      "x_accounts",
      JSON.stringify([...new Set(seedHandles)]),
    )
  }
  if (getAppSetting(database, "schedule_extraction_enabled", null) == null) {
    const enabled = settingBoolean(
      process.env.SCHEDULE_EXTRACTION_ENABLED ?? "1",
      true,
    )
    setAppSetting(database, "schedule_extraction_enabled", enabled ? "1" : "0")
  }
  if (getAppSetting(database, "schedule_keywords", null) == null) {
    const keywords = normalizedScheduleKeywords(
      process.env.SCHEDULE_KEYWORDS ?? DEFAULT_SCHEDULE_KEYWORDS,
    )
    setAppSetting(database, "schedule_keywords", JSON.stringify(keywords))
  }
  if (getAppSetting(database, "featured_video_id", null) == null) {
    const seedFeatured =
      data?.focus?.video_id ??
      data?.focus?.videoId ??
      youtubeIdFromUrl(data?.focus?.url)
    if (seedFeatured) setAppSetting(database, "featured_video_id", seedFeatured)
  }
}

export function initializeDatabase({
  seed = true,
  filename = databasePath,
} = {}) {
  const database = openDatabase({ filename })
  if (seed) seedDatabase(database)
  return database
}

export function upsertProfile(database, profile) {
  insertProfile(database, profile, true)
  registerMediaCandidates(database, mediaCandidatesFromProfile(profile))
}

export function upsertPosts(database, posts = []) {
  const run = database.transaction((rows) =>
    rows.forEach((post) => insertPost(database, post, true)),
  )
  run(posts)
  registerMediaCandidates(database, posts.flatMap(mediaCandidatesFromPost))
}

export function upsertEvents(database, events = []) {
  const run = database.transaction((rows) => {
    for (const event of rows) {
      insertEvent(database, event, true)
      insertEventSource(database, event, event.id)
    }
  })
  run(events)
}

export function upsertVideos(database, videos = []) {
  const run = database.transaction((rows) =>
    rows.forEach((video) => insertVideo(database, video, true)),
  )
  run(videos)
  registerMediaCandidates(database, videos.flatMap(mediaCandidatesFromVideo))
}

export function upsertAssets(database, assets = []) {
  const run = database.transaction((rows) =>
    rows.forEach((asset) => insertAsset(database, asset, true)),
  )
  run(assets)
  registerMediaCandidates(database, assets.flatMap(mediaCandidatesFromAsset))
}

const eventAdminColumns = `
  id, source, source_item_id AS sourceItemId, source_key AS sourceKey,
  title, detail, starts_on AS startsOn, starts_at AS startsAt,
  ends_at AS endsAt, timezone, time_precision AS timePrecision,
  status, event_type AS eventType, url, provenance,
  manual_locked AS manualLocked, confidence, extraction_id AS extractionId,
  deleted_at AS deletedAt, created_at AS createdAt, updated_at AS updatedAt
`

export function getEvent(database, id) {
  return (
    database
      .prepare(`SELECT ${eventAdminColumns} FROM events WHERE id = ?`)
      .get(String(id)) || null
  )
}

export function listAdminEvents(database, { includeDeleted = false } = {}) {
  const where = includeDeleted ? "" : "WHERE deleted_at IS NULL"
  return database
    .prepare(
      `SELECT ${eventAdminColumns} FROM events ${where}
       ORDER BY starts_on DESC, COALESCE(starts_at, starts_on) DESC, id ASC`,
    )
    .all()
    .map((event) => ({
      ...event,
      manualLocked: Boolean(event.manualLocked),
    }))
}

export function createManualEvent(database, event) {
  const id = String(event.id || `manual-${crypto.randomUUID()}`)
  if (getEvent(database, id)) throw new Error("event already exists")
  const value = {
    ...event,
    id,
    source: "manual",
    source_item_id: id,
    source_key: id,
    provenance: "manual",
    manual_locked: 1,
    deleted_at: null,
  }
  const create = database.transaction(() => {
    insertEvent(database, value, false)
    insertEventSource(database, value, id)
  })
  create()
  return getEvent(database, id)
}

export function updateManualEvent(database, id, event) {
  const existing = getEvent(database, id)
  if (!existing) return null
  const startsAt = nullable(event.starts_at ?? event.startsAt)
  const startsOn =
    nullable(event.starts_on ?? event.startsOn) || dateKeyInJapan(startsAt)
  if (!startsOn) throw new Error("event requires starts_on or starts_at")
  const timestamp = nowIso()
  database
    .prepare(
      `UPDATE events SET
         title=@title, detail=@detail, starts_on=@starts_on,
         starts_at=@starts_at, ends_at=@ends_at, timezone=@timezone,
         time_precision=@time_precision, status=@status,
         event_type=@event_type, url=@url, provenance='manual',
         manual_locked=1, deleted_at=NULL, updated_at=@updated_at,
         raw_json=@raw_json
       WHERE id=@id`,
    )
    .run({
      id: String(id),
      title: String(event.title || "").trim() || existing.title,
      detail: nullable(event.detail),
      starts_on: startsOn,
      starts_at: startsAt,
      ends_at: nullable(event.ends_at ?? event.endsAt),
      timezone: event.timezone || existing.timezone || JAPAN_TIME_ZONE,
      time_precision:
        event.time_precision ??
        event.timePrecision ??
        (startsAt ? "exact" : "unknown"),
      status: nullable(event.status),
      event_type: event.event_type ?? event.eventType ?? existing.eventType,
      url: nullable(event.url),
      updated_at: timestamp,
      raw_json: json({ ...event, provenance: "manual", manualLocked: true }),
    })
  insertEventSource(
    database,
    {
      ...event,
      source: "manual",
      source_item_id: String(id),
      source_key: "confirmed",
    },
    id,
    timestamp,
  )
  return getEvent(database, id)
}

export function deleteManualEvent(database, id) {
  const timestamp = nowIso()
  const result = database
    .prepare(
      `UPDATE events SET provenance='manual', manual_locked=1,
       deleted_at=?, updated_at=? WHERE id=?`,
    )
    .run(timestamp, timestamp, String(id))
  return result.changes > 0
}

export function replaceAutomaticEventsForSource(
  database,
  { source, sourceItemId, extractionId = null, events = [] },
) {
  const timestamp = nowIso()
  const replace = database.transaction(() => {
    const activeKeys = new Set()
    for (const event of events) {
      const sourceKey = String(event.source_key ?? event.sourceKey ?? event.id)
      activeKeys.add(sourceKey)
      const value = {
        ...event,
        source: event.canonicalSource || source,
        source_item_id: String(event.canonicalSourceItemId || sourceItemId),
        source_key: String(event.canonicalSourceKey || sourceKey),
        provenance: "automatic",
        manual_locked: 0,
        extraction_id: extractionId,
        deleted_at: null,
      }
      insertEvent(database, value, true)
      insertEventSource(
        database,
        {
          ...event,
          source,
          source_item_id: String(sourceItemId),
          source_key: sourceKey,
        },
        event.id,
        timestamp,
      )
    }

    const previous = database
      .prepare(
        `SELECT id, event_id AS eventId, source_key AS sourceKey
         FROM event_sources
         WHERE source = ? AND source_item_id = ? AND deleted_at IS NULL`,
      )
      .all(String(source), String(sourceItemId))
    const hideSource = database.prepare(
      "UPDATE event_sources SET deleted_at=?, updated_at=? WHERE id=?",
    )
    const activeSourceCount = database.prepare(
      "SELECT COUNT(*) AS count FROM event_sources WHERE event_id=? AND deleted_at IS NULL",
    )
    const hideEvent = database.prepare(
      `UPDATE events SET deleted_at=?, updated_at=?
       WHERE id=? AND manual_locked=0`,
    )
    let retired = 0
    for (const row of previous) {
      if (activeKeys.has(row.sourceKey)) continue
      hideSource.run(timestamp, timestamp, row.id)
      retired += 1
      if (activeSourceCount.get(row.eventId).count === 0) {
        hideEvent.run(timestamp, timestamp, row.eventId)
      }
    }
    return {
      upserted: events.length,
      retired,
    }
  })
  return replace()
}

export function getAppSetting(database, key, fallback = null) {
  return (
    database.prepare("SELECT value FROM app_settings WHERE key = ?").get(key)
      ?.value ?? fallback
  )
}

export function setAppSetting(database, key, value) {
  const timestamp = nowIso()
  database
    .prepare(
      `INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
       ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
    )
    .run(String(key), String(value), timestamp)
  return { key: String(key), value: String(value), updatedAt: timestamp }
}

function settingBoolean(value, fallback = true) {
  if (value == null || value === "") return fallback
  if (typeof value === "boolean") return value
  const normalized = String(value).trim().toLowerCase()
  if (["1", "true", "yes", "on"].includes(normalized)) return true
  if (["0", "false", "no", "off"].includes(normalized)) return false
  return fallback
}

function normalizedScheduleKeywords(value) {
  const values = parseListSetting(value, DEFAULT_SCHEDULE_KEYWORDS, {
    fallbackOnEmpty: false,
  })
  return [
    ...new Set(
      values
        .map((keyword) => String(keyword).trim())
        .filter((keyword) => keyword.length > 0 && keyword.length <= 80),
    ),
  ]
}

export function getScheduleExtractionConfig(database) {
  const enabledSetting = getAppSetting(
    database,
    "schedule_extraction_enabled",
    process.env.SCHEDULE_EXTRACTION_ENABLED ?? "1",
  )
  const keywordsSetting = getAppSetting(
    database,
    "schedule_keywords",
    process.env.SCHEDULE_KEYWORDS ?? DEFAULT_SCHEDULE_KEYWORDS,
  )
  return {
    enabled: settingBoolean(enabledSetting, true),
    keywords: normalizedScheduleKeywords(keywordsSetting),
  }
}

export function getConfiguredXHandles(database) {
  return configuredXHandles(database)
}

export function getFeaturedVideoId(database) {
  const configured = database
    .prepare("SELECT value FROM app_settings WHERE key = 'featured_video_id'")
    .get()?.value
  if (configured !== undefined) {
    const configuredId = String(configured || "") || null
    return configuredId && getVideoRecord(database, configuredId)
      ? configuredId
      : null
  }
  const legacyId =
    database
      .prepare("SELECT video_id AS videoId FROM focus ORDER BY id LIMIT 1")
      .get()?.videoId || null
  return legacyId && getVideoRecord(database, legacyId) ? legacyId : null
}

export function listAdminVideos(database, { limit = 100 } = {}) {
  const boundedLimit = Math.min(200, Math.max(1, Number(limit) || 100))
  const videos = database
    .prepare(
      `SELECT id, source, title, published_at AS publishedAt,
       scheduled_at AS scheduledAt, url, thumbnail_url AS thumbnailUrl,
       kind, is_upcoming AS isUpcoming
       FROM videos
       ORDER BY COALESCE(scheduled_at, published_at) DESC, id ASC
       LIMIT ?`,
    )
    .all(boundedLimit)
  const featuredVideoId = getFeaturedVideoId(database)
  if (
    featuredVideoId &&
    !videos.some((video) => video.id === featuredVideoId)
  ) {
    const featuredVideo = getVideoRecord(database, featuredVideoId)
    if (featuredVideo) videos.push(featuredVideo)
  }
  return videos.map((video) => ({
    ...video,
    isUpcoming: Boolean(video.isUpcoming),
  }))
}

export function setFeaturedVideoId(database, videoId) {
  const normalizedValue = String(videoId ?? "").trim()
  const normalized = nullable(normalizedValue)
  if (normalized && !getVideoRecord(database, normalized)) {
    throw new Error("featured video does not exist")
  }
  setAppSetting(database, "featured_video_id", normalized || "")
  return normalized
}

export function getSyncState(database, source, accountId) {
  const row = database
    .prepare(
      `SELECT source, account_id AS accountId, cursor_id AS cursorId,
       cursor_time AS cursorTime, last_success_at AS lastSuccessAt,
       metadata_json AS metadataJson
       FROM sync_state WHERE source = ? AND account_id = ?`,
    )
    .get(String(source), String(accountId))
  if (!row) return null
  const { metadataJson, ...state } = row
  return { ...state, metadata: parseJson(metadataJson) || {} }
}

export function upsertSyncState(
  database,
  { source, accountId, cursorId = null, cursorTime = null, metadata = {} },
) {
  const timestamp = nowIso()
  database
    .prepare(
      `INSERT INTO sync_state (
         source, account_id, cursor_id, cursor_time, last_success_at, metadata_json
       ) VALUES (?, ?, ?, ?, ?, ?)
       ON CONFLICT(source, account_id) DO UPDATE SET
         cursor_id=excluded.cursor_id, cursor_time=excluded.cursor_time,
         last_success_at=excluded.last_success_at,
         metadata_json=excluded.metadata_json`,
    )
    .run(
      String(source),
      String(accountId),
      nullable(cursorId),
      nullable(cursorTime),
      timestamp,
      json(metadata),
    )
  return getSyncState(database, source, accountId)
}

export function getScheduleExtraction(
  database,
  { source, sourceItemId, contentFingerprint, extractorVersion },
) {
  return (
    database
      .prepare(
        `SELECT id, source, source_item_id AS sourceItemId,
         content_fingerprint AS contentFingerprint,
         extractor_version AS extractorVersion, model, status,
         result_json AS resultJson, error, created_at AS createdAt,
         updated_at AS updatedAt
         FROM schedule_extractions
         WHERE source=? AND source_item_id=? AND content_fingerprint=?
           AND extractor_version=?`,
      )
      .get(
        String(source),
        String(sourceItemId),
        String(contentFingerprint),
        String(extractorVersion),
      ) || null
  )
}

export function upsertScheduleExtraction(
  database,
  {
    source,
    sourceItemId,
    contentFingerprint,
    extractorVersion,
    model,
    status,
    result = null,
    error = null,
  },
) {
  const timestamp = nowIso()
  database
    .prepare(
      `INSERT INTO schedule_extractions (
         source, source_item_id, content_fingerprint, extractor_version,
         model, status, result_json, error, created_at, updated_at
       ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(source, source_item_id, content_fingerprint, extractor_version)
       DO UPDATE SET model=excluded.model, status=excluded.status,
         result_json=excluded.result_json, error=excluded.error,
         updated_at=excluded.updated_at`,
    )
    .run(
      String(source),
      String(sourceItemId),
      String(contentFingerprint),
      String(extractorVersion),
      String(model),
      String(status),
      json(result),
      nullable(error),
      timestamp,
      timestamp,
    )
  return getScheduleExtraction(database, {
    source,
    sourceItemId,
    contentFingerprint,
    extractorVersion,
  })
}

export function getKnownPostIds(database, ids = []) {
  const normalized = [...new Set(ids.map(String))]
  if (!normalized.length) return new Set()
  const placeholders = normalized.map(() => "?").join(", ")
  return new Set(
    database
      .prepare(`SELECT id FROM posts WHERE id IN (${placeholders})`)
      .all(...normalized)
      .map((row) => String(row.id)),
  )
}

export function listScheduleCandidatePosts(
  database,
  { limit = 20, keywords = null } = {},
) {
  const boundedLimit = Math.min(100, Math.max(1, Number(limit) || 20))
  const configuredKeywords = normalizedScheduleKeywords(
    keywords == null
      ? getScheduleExtractionConfig(database).keywords
      : keywords,
  )
  const loweredKeywords = configuredKeywords.map((keyword) =>
    keyword.toLowerCase(),
  )
  return database
    .prepare(
      `SELECT id, source, label, text, published_at AS publishedAt, url,
       account_handle AS accountHandle,
       media_url AS mediaUrl, media_alt AS mediaAlt, raw_json AS rawJson
       FROM posts
       WHERE source='x'
       ORDER BY published_at DESC`,
    )
    .all()
    .map(({ rawJson, ...post }) => ({
      ...post,
      raw: parseJson(rawJson),
    }))
    .filter((post) => {
      const haystack = String(post.text || "").toLocaleLowerCase()
      return loweredKeywords.some((keyword) => haystack.includes(keyword))
    })
    .slice(0, boundedLimit)
}

export function getVideoRecord(database, id) {
  return (
    database
      .prepare(
        `SELECT id, source, title, published_at AS publishedAt,
         scheduled_at AS scheduledAt, url, thumbnail_url AS thumbnailUrl,
         kind, is_upcoming AS isUpcoming
         FROM videos WHERE id = ?`,
      )
      .get(String(id)) || null
  )
}

export function listActiveVideoIds(database, { limit = 20 } = {}) {
  const boundedLimit = Math.min(100, Math.max(1, Number(limit) || 20))
  return database
    .prepare(
      `SELECT id FROM videos
       WHERE is_upcoming=1 OR scheduled_at IS NOT NULL
       ORDER BY COALESCE(scheduled_at, published_at) DESC LIMIT ?`,
    )
    .all(boundedLimit)
    .map((row) => String(row.id))
}

export function startSyncRun(
  database,
  source,
  startedAt = new Date().toISOString(),
) {
  const result = database
    .prepare(
      `INSERT INTO sync_runs (source, started_at, status) VALUES (?, ?, 'running')`,
    )
    .run(source, startedAt)
  return Number(result.lastInsertRowid)
}

export function finishSyncRun(
  database,
  id,
  { status, message = "", counts = {} } = {},
) {
  database
    .prepare(
      `UPDATE sync_runs SET finished_at = ?, status = ?, message = ?, counts_json = ? WHERE id = ?`,
    )
    .run(new Date().toISOString(), status, message, JSON.stringify(counts), id)
}

function parseJson(value) {
  if (!value) return undefined
  try {
    return JSON.parse(value)
  } catch {
    return undefined
  }
}

function mapProfile(row) {
  if (!row) return null
  const { raw_json: _raw, ...profile } = row
  return profile
}

function mapRows(rows) {
  return rows.map(({ raw_json: _raw, ...row }) => row)
}

export function getLatestSync(database) {
  const row = database
    .prepare(
      `SELECT id, source, started_at AS startedAt, finished_at AS finishedAt, status, message, counts_json AS countsJson
    FROM sync_runs WHERE status != 'running' ORDER BY COALESCE(finished_at, started_at) DESC LIMIT 1`,
    )
    .get()
  if (!row) return null
  const { countsJson: _countsJson, ...summary } = row
  return { ...summary, counts: parseJson(row.countsJson) || {} }
}

export function getDashboard(database, { days = 3, now = new Date() } = {}) {
  const windowStart = now.getTime() - days * 24 * 60 * 60 * 1000
  const mediaRows = listMediaAssets(database)
  const mediaBySourceUrl = new Map(
    mediaRows.map((asset) => [asset.sourceUrl, asset]),
  )
  const mediaById = new Map(mediaRows.map((asset) => [asset.id, asset]))
  const postMediaLinks = listMediaLinks(database, { ownerType: "post" }).filter(
    (link) => link.role === "post-image",
  )
  const postMediaByOwner = new Map()
  for (const link of postMediaLinks) {
    if (!postMediaByOwner.has(link.ownerId))
      postMediaByOwner.set(link.ownerId, [])
    postMediaByOwner.get(link.ownerId).push(link)
  }
  const posts = database
    .prepare(
      `SELECT id, source, account_handle AS accountHandle, type, label, text,
       published_at AS publishedAt, url, likes, reposts, replies,
       media_url AS mediaUrl, media_alt AS mediaAlt
    FROM posts ORDER BY published_at DESC`,
    )
    .all()
    .filter((post) => {
      const timestamp = Date.parse(post.publishedAt)
      return Number.isNaN(timestamp) || timestamp >= windowStart
    })
    .map((post) => {
      const accountHandle =
        post.accountHandle ||
        (post.source === "x" ? xHandleFromUrl(post.url) : null)
      const fallbackMedia = resolveMediaReference(
        database,
        post.mediaUrl,
        mediaBySourceUrl,
      )
      const linkedMedia = (postMediaByOwner.get(String(post.id)) || [])
        .sort((a, b) => a.position - b.position)
        .map((link) => {
          const media = mediaReferenceForAsset(mediaById.get(link.mediaId))
          if (!media) return null
          return {
            id: media.id,
            url: media.publicUrl,
            status: media.status,
            sourceUrl: media.sourceUrl,
            alt: link.alt || post.mediaAlt || null,
            position: link.position,
            width: media.width,
            height: media.height,
          }
        })
        .filter(Boolean)
      const media = linkedMedia.length
        ? linkedMedia
        : post.mediaUrl
          ? [
              {
                id: fallbackMedia.id,
                url: fallbackMedia.publicUrl,
                status: fallbackMedia.status,
                sourceUrl: fallbackMedia.sourceUrl,
                alt: post.mediaAlt || null,
                position: 0,
                width: fallbackMedia.width ?? null,
                height: fallbackMedia.height ?? null,
              },
            ]
          : []
      const primaryMedia = media[0] || fallbackMedia
      return {
        ...post,
        accountHandle,
        accountUrl:
          accountHandle && post.source === "x"
            ? `https://x.com/${accountHandle}`
            : null,
        media,
        mediaUrl: primaryMedia.url ?? primaryMedia.publicUrl ?? null,
        mediaId: primaryMedia.id,
        mediaStatus: primaryMedia.status,
        mediaSourceUrl: primaryMedia.sourceUrl,
      }
    })

  const todayKey = dateKeyInJapan(now)
  const events = database
    .prepare(
      `SELECT id, source, source_item_id AS sourceItemId, title, detail,
       starts_on AS startsOn, starts_at AS startsAt, ends_at AS endsAt,
       timezone, time_precision AS timePrecision, status,
       event_type AS eventType, url, provenance, manual_locked AS manualLocked
       FROM events WHERE deleted_at IS NULL
       ORDER BY starts_on ASC, COALESCE(starts_at, starts_on) ASC`,
    )
    .all()
    .map((event) => ({
      ...event,
      manualLocked: Boolean(event.manualLocked),
      isUpcoming:
        event.startsAt && !Number.isNaN(Date.parse(event.startsAt))
          ? Date.parse(event.startsAt) >= now.getTime()
          : Boolean(event.startsOn && todayKey && event.startsOn >= todayKey),
    }))

  const featuredVideoId = getFeaturedVideoId(database)
  const videos = database
    .prepare(
      `SELECT id, source, title, published_at AS publishedAt, scheduled_at AS scheduledAt, url,
    thumbnail_url AS thumbnailUrl, kind, is_upcoming AS isUpcoming FROM videos
    ORDER BY COALESCE(scheduled_at, published_at) DESC LIMIT 30`,
    )
    .all()
  if (
    featuredVideoId &&
    !videos.some((video) => video.id === featuredVideoId)
  ) {
    const featuredRow = getVideoRecord(database, featuredVideoId)
    if (featuredRow) videos.push(featuredRow)
  }
  const mappedVideos = videos
    .map((video) => ({
      ...video,
      isUpcoming: video.scheduledAt
        ? Date.parse(video.scheduledAt) >= now.getTime()
        : Boolean(video.isUpcoming),
    }))
    .map((video) => {
      const media = resolveMediaReference(
        database,
        video.thumbnailUrl,
        mediaBySourceUrl,
      )
      return {
        ...video,
        thumbnailUrl: media.publicUrl,
        thumbnailId: media.id,
        thumbnailStatus: media.status,
        thumbnailSourceUrl: media.sourceUrl,
      }
    })

  const focusRow = database
    .prepare(
      `SELECT id, video_id AS videoId, date_label AS dateLabel, title,
       description, image_url AS imageUrl, url, source_url AS sourceUrl,
       updated_at AS updatedAt FROM focus ORDER BY id LIMIT 1`,
    )
    .get()
  const focus = focusRow
    ? (() => {
        const media = resolveMediaReference(
          database,
          focusRow.imageUrl,
          mediaBySourceUrl,
        )
        return {
          ...focusRow,
          imageUrl: media.publicUrl,
          imageId: media.id,
          imageStatus: media.status,
          imageSourceUrl: media.sourceUrl,
        }
      })()
    : null
  const timeline = database
    .prepare(
      `SELECT id, year, title, detail, sort_order AS sortOrder FROM timeline ORDER BY sort_order ASC, id ASC`,
    )
    .all()
  const resources = database
    .prepare(
      `SELECT id, title, detail, icon, tone, url, sort_order AS sortOrder FROM resources ORDER BY sort_order ASC, id ASC`,
    )
    .all()
  const assets = database
    .prepare(
      `SELECT id, kind, url, source_url AS sourceUrl, alt, updated_at AS updatedAt FROM assets ORDER BY id ASC`,
    )
    .all()
    .map((asset) => {
      const media = resolveMediaReference(database, asset.url, mediaBySourceUrl)
      return {
        ...asset,
        url: media.publicUrl,
        sourceUrl: asset.sourceUrl || media.sourceUrl,
        mediaId: media.id,
        mediaStatus: media.status,
        mediaSourceUrl: media.sourceUrl,
      }
    })
  const profileRow = mapProfile(
    database
      .prepare(
        `SELECT id, display_name AS displayName, romanized_name AS romanizedName, bio, avatar_url AS avatarUrl, banner_url AS bannerUrl, x_url AS xUrl, youtube_url AS youtubeUrl, updated_at AS updatedAt FROM profiles ORDER BY id LIMIT 1`,
      )
      .get(),
  )
  const profile = profileRow
    ? (() => {
        const avatar = resolveMediaReference(
          database,
          profileRow.avatarUrl,
          mediaBySourceUrl,
        )
        const banner = resolveMediaReference(
          database,
          profileRow.bannerUrl,
          mediaBySourceUrl,
        )
        return {
          ...profileRow,
          avatarUrl: avatar.publicUrl,
          avatarId: avatar.id,
          avatarStatus: avatar.status,
          avatarSourceUrl: avatar.sourceUrl,
          bannerUrl: banner.publicUrl,
          bannerId: banner.id,
          bannerStatus: banner.status,
          bannerSourceUrl: banner.sourceUrl,
        }
      })()
    : null
  const latestSync = getLatestSync(database)
  const mediaCache = mediaRows.reduce(
    (summary, asset) => {
      const media = mediaReferenceForAsset(asset)
      const status = media?.status || asset.status
      summary.total += 1
      summary[status] = (summary[status] || 0) + 1
      return summary
    },
    { total: 0 },
  )

  const dateCandidates = [
    profile?.updatedAt,
    focusRow?.updatedAt,
    ...posts.map((post) => post.publishedAt),
    ...mappedVideos.map((video) => video.publishedAt),
  ]
    .map((value) => Date.parse(value || ""))
    .filter((value) => !Number.isNaN(value))
  const fetchedAt =
    latestSync?.finishedAt ||
    (dateCandidates.length
      ? new Date(Math.max(...dateCandidates)).toISOString()
      : null)

  return {
    profile,
    posts,
    events,
    videos: mappedVideos,
    focus,
    timeline,
    resources,
    assets,
    meta: {
      fetchedAt,
      lastSync: latestSync,
      postWindowDays: days,
      generatedAt: now.toISOString(),
      mediaCache,
      featuredVideoId,
      xAccounts: configuredXHandles(database),
    },
  }
}
