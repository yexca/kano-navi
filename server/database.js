import crypto from "node:crypto"
import fs from "node:fs"
import path from "node:path"
import Database from "better-sqlite3"
import { isLikelyScheduleBoardText } from "./schedule-asset.js"
import { fileURLToPath } from "node:url"
import { seedData } from "./seed-data.js"
import {
  WORKFLOW_STEP_IDS,
  clampWorkflowInterval,
  normalizeWorkflowSteps,
} from "./workflow-catalog.js"
import {
  avatarMediaDirectory,
  legacyMediaCacheDirectory,
  MEDIA_STATUS,
  cacheRelativePathForHash,
  ensureMediaCacheDirectories,
  extensionForMimeType,
  isSafeContentHash,
  isSafeMediaId,
  mediaNamespaceForSource,
  mediaIdForSourceUrl,
  xMediaDirectory,
  youtubeMediaDirectory,
  profileSlotRelativePath,
  normalizeMediaMimeType,
  normalizeSourceUrl,
  publicAssetExists,
  publicMediaUrl,
  resolveMediaCachePath,
  mediaCacheTempDirectory,
  sanitizeExtension,
} from "./media-cache.js"

// A provider gets one initial request plus two retries before failover.
export const LLM_MAX_RETRIES = 2

const moduleDir = path.dirname(fileURLToPath(import.meta.url))
const projectDir = path.resolve(moduleDir, "..")

export const dataDirectory = path.join(projectDir, "data")
export const databaseDirectory = path.join(dataDirectory, "database")
export const databasePath = path.join(databaseDirectory, "kano.sqlite")
const legacyDatabasePath = path.join(dataDirectory, "kano.sqlite")

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
    cancellation_status TEXT NOT NULL DEFAULT 'none',
    cancellation_source TEXT,
    cancellation_reason TEXT,
    cancellation_evidence TEXT,
    cancellation_source_item_id TEXT,
    cancellation_confidence REAL,
    cancellation_at TEXT,
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

  CREATE TABLE IF NOT EXISTS post_llm_states (
    post_id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'never',
    route TEXT,
    last_attempt_at TEXT,
    last_processed_at TEXT,
    last_error TEXT,
    last_extraction_id INTEGER,
    reprocess_requested INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
  );

  CREATE INDEX IF NOT EXISTS post_llm_states_status_idx
    ON post_llm_states (status, reprocess_requested, updated_at DESC);

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
    week_start TEXT,
    source_account TEXT,
    updated_at TEXT,
    raw_json TEXT
  );

  CREATE TABLE IF NOT EXISTS schedule_asset_reviews (
    asset_id TEXT PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    llm_status TEXT NOT NULL DEFAULT 'pending',
    llm_confidence REAL,
    llm_reason TEXT,
    llm_evidence TEXT,
    llm_model TEXT,
    llm_checked_at TEXT,
    manual_status TEXT NOT NULL DEFAULT 'unreviewed',
    manual_reason TEXT,
    manual_checked_at TEXT,
    updated_at TEXT NOT NULL
  );

  CREATE INDEX IF NOT EXISTS schedule_asset_reviews_status_idx
    ON schedule_asset_reviews (llm_status, manual_status, updated_at DESC);

  CREATE TABLE IF NOT EXISTS profile_media (
    id TEXT PRIMARY KEY,
    slot TEXT NOT NULL,
    source TEXT NOT NULL,
    source_ref TEXT,
    source_url TEXT,
    cache_path TEXT,
    active_cache_path TEXT,
    mime_type TEXT,
    extension TEXT,
    byte_size INTEGER,
    sha256 TEXT,
    width INTEGER,
    height INTEGER,
    status TEXT NOT NULL DEFAULT 'pending',
    is_active INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    raw_json TEXT
  );

  CREATE INDEX IF NOT EXISTS profile_media_slot_idx
    ON profile_media (slot, is_active, updated_at DESC);

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
    counts_json TEXT,
    triggered_by TEXT,
    job_id TEXT
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

  CREATE TABLE IF NOT EXISTS llm_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    protocol TEXT NOT NULL DEFAULT 'openai-responses',
    base_url TEXT NOT NULL,
    model TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    vision_capable INTEGER NOT NULL DEFAULT 1,
    capabilities_json TEXT NOT NULL DEFAULT '["text","image"]',
    timeout_ms INTEGER NOT NULL DEFAULT 30000,
    max_retries INTEGER NOT NULL DEFAULT ${LLM_MAX_RETRIES},
    api_key_ciphertext TEXT,
    last_status TEXT,
    last_error TEXT,
    last_checked_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );

  CREATE TABLE IF NOT EXISTS llm_route_providers (
    route TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES llm_providers (id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (route, provider_id),
    UNIQUE (route, priority)
  );

  CREATE INDEX IF NOT EXISTS llm_route_providers_order_idx
    ON llm_route_providers (route, priority);

  CREATE TABLE IF NOT EXISTS llm_models (
    provider_id TEXT NOT NULL REFERENCES llm_providers (id) ON DELETE CASCADE,
    model_id TEXT NOT NULL,
    name TEXT,
    tags_json TEXT NOT NULL DEFAULT '["text"]',
    enabled INTEGER NOT NULL DEFAULT 1,
    origin TEXT NOT NULL DEFAULT 'manual',
    owned_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (provider_id, model_id)
  );

  CREATE TABLE IF NOT EXISTS llm_route_targets (
    route TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES llm_providers (id) ON DELETE CASCADE,
    model_id TEXT NOT NULL DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (route, provider_id, model_id),
    UNIQUE (route, priority)
  );

  CREATE TABLE IF NOT EXISTS workflows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    steps_json TEXT NOT NULL,
    schedule_enabled INTEGER NOT NULL DEFAULT 0,
    interval_minutes INTEGER NOT NULL DEFAULT 60,
    last_run_at TEXT,
    last_job_id TEXT,
    last_status TEXT,
    next_run_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );
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
export const SCHEDULE_VISION_ROUTE = "schedule_vision"
export const SCHEDULE_MESSAGE_ROUTE = "schedule_message"
export const SCHEDULE_BOARD_ROUTE = "schedule_board"
export const SCHEDULE_ROUTES = [
  SCHEDULE_MESSAGE_ROUTE,
  SCHEDULE_BOARD_ROUTE,
  SCHEDULE_VISION_ROUTE,
]
export const LLM_CAPABILITIES = ["text", "image"]
// Model tags shown in the provider center. `text` and `image` are routing
// capabilities; the rest are descriptive labels for operators.
export const LLM_MODEL_TAGS = [
  "text",
  "image",
  "reasoning",
  "tools",
  "embedding",
]
export const LLM_MODEL_ID_PATTERN = /^[A-Za-z0-9@][A-Za-z0-9._:/@+-]{0,199}$/u
export const LLM_PROTOCOLS = ["openai-responses", "openai-chat-completions"]
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

/**
 * Normalize the public LLM input capability vocabulary. The persisted
 * `vision_capable` flag remains a migration/response compatibility field, but
 * routing is based on this explicit set.
 */
export function normalizeLlmCapabilities(
  value,
  legacyVisionCapable = true,
  { fallback = legacyVisionCapable ? LLM_CAPABILITIES : ["text"] } = {},
) {
  let values = value
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value)
      values = Array.isArray(parsed) ? parsed : value.split(/[,\s]+/u)
    } catch {
      values = value.split(/[,\s]+/u)
    }
  }
  if (!Array.isArray(values)) values = []
  const normalized = [
    ...new Set(
      values
        .map((item) =>
          String(item || "")
            .trim()
            .toLowerCase(),
        )
        .map((item) => (item === "vision" ? "image" : item))
        .filter((item) => LLM_CAPABILITIES.includes(item)),
    ),
  ]
  return normalized.length ? normalized : [...fallback]
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
      cancellation_status TEXT NOT NULL DEFAULT 'none',
      cancellation_source TEXT,
      cancellation_reason TEXT,
      cancellation_evidence TEXT,
      cancellation_source_item_id TEXT,
      cancellation_confidence REAL,
      cancellation_at TEXT,
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
      cancellation_status, cancellation_source, cancellation_reason,
      cancellation_evidence, cancellation_source_item_id,
      cancellation_confidence, cancellation_at,
      deleted_at, created_at, updated_at, raw_json
    ) VALUES (
      @id, @source, @source_item_id, @source_key, @title, @detail, @starts_on,
      @starts_at, @ends_at, @timezone, @time_precision, @status, @event_type,
      @url, @is_upcoming, @provenance, @manual_locked, @confidence,
      @extraction_id, @cancellation_status, @cancellation_source,
      @cancellation_reason, @cancellation_evidence,
      @cancellation_source_item_id, @cancellation_confidence,
      @cancellation_at, @deleted_at, @created_at, @updated_at, @raw_json
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
        cancellation_status: "none",
        cancellation_source: null,
        cancellation_reason: null,
        cancellation_evidence: null,
        cancellation_source_item_id: null,
        cancellation_confidence: null,
        cancellation_at: null,
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
  // The seeded resource used to imply that the official site was obsolete.
  // Update only that exact seeded wording so operator-edited resource records
  // remain untouched while existing databases receive the corrected copy.
  database
    .prepare(
      `UPDATE resources
       SET title = 'official site', detail = NULL
       WHERE id = 9
         AND title = '旧 official site'
         AND detail = '更新截至 2022 · 资料存档'`,
    )
    .run()
  // The first seeded milestones said the 2026 move "started" VTuber activity
  // and linked a news URL instead of the agency talent page. Correct only rows
  // that still carry the exact seeded values, and make room for the 2019/2021
  // milestones that the next seed pass inserts.
  database
    .prepare(
      `UPDATE timeline
       SET detail = '8 月加入ミリプロSONA，以新形象开始配信。'
       WHERE id = 1 AND detail = '加入ミリプロSONA，开始 VTuber 活动。'`,
    )
    .run()
  database
    .prepare(
      `UPDATE timeline
       SET sort_order = CASE id WHEN 3 THEN 5 ELSE 6 END
       WHERE (id = 3 AND title = 'Major debut' AND sort_order = 3)
          OR (id = 4 AND title = '第一首投稿' AND sort_order = 4)`,
    )
    .run()
  database
    .prepare(
      `UPDATE resources
       SET url = 'https://milpr.com/talents/kano-mahoro',
           detail = '事务所个人页 · 资料与公告'
       WHERE id = 7
         AND url = 'https://milpr.com/news/mahoro_debut'
         AND detail = '所属与官方公告'`,
    )
    .run()
  const eventColumns = tableColumns(database, "events")
  const eventAdditions = [
    [
      "cancellation_status",
      "ALTER TABLE events ADD COLUMN cancellation_status TEXT NOT NULL DEFAULT 'none'",
    ],
    [
      "cancellation_source",
      "ALTER TABLE events ADD COLUMN cancellation_source TEXT",
    ],
    [
      "cancellation_reason",
      "ALTER TABLE events ADD COLUMN cancellation_reason TEXT",
    ],
    [
      "cancellation_evidence",
      "ALTER TABLE events ADD COLUMN cancellation_evidence TEXT",
    ],
    [
      "cancellation_source_item_id",
      "ALTER TABLE events ADD COLUMN cancellation_source_item_id TEXT",
    ],
    [
      "cancellation_confidence",
      "ALTER TABLE events ADD COLUMN cancellation_confidence REAL",
    ],
    ["cancellation_at", "ALTER TABLE events ADD COLUMN cancellation_at TEXT"],
  ]
  for (const [column, statement] of eventAdditions) {
    if (!eventColumns.has(column)) database.exec(statement)
  }
  database.exec(`
    CREATE TABLE IF NOT EXISTS post_llm_states (
      post_id TEXT PRIMARY KEY,
      status TEXT NOT NULL DEFAULT 'never',
      route TEXT,
      last_attempt_at TEXT,
      last_processed_at TEXT,
      last_error TEXT,
      last_extraction_id INTEGER,
      reprocess_requested INTEGER NOT NULL DEFAULT 0,
      updated_at TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS post_llm_states_status_idx
      ON post_llm_states (status, reprocess_requested, updated_at DESC);
    CREATE TABLE IF NOT EXISTS schedule_asset_reviews (
      asset_id TEXT PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
      llm_status TEXT NOT NULL DEFAULT 'pending',
      llm_confidence REAL,
      llm_reason TEXT,
      llm_evidence TEXT,
      llm_model TEXT,
      llm_checked_at TEXT,
      manual_status TEXT NOT NULL DEFAULT 'unreviewed',
      manual_reason TEXT,
      manual_checked_at TEXT,
      updated_at TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS schedule_asset_reviews_status_idx
      ON schedule_asset_reviews (llm_status, manual_status, updated_at DESC);
  `)
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

  const assetColumns = tableColumns(database, "assets")
  if (!assetColumns.has("week_start")) {
    database.exec("ALTER TABLE assets ADD COLUMN week_start TEXT")
  }
  if (!assetColumns.has("source_account")) {
    database.exec("ALTER TABLE assets ADD COLUMN source_account TEXT")
  }

  const syncRunColumns = tableColumns(database, "sync_runs")
  if (!syncRunColumns.has("triggered_by")) {
    database.exec("ALTER TABLE sync_runs ADD COLUMN triggered_by TEXT")
  }
  if (!syncRunColumns.has("job_id")) {
    database.exec("ALTER TABLE sync_runs ADD COLUMN job_id TEXT")
  }
  database.exec(
    "CREATE INDEX IF NOT EXISTS sync_runs_job_idx ON sync_runs (job_id, id DESC)",
  )

  const providerColumns = tableColumns(database, "llm_providers")
  if (!providerColumns.has("max_retries")) {
    database.exec(
      `ALTER TABLE llm_providers ADD COLUMN max_retries INTEGER NOT NULL DEFAULT ${LLM_MAX_RETRIES}`,
    )
  }
  let addedCapabilitiesColumn = false
  if (!providerColumns.has("capabilities_json")) {
    database.exec(
      'ALTER TABLE llm_providers ADD COLUMN capabilities_json TEXT NOT NULL DEFAULT \'["text","image"]\'',
    )
    addedCapabilitiesColumn = true
  }
  const providerRows = database
    .prepare(
      "SELECT id, vision_capable AS visionCapable, capabilities_json AS capabilitiesJson FROM llm_providers",
    )
    .all()
  const updateCapabilities = database.prepare(
    "UPDATE llm_providers SET capabilities_json = ? WHERE id = ?",
  )
  for (const row of providerRows) {
    const capabilities = normalizeLlmCapabilities(
      addedCapabilitiesColumn ? null : row.capabilitiesJson,
      row.visionCapable !== 0,
    )
    updateCapabilities.run(JSON.stringify(capabilities), row.id)
  }

  // Older databases defaulted this field to zero. Keep the value as a
  // compatibility field, but make the failover contract deterministic.
  database
    .prepare(
      "UPDATE llm_providers SET max_retries = ? WHERE max_retries IS NULL OR max_retries <> ?",
    )
    .run(LLM_MAX_RETRIES, LLM_MAX_RETRIES)

  // Routes now target a provider plus a catalog model. Copy the older
  // provider-only order once; an empty model ID means "the provider's
  // default model" so legacy clients keep their semantics.
  if (getAppSetting(database, "llm_route_targets_migrated", null) == null) {
    const migrateTargets = database.transaction(() => {
      const legacyRows = database
        .prepare(
          `SELECT route, provider_id AS providerId, priority, created_at AS createdAt
           FROM llm_route_providers ORDER BY route, priority`,
        )
        .all()
      const insertTarget = database.prepare(
        `INSERT OR IGNORE INTO llm_route_targets
         (route, provider_id, model_id, priority, created_at, updated_at)
         VALUES (?, ?, '', ?, ?, ?)`,
      )
      for (const row of legacyRows) {
        insertTarget.run(
          row.route,
          row.providerId,
          row.priority,
          row.createdAt,
          row.createdAt,
        )
      }
      setAppSetting(database, "llm_route_targets_migrated", "1")
    })
    migrateTargets()
  }
  // Every provider default model also appears in the model catalog.
  const insertDefaultModel = database.prepare(
    `INSERT OR IGNORE INTO llm_models
     (provider_id, model_id, name, tags_json, enabled, origin, owned_by, created_at, updated_at)
     VALUES (?, ?, NULL, ?, 1, 'legacy', NULL, ?, ?)`,
  )
  for (const row of database
    .prepare(
      "SELECT id, model, capabilities_json AS capabilitiesJson, created_at AS createdAt FROM llm_providers WHERE model <> ''",
    )
    .all()) {
    insertDefaultModel.run(
      row.id,
      row.model,
      JSON.stringify(normalizeLlmCapabilities(row.capabilitiesJson)),
      row.createdAt,
      row.createdAt,
    )
  }

  // The original implementation had one vision route. New installations and
  // upgrades keep that order for both detectors until an operator separates
  // them explicitly in the admin API.
  const legacyTargets = getLlmRouteTargets(database, SCHEDULE_VISION_ROUTE)
  for (const route of [SCHEDULE_MESSAGE_ROUTE, SCHEDULE_BOARD_ROUTE]) {
    const current = database
      .prepare("SELECT 1 FROM llm_route_targets WHERE route = ? LIMIT 1")
      .get(route)
    if (!current && legacyTargets.length) {
      setLlmRouteTargets(database, route, legacyTargets)
    }
  }
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
  if (filename !== ":memory:") {
    const resolvedFilename = path.resolve(filename)
    fs.mkdirSync(path.dirname(resolvedFilename), { recursive: true })
    // Move the pre-namespace database once when upgrading an existing local
    // checkout. SQLite WAL sidecars travel with it when present.
    if (
      resolvedFilename === path.resolve(databasePath) &&
      !fs.existsSync(resolvedFilename) &&
      fs.existsSync(legacyDatabasePath)
    ) {
      fs.renameSync(legacyDatabasePath, resolvedFilename)
      for (const suffix of ["-wal", "-shm"]) {
        const oldSidecar = `${legacyDatabasePath}${suffix}`
        const newSidecar = `${resolvedFilename}${suffix}`
        if (fs.existsSync(oldSidecar) && !fs.existsSync(newSidecar))
          fs.renameSync(oldSidecar, newSidecar)
      }
    }
  }
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
         provenance, manual_locked, confidence, extraction_id,
         cancellation_status, cancellation_source, cancellation_reason,
         cancellation_evidence, cancellation_source_item_id,
         cancellation_confidence, cancellation_at, deleted_at,
         created_at, updated_at, raw_json
       ) VALUES (
         @id, @source, @source_item_id, @source_key, @title, @detail, @starts_on,
         @starts_at, @ends_at, @timezone, @time_precision, @status, @event_type,
         @url, @is_upcoming, @provenance, @manual_locked, @confidence,
         @extraction_id, @cancellation_status, @cancellation_source,
         @cancellation_reason, @cancellation_evidence,
         @cancellation_source_item_id, @cancellation_confidence,
         @cancellation_at, @deleted_at, @created_at, @updated_at, @raw_json
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
         cancellation_status=CASE WHEN events.cancellation_status='llm_suspected'
           THEN events.cancellation_status ELSE excluded.cancellation_status END,
         cancellation_source=CASE WHEN events.cancellation_status='llm_suspected'
           THEN events.cancellation_source ELSE excluded.cancellation_source END,
         cancellation_reason=CASE WHEN events.cancellation_status='llm_suspected'
           THEN events.cancellation_reason ELSE excluded.cancellation_reason END,
         cancellation_evidence=CASE WHEN events.cancellation_status='llm_suspected'
           THEN events.cancellation_evidence ELSE excluded.cancellation_evidence END,
         cancellation_source_item_id=CASE WHEN events.cancellation_status='llm_suspected'
           THEN events.cancellation_source_item_id ELSE excluded.cancellation_source_item_id END,
         cancellation_confidence=CASE WHEN events.cancellation_status='llm_suspected'
           THEN events.cancellation_confidence ELSE excluded.cancellation_confidence END,
         cancellation_at=CASE WHEN events.cancellation_status='llm_suspected'
           THEN events.cancellation_at ELSE excluded.cancellation_at END,
         updated_at=excluded.updated_at, raw_json=excluded.raw_json
       WHERE events.manual_locked = 0`
    : `INSERT OR IGNORE INTO events (
         id, source, source_item_id, source_key, title, detail, starts_on, starts_at,
         ends_at, timezone, time_precision, status, event_type, url, is_upcoming,
         provenance, manual_locked, confidence, extraction_id,
         cancellation_status, cancellation_source, cancellation_reason,
         cancellation_evidence, cancellation_source_item_id,
         cancellation_confidence, cancellation_at, deleted_at,
         created_at, updated_at, raw_json
       ) VALUES (
         @id, @source, @source_item_id, @source_key, @title, @detail, @starts_on,
         @starts_at, @ends_at, @timezone, @time_precision, @status, @event_type,
         @url, @is_upcoming, @provenance, @manual_locked, @confidence,
         @extraction_id, @cancellation_status, @cancellation_source,
         @cancellation_reason, @cancellation_evidence,
         @cancellation_source_item_id, @cancellation_confidence,
         @cancellation_at, @deleted_at, @created_at, @updated_at, @raw_json
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
    cancellation_status:
      nullable(event.cancellation_status ?? event.cancellationStatus) || "none",
    cancellation_source: nullable(
      event.cancellation_source ?? event.cancellationSource,
    ),
    cancellation_reason: nullable(
      event.cancellation_reason ?? event.cancellationReason,
    ),
    cancellation_evidence: nullable(
      event.cancellation_evidence ?? event.cancellationEvidence,
    ),
    cancellation_source_item_id: nullable(
      event.cancellation_source_item_id ?? event.cancellationSourceItemId,
    ),
    cancellation_confidence:
      event.cancellation_confidence != null ||
      event.cancellationConfidence != null
        ? Number.isFinite(
            Number(
              event.cancellation_confidence ?? event.cancellationConfidence,
            ),
          )
          ? Number(
              event.cancellation_confidence ?? event.cancellationConfidence,
            )
          : null
        : null,
    cancellation_at: nullable(event.cancellation_at ?? event.cancellationAt),
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
    ? `INSERT INTO assets (id, kind, url, source_url, alt, week_start, source_account, updated_at, raw_json)
       VALUES (@id, @kind, @url, @source_url, @alt, @week_start, @source_account, @updated_at, @raw_json)
       ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, url=excluded.url, source_url=excluded.source_url,
       alt=excluded.alt, week_start=excluded.week_start, source_account=excluded.source_account,
       updated_at=excluded.updated_at, raw_json=excluded.raw_json`
    : `INSERT OR IGNORE INTO assets (id, kind, url, source_url, alt, week_start, source_account, updated_at, raw_json)
       VALUES (@id, @kind, @url, @source_url, @alt, @week_start, @source_account, @updated_at, @raw_json)`
  database.prepare(sql).run({
    id: String(asset.id),
    kind: asset.kind || "image",
    url: asset.url || "",
    source_url: nullable(asset.source_url),
    alt: nullable(asset.alt),
    week_start: nullable(asset.week_start ?? asset.weekStart),
    source_account: nullable(asset.source_account ?? asset.sourceAccount),
    updated_at: nullable(asset.updated_at),
    raw_json: json(asset),
  })
}

const SCHEDULE_ASSET_LLM_STATUSES = new Set([
  "pending",
  "running",
  "schedule",
  "not_schedule",
  "uncertain",
  "failed",
  "skipped",
])
const SCHEDULE_ASSET_MANUAL_STATUSES = new Set([
  "unreviewed",
  "schedule",
  "not_schedule",
])

function normalizedScheduleAssetLlmStatus(value) {
  const normalized = String(value || "pending")
    .trim()
    .toLowerCase()
  return SCHEDULE_ASSET_LLM_STATUSES.has(normalized) ? normalized : "pending"
}

function normalizedScheduleAssetManualStatus(value) {
  const normalized = String(value || "unreviewed")
    .trim()
    .toLowerCase()
  return SCHEDULE_ASSET_MANUAL_STATUSES.has(normalized)
    ? normalized
    : "unreviewed"
}

/**
 * A schedule image is public only when an operator marked it as a schedule
 * board, or, without a manual label, when its source post still reads as a
 * board notice and the image-only LLM check returned `schedule`. A missing
 * source post leaves the decision to the image check alone.
 */
function scheduleAssetReviewRow(row) {
  if (!row) return null
  const { sourceText, ...review } = row
  const manualStatus = normalizedScheduleAssetManualStatus(row.manualStatus)
  const llmStatus = normalizedScheduleAssetLlmStatus(row.llmStatus)
  const effectiveStatus =
    manualStatus === "unreviewed" ? llmStatus : manualStatus
  const sourceMatchesBoard =
    sourceText == null || isLikelyScheduleBoardText(sourceText)
  return {
    ...review,
    llmStatus,
    llmConfidence: row.llmConfidence == null ? null : Number(row.llmConfidence),
    manualStatus,
    effectiveStatus,
    sourceMatchesBoard,
    approved:
      manualStatus === "schedule" ||
      (manualStatus === "unreviewed" &&
        llmStatus === "schedule" &&
        sourceMatchesBoard),
  }
}

// The source post text is read from raw_json so a quoted post counts the same
// way it did when sync promoted the image (see scheduleBoardSourceText).
const scheduleAssetReviewColumns = `
  a.id, a.kind, a.url, a.source_url AS sourceUrl,
  a.alt, a.week_start AS weekStart, a.source_account AS sourceAccount,
  a.updated_at AS assetUpdatedAt,
  (SELECT COALESCE(json_extract(p.raw_json, '$.search_text'), p.text)
     FROM posts p WHERE p.url = a.source_url
     ORDER BY p.published_at DESC LIMIT 1) AS sourceText,
  r.llm_status AS llmStatus, r.llm_confidence AS llmConfidence,
  r.llm_reason AS llmReason, r.llm_evidence AS llmEvidence,
  r.llm_model AS llmModel, r.llm_checked_at AS llmCheckedAt,
  COALESCE(r.manual_status, 'unreviewed') AS manualStatus,
  r.manual_reason AS manualReason, r.manual_checked_at AS manualCheckedAt,
  r.updated_at AS reviewUpdatedAt
`

export function getScheduleAssetReview(database, assetId) {
  const row = database
    .prepare(
      `SELECT ${scheduleAssetReviewColumns}
       FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id = a.id
       WHERE a.id = ? AND a.kind = 'schedule'`,
    )
    .get(String(assetId))
  return scheduleAssetReviewRow(row)
}

export function listScheduleAssetReviews(database, { limit = 100 } = {}) {
  const boundedLimit = Math.min(200, Math.max(1, Number(limit) || 100))
  return database
    .prepare(
      `SELECT ${scheduleAssetReviewColumns}
       FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id = a.id
       WHERE a.kind = 'schedule'
       ORDER BY COALESCE(a.week_start, a.updated_at) DESC, a.id ASC
       LIMIT ?`,
    )
    .all(boundedLimit)
    .map(scheduleAssetReviewRow)
}

export function upsertScheduleAssetReview(database, review = {}) {
  const assetId = String(review.assetId || review.asset_id || "").trim()
  if (!assetId || !getScheduleAssetReview(database, assetId)) return null
  const timestamp = nowIso()
  database
    .prepare(
      `INSERT INTO schedule_asset_reviews (
         asset_id, llm_status, llm_confidence, llm_reason, llm_evidence,
         llm_model, llm_checked_at, manual_status, manual_reason,
         manual_checked_at, updated_at
       ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(asset_id) DO UPDATE SET
         llm_status=excluded.llm_status,
         llm_confidence=excluded.llm_confidence,
         llm_reason=excluded.llm_reason,
         llm_evidence=excluded.llm_evidence,
         llm_model=excluded.llm_model,
         llm_checked_at=excluded.llm_checked_at,
         manual_status=excluded.manual_status,
         manual_reason=excluded.manual_reason,
         manual_checked_at=excluded.manual_checked_at,
         updated_at=excluded.updated_at`,
    )
    .run(
      assetId,
      normalizedScheduleAssetLlmStatus(review.llmStatus),
      review.llmConfidence == null ||
        !Number.isFinite(Number(review.llmConfidence))
        ? null
        : Math.min(1, Math.max(0, Number(review.llmConfidence))),
      nullable(review.llmReason),
      nullable(review.llmEvidence),
      nullable(review.llmModel),
      nullable(review.llmCheckedAt),
      normalizedScheduleAssetManualStatus(review.manualStatus),
      nullable(review.manualReason),
      nullable(review.manualCheckedAt),
      timestamp,
    )
  return getScheduleAssetReview(database, assetId)
}

export function updateScheduleAssetManualReview(
  database,
  assetId,
  { status = "unreviewed", reason = null } = {},
) {
  const existing = getScheduleAssetReview(database, assetId)
  if (!existing) return null
  const manualStatus = normalizedScheduleAssetManualStatus(status)
  const timestamp = nowIso()
  database
    .prepare(
      `INSERT INTO schedule_asset_reviews (
         asset_id, llm_status, llm_confidence, llm_reason, llm_evidence,
         llm_model, llm_checked_at, manual_status, manual_reason,
         manual_checked_at, updated_at
       ) VALUES (?, 'pending', NULL, NULL, NULL, NULL, NULL, ?, ?, ?, ?)
       ON CONFLICT(asset_id) DO UPDATE SET
         manual_status=excluded.manual_status,
         manual_reason=excluded.manual_reason,
         manual_checked_at=excluded.manual_checked_at,
         updated_at=excluded.updated_at`,
    )
    .run(
      String(assetId),
      manualStatus,
      manualStatus === "unreviewed" ? null : nullable(reason),
      manualStatus === "unreviewed" ? null : timestamp,
      timestamp,
    )
  return getScheduleAssetReview(database, assetId)
}

function ensureScheduleAssetReview(database, assetId) {
  database
    .prepare(
      `INSERT OR IGNORE INTO schedule_asset_reviews (
         asset_id, llm_status, manual_status, updated_at
       ) VALUES (?, 'pending', 'unreviewed', ?)`,
    )
    .run(String(assetId), nowIso())
}

function resetScheduleAssetReview(database, assetId) {
  database
    .prepare(
      `INSERT INTO schedule_asset_reviews (
         asset_id, llm_status, manual_status, updated_at
       ) VALUES (?, 'pending', 'unreviewed', ?)
       ON CONFLICT(asset_id) DO UPDATE SET
         llm_status='pending', llm_confidence=NULL, llm_reason=NULL,
         llm_evidence=NULL, llm_model=NULL, llm_checked_at=NULL,
         manual_status='unreviewed', manual_reason=NULL,
         manual_checked_at=NULL, updated_at=excluded.updated_at`,
    )
    .run(String(assetId), nowIso())
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
  return urls.filter(isCacheableImageUrl).map((source_url, position) => ({
    source: post?.source || "x",
    source_url,
    owner_type: "post",
    owner_id: post?.id,
    role: "post-image",
    position,
    alt: post?.media_alt,
  }))
}

function isCacheableImageUrl(value) {
  if (typeof value !== "string" || value.trim() === "") return false
  try {
    const url = new URL(value)
    const hostname = url.hostname.toLowerCase()
    if (hostname === "video.twimg.com") return false
    return !/\.(?:avi|m4v|mkv|mov|mp4|m3u8|webm)(?:$|[?#])/iu.test(url.pathname)
  } catch {
    return false
  }
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
    const expectedExtension = extensionForMimeType(normalizedMimeType)
    const cachePathValue = String(requestedCachePath)
    const namespace = cachePathValue.startsWith("sha256/")
      ? null
      : mediaNamespaceForSource(asset?.source)
    const expectedPath = cacheRelativePathForHash(
      requestedHash,
      expectedExtension,
      namespace,
    )
    if (cachePathValue !== expectedPath)
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

const profileMediaColumns = `
  id, slot, source, source_ref AS sourceRef, source_url AS sourceUrl,
  cache_path AS cachePath, active_cache_path AS activeCachePath,
  mime_type AS mimeType, extension, byte_size AS byteSize, sha256, width, height,
  status, is_active AS isActive, last_error AS lastError,
  created_at AS createdAt, updated_at AS updatedAt
`

const profileMediaSlotNames = new Set(["avatar", "banner"])
const profileMediaSources = new Set(["x", "youtube", "upload"])

function profileMediaSourceRoot(source) {
  const namespace = mediaNamespaceForSource(source)
  if (namespace === "youtube") return path.resolve(youtubeMediaDirectory)
  if (namespace === "avatar") return path.resolve(avatarMediaDirectory)
  return path.resolve(xMediaDirectory)
}

function pathInsideRoot(filePath, root) {
  return filePath === root || filePath.startsWith(`${root}${path.sep}`)
}

function normalizedProfileSlot(value) {
  const slot = String(value || "")
    .trim()
    .toLowerCase()
  return profileMediaSlotNames.has(slot) ? slot : null
}

function normalizedProfileSource(value) {
  const source = String(value || "")
    .trim()
    .toLowerCase()
  return profileMediaSources.has(source) ? source : null
}

function profileMediaCandidateId(slot, sourceUrl) {
  return crypto
    .createHash("sha256")
    .update(`${slot}\0${sourceUrl}`)
    .digest("hex")
}

function mapProfileMedia(row) {
  if (!row) return null
  return {
    ...row,
    isActive: Boolean(row.isActive),
    publicUrl:
      row.isActive &&
      row.status === MEDIA_STATUS.READY &&
      isSafeContentHash(row.sha256)
        ? `/media/profile/${row.slot}?v=${row.sha256}`
        : null,
  }
}

export function getProfileMedia(database, id) {
  if (!id) return null
  return mapProfileMedia(
    database
      .prepare(`SELECT ${profileMediaColumns} FROM profile_media WHERE id = ?`)
      .get(String(id)),
  )
}

export function listProfileMedia(database, { slot = null } = {}) {
  const normalizedSlot = slot == null ? null : normalizedProfileSlot(slot)
  if (slot != null && !normalizedSlot) return []
  const rows = normalizedSlot
    ? database
        .prepare(
          `SELECT ${profileMediaColumns} FROM profile_media WHERE slot = ? ORDER BY is_active DESC, updated_at DESC, id ASC`,
        )
        .all(normalizedSlot)
    : database
        .prepare(
          `SELECT ${profileMediaColumns} FROM profile_media ORDER BY slot ASC, is_active DESC, updated_at DESC, id ASC`,
        )
        .all()
  return rows.map(mapProfileMedia)
}

export function getActiveProfileMedia(database, slot) {
  const normalizedSlot = normalizedProfileSlot(slot)
  if (!normalizedSlot) return null
  return mapProfileMedia(
    database
      .prepare(
        `SELECT ${profileMediaColumns} FROM profile_media WHERE slot = ? AND is_active = 1 LIMIT 1`,
      )
      .get(normalizedSlot),
  )
}

/** Register a manually discovered profile image without downloading it. */
export function upsertProfileMediaCandidate(database, candidate = {}) {
  const slot = normalizedProfileSlot(candidate.slot)
  if (!slot) throw new Error("invalid profile media slot")
  const source = normalizedProfileSource(candidate.source)
  if (!source) throw new Error("invalid profile media source")
  const sourceUrl = candidate.sourceUrl
    ? normalizeSourceUrl(candidate.sourceUrl)
    : null
  if (source !== "upload" && !sourceUrl)
    throw new Error("profile media requires an HTTP(S) source URL")
  const id = String(
    candidate.id ||
      (sourceUrl
        ? profileMediaCandidateId(slot, sourceUrl)
        : `upload-${crypto.randomUUID()}`),
  )
  const timestamp = nowIso()
  database
    .prepare(
      `INSERT INTO profile_media (
         id, slot, source, source_ref, source_url, status, is_active,
         created_at, updated_at, raw_json
       ) VALUES (@id, @slot, @source, @source_ref, @source_url, 'pending', 0,
         @created_at, @updated_at, @raw_json)
       ON CONFLICT(id) DO UPDATE SET
         slot=excluded.slot, source=excluded.source,
         source_ref=COALESCE(excluded.source_ref, profile_media.source_ref),
         source_url=COALESCE(excluded.source_url, profile_media.source_url),
         status=CASE WHEN profile_media.status = 'ready' THEN 'ready' ELSE 'pending' END,
         last_error=CASE WHEN profile_media.status = 'ready' THEN profile_media.last_error ELSE NULL END,
         updated_at=excluded.updated_at,
         raw_json=COALESCE(excluded.raw_json, profile_media.raw_json)`,
    )
    .run({
      id,
      slot,
      source,
      source_ref: nullable(candidate.sourceRef ?? candidate.source_ref),
      source_url: sourceUrl,
      created_at: timestamp,
      updated_at: timestamp,
      raw_json: json(candidate),
    })
  return getProfileMedia(database, id)
}

export function updateProfileMediaReady(
  database,
  id,
  {
    cachePath,
    mimeType,
    sha256,
    byteSize = null,
    width = null,
    height = null,
  } = {},
) {
  const existing = getProfileMedia(database, id)
  if (!existing) return null
  const normalizedMime = normalizeMediaMimeType(mimeType)
  const hash = String(sha256 || "").toLowerCase()
  if (!normalizedMime || !isSafeContentHash(hash))
    throw new Error("invalid profile media metadata")
  const extension = extensionForMimeType(normalizedMime)
  const expectedPath = cacheRelativePathForHash(
    hash,
    extension,
    mediaNamespaceForSource(existing.source),
  )
  if (String(cachePath || "") !== expectedPath)
    throw new Error("invalid profile media cache path")
  const filePath = resolveMediaCachePath(expectedPath)
  let stat
  try {
    stat = fs.statSync(filePath)
  } catch {
    stat = null
  }
  if (!stat?.isFile())
    throw new Error("profile media cache file does not exist")
  const timestamp = nowIso()
  database
    .prepare(
      `UPDATE profile_media SET cache_path=?, mime_type=?, extension=?, byte_size=?,
       sha256=?, width=?, height=?, status='ready', last_error=NULL, updated_at=?
       WHERE id=?`,
    )
    .run(
      expectedPath,
      normalizedMime,
      extension,
      asIntegerOrNull(byteSize) ?? stat.size,
      hash,
      asIntegerOrNull(width),
      asIntegerOrNull(height),
      timestamp,
      String(id),
    )
  return getProfileMedia(database, id)
}

export function markProfileMediaFailed(database, id, error) {
  const existing = getProfileMedia(database, id)
  if (!existing) return null
  database
    .prepare(
      `UPDATE profile_media SET status='failed', last_error=?, updated_at=? WHERE id=?`,
    )
    .run(
      String(error || "profile media download failed").slice(0, 500),
      nowIso(),
      String(id),
    )
  return getProfileMedia(database, id)
}

export function selectProfileMedia(database, id) {
  const candidate = getProfileMedia(database, id)
  if (!candidate || candidate.status !== MEDIA_STATUS.READY)
    throw new Error("profile media is not ready")
  const sourcePath = candidate.cachePath
    ? resolveMediaCachePath(candidate.cachePath)
    : null
  if (!sourcePath) throw new Error("profile media cache path is invalid")
  ensureMediaCacheDirectories()
  const resolvedSource = fs.realpathSync(sourcePath)
  const allowedRoots = [
    profileMediaSourceRoot(candidate.source),
    path.resolve(legacyMediaCacheDirectory),
  ]
  if (!allowedRoots.some((root) => pathInsideRoot(resolvedSource, root))) {
    throw new Error("profile media cache path is outside profile storage")
  }
  const extension = sanitizeExtension(
    candidate.extension || extensionForMimeType(candidate.mimeType),
  )
  const destinationRelative = profileSlotRelativePath(candidate.slot, extension)
  const destination = resolveMediaCachePath(destinationRelative)
  if (!destination) throw new Error("profile media destination is invalid")
  const temporary = path.join(
    mediaCacheTempDirectory,
    `${candidate.slot}.${process.pid}.${crypto.randomUUID()}.part`,
  )
  try {
    fs.copyFileSync(resolvedSource, temporary, fs.constants.COPYFILE_FICLONE)
    fs.renameSync(temporary, destination)
  } catch (error) {
    try {
      fs.rmSync(temporary, { force: true })
    } catch {
      /* best effort cleanup */
    }
    throw error
  }
  const timestamp = nowIso()
  const select = database.transaction(() => {
    database
      .prepare(
        "UPDATE profile_media SET is_active=0, updated_at=? WHERE slot=?",
      )
      .run(timestamp, candidate.slot)
    database
      .prepare(
        `UPDATE profile_media SET is_active=1, active_cache_path=?, updated_at=? WHERE id=?`,
      )
      .run(destinationRelative, timestamp, String(id))
  })
  select()
  return getProfileMedia(database, id)
}

export function profileMediaSlots(database) {
  return {
    avatar: getActiveProfileMedia(database, "avatar"),
    banner: getActiveProfileMedia(database, "banner"),
  }
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
  if (getAppSetting(database, "schedule_keyword_enabled", null) == null) {
    const enabled = settingBoolean(
      process.env.SCHEDULE_KEYWORD_ENABLED ??
        getAppSetting(database, "schedule_extraction_enabled", "1"),
      true,
    )
    setAppSetting(database, "schedule_keyword_enabled", enabled ? "1" : "0")
  }
  if (getAppSetting(database, "schedule_vision_enabled", null) == null) {
    const enabled = settingBoolean(
      process.env.SCHEDULE_VISION_ENABLED ??
        getAppSetting(database, "schedule_extraction_enabled", "1"),
      true,
    )
    setAppSetting(database, "schedule_vision_enabled", enabled ? "1" : "0")
  }
  if (getAppSetting(database, "schedule_message_enabled", null) == null) {
    const enabled = settingBoolean(
      process.env.SCHEDULE_MESSAGE_ENABLED ??
        getAppSetting(database, "schedule_extraction_enabled", "1"),
      true,
    )
    setAppSetting(database, "schedule_message_enabled", enabled ? "1" : "0")
  }
  if (getAppSetting(database, "dashboard_revision", null) == null) {
    setAppSetting(database, "dashboard_revision", "0")
  }
  if (!database.prepare("SELECT 1 FROM llm_providers LIMIT 1").get()) {
    const legacyModel = getAppSetting(database, "llm_model", "gpt-4o-mini")
    upsertLlmProvider(database, {
      id: "openai-default",
      name: "OpenAI 默认",
      protocol: "openai-responses",
      baseUrl: "https://api.openai.com/v1",
      model: legacyModel,
      enabled: true,
      visionCapable: true,
      capabilities: LLM_CAPABILITIES,
      timeoutMs: 30000,
      maxRetries: 2,
      replaceApiKey: false,
      apiKeyCiphertext: null,
    })
    setLlmRouteProviders(database, SCHEDULE_VISION_ROUTE, ["openai-default"])
    setLlmRouteProviders(database, SCHEDULE_MESSAGE_ROUTE, ["openai-default"])
    setLlmRouteProviders(database, SCHEDULE_BOARD_ROUTE, ["openai-default"])
  }
  if (getAppSetting(database, "workflows_seeded", null) == null) {
    // Seeded workflows start unscheduled: timed polling of public platforms
    // is an explicit operator decision.
    if (!database.prepare("SELECT 1 FROM workflows LIMIT 1").get()) {
      upsertWorkflow(database, {
        id: "full-refresh",
        name: "完整更新",
        steps: WORKFLOW_STEP_IDS,
        scheduleEnabled: false,
        intervalMinutes: 60,
      })
      upsertWorkflow(database, {
        id: "sources-only",
        name: "仅抓取来源",
        steps: ["x", "youtube"],
        scheduleEnabled: false,
        intervalMinutes: 30,
      })
    }
    setAppSetting(database, "workflows_seeded", "1")
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
  const previousUrl = database.prepare("SELECT url FROM assets WHERE id = ?")
  const run = database.transaction((rows) => {
    for (const asset of rows) {
      const isSchedule = asset?.kind === "schedule" && asset?.id
      const before = isSchedule ? previousUrl.get(String(asset.id)) : null
      insertAsset(database, asset, true)
      if (!isSchedule) continue
      // Sync reuses `schedule-<week>` and the `weekly-schedule` alias, so a
      // review belongs to the image URL it judged, not just to the asset ID.
      if (before && String(before.url || "") !== String(asset.url || "")) {
        resetScheduleAssetReview(database, asset.id)
      } else {
        ensureScheduleAssetReview(database, asset.id)
      }
    }
  })
  run(assets)
  registerMediaCandidates(database, assets.flatMap(mediaCandidatesFromAsset))
}

const eventAdminColumns = `
  id, source, source_item_id AS sourceItemId, source_key AS sourceKey,
  title, detail, starts_on AS startsOn, starts_at AS startsAt,
  ends_at AS endsAt, timezone, time_precision AS timePrecision,
  status, event_type AS eventType, url, provenance,
  manual_locked AS manualLocked, confidence, extraction_id AS extractionId,
  cancellation_status AS cancellationStatus,
  cancellation_source AS cancellationSource,
  cancellation_reason AS cancellationReason,
  cancellation_evidence AS cancellationEvidence,
  cancellation_source_item_id AS cancellationSourceItemId,
  cancellation_confidence AS cancellationConfidence,
  cancellation_at AS cancellationAt,
  deleted_at AS deletedAt, created_at AS createdAt, updated_at AS updatedAt
`

export function getEvent(database, id) {
  return (
    database
      .prepare(`SELECT ${eventAdminColumns} FROM events WHERE id = ?`)
      .get(String(id)) || null
  )
}

const POST_LLM_STATUSES = new Set([
  "never",
  "queued",
  "running",
  "success",
  "uncertain",
  "failed",
  "skipped",
])

function normalizedPostLlmStatus(value) {
  const status = String(value || "never")
    .trim()
    .toLowerCase()
  return POST_LLM_STATUSES.has(status) ? status : "never"
}

export function getPostLlmState(database, postId) {
  const row = database
    .prepare(
      `SELECT post_id AS postId, status, route, last_attempt_at AS lastAttemptAt,
       last_processed_at AS lastProcessedAt, last_error AS lastError,
       last_extraction_id AS lastExtractionId,
       reprocess_requested AS reprocessRequested, updated_at AS updatedAt
       FROM post_llm_states WHERE post_id = ?`,
    )
    .get(String(postId))
  return row
    ? { ...row, reprocessRequested: Boolean(row.reprocessRequested) }
    : null
}

export function upsertPostLlmState(database, state = {}) {
  const postId = String(state.postId || state.post_id || "").trim()
  if (!postId) throw new Error("post LLM state requires postId")
  const timestamp = nowIso()
  const status = normalizedPostLlmStatus(state.status)
  database
    .prepare(
      `INSERT INTO post_llm_states (
         post_id, status, route, last_attempt_at, last_processed_at,
         last_error, last_extraction_id, reprocess_requested, updated_at
       ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(post_id) DO UPDATE SET
         status=excluded.status, route=COALESCE(excluded.route, post_llm_states.route),
         last_attempt_at=COALESCE(excluded.last_attempt_at, post_llm_states.last_attempt_at),
         last_processed_at=COALESCE(excluded.last_processed_at, post_llm_states.last_processed_at),
         last_error=excluded.last_error,
         last_extraction_id=COALESCE(excluded.last_extraction_id, post_llm_states.last_extraction_id),
         reprocess_requested=excluded.reprocess_requested,
         updated_at=excluded.updated_at`,
    )
    .run(
      postId,
      status,
      nullable(state.route),
      nullable(state.lastAttemptAt ?? state.last_attempt_at),
      nullable(state.lastProcessedAt ?? state.last_processed_at),
      nullable(state.lastError ?? state.last_error),
      asIntegerOrNull(state.lastExtractionId ?? state.last_extraction_id),
      (state.reprocessRequested ?? state.reprocess_requested) ? 1 : 0,
      timestamp,
    )
  return getPostLlmState(database, postId)
}

export function requestPostLlmReprocess(database, postId, route = null) {
  const exists = database
    .prepare("SELECT 1 FROM posts WHERE id = ?")
    .get(String(postId))
  if (!exists) return null
  const normalizedRoute =
    route === SCHEDULE_MESSAGE_ROUTE || route === SCHEDULE_BOARD_ROUTE
      ? route
      : null
  return upsertPostLlmState(database, {
    postId,
    status: "queued",
    route: normalizedRoute,
    reprocessRequested: true,
    lastError: null,
  })
}

export function listPostLlmStates(
  database,
  {
    limit = 50,
    status = "",
    reprocessRequested = false,
    includeRaw = false,
  } = {},
) {
  const boundedLimit = Math.min(200, Math.max(1, Number(limit) || 50))
  const clauses = []
  const values = []
  if (status && POST_LLM_STATUSES.has(String(status))) {
    clauses.push("s.status = ?")
    values.push(String(status))
  }
  if (reprocessRequested) clauses.push("s.reprocess_requested = 1")
  const where = clauses.length ? `WHERE ${clauses.join(" AND ")}` : ""
  return database
    .prepare(
      `SELECT p.id, p.source, p.account_handle AS accountHandle, p.text,
       p.published_at AS publishedAt, p.url, p.media_url AS mediaUrl,
       p.media_alt AS mediaAlt, p.raw_json AS rawJson,
       COALESCE(s.status, 'never') AS llmStatus, s.route AS llmRoute,
       s.last_attempt_at AS llmLastAttemptAt,
       s.last_processed_at AS llmLastProcessedAt,
       s.last_error AS llmLastError,
       s.last_extraction_id AS llmLastExtractionId,
       COALESCE(s.reprocess_requested, 0) AS llmReprocessRequested
       FROM posts p LEFT JOIN post_llm_states s ON s.post_id = p.id
       ${where}
       ORDER BY p.published_at DESC LIMIT ?`,
    )
    .all(...values, boundedLimit)
    .map((post) => {
      const { rawJson, ...publicPost } = post
      if (!includeRaw)
        return {
          ...publicPost,
          llmReprocessRequested: Boolean(post.llmReprocessRequested),
        }
      return {
        ...publicPost,
        raw: (() => {
          try {
            return rawJson ? JSON.parse(rawJson) : null
          } catch {
            return null
          }
        })(),
        llmReprocessRequested: Boolean(post.llmReprocessRequested),
      }
    })
}

export function listPostsRequestedForLlm(database, { limit = 20 } = {}) {
  return listPostLlmStates(database, {
    limit,
    reprocessRequested: true,
    includeRaw: true,
  })
}

export function applyLlmCancellationJudgements(
  database,
  { sourceItemId, reason, evidence, confidence, targets = [] } = {},
) {
  const timestamp = nowIso()
  const uniqueTargets = new Map()
  for (const target of targets) {
    const eventId = String(target.eventId || target.id || "").trim()
    if (!eventId || uniqueTargets.has(eventId)) continue
    uniqueTargets.set(eventId, target)
  }
  const update = database.prepare(
    `UPDATE events SET
       cancellation_status='llm_suspected', cancellation_source='llm',
       cancellation_reason=?, cancellation_evidence=?,
       cancellation_source_item_id=?, cancellation_confidence=?,
       cancellation_at=?, updated_at=?
       WHERE id=? AND deleted_at IS NULL
         AND COALESCE(cancellation_status, 'none') != 'manual_confirmed'`,
  )
  const apply = database.transaction(() => {
    let applied = 0
    for (const [eventId, target] of uniqueTargets) {
      const result = update.run(
        String(reason || target.reason || "LLM 判断该日程可能已取消").slice(
          0,
          500,
        ),
        String(evidence || target.evidence || "").slice(0, 1000) || null,
        nullable(sourceItemId),
        Number.isFinite(Number(confidence)) ? Number(confidence) : null,
        timestamp,
        timestamp,
        eventId,
      )
      applied += result.changes
    }
    return applied
  })
  return { applied: apply(), candidates: uniqueTargets.size }
}

function normalizedCancellationTitle(value) {
  return String(value || "")
    .toLowerCase()
    .replace(/[\s「」『』【】()（）［］[\]・:：,.，。!?！？]/gu, "")
}

export function findCancellationTargets(database, events = []) {
  const active = database
    .prepare(
      `SELECT id, title, starts_on AS startsOn, starts_at AS startsAt,
       provenance, manual_locked AS manualLocked
       FROM events WHERE deleted_at IS NULL`,
    )
    .all()
  const targets = []
  for (const candidate of events) {
    const date = String(
      candidate?.starts_on ?? candidate?.startsOn ?? candidate?.date ?? "",
    )
    if (!/^\d{4}-\d{2}-\d{2}$/u.test(date)) continue
    const title = normalizedCancellationTitle(candidate?.title)
    // Stored times mix `Z` and `+09:00` offsets, so compare instants.
    const instant = Date.parse(candidate?.starts_at ?? candidate?.startsAt)
    const sameDate = active.filter((event) => {
      if (event.startsOn !== date) return false
      const eventInstant = Date.parse(event.startsAt)
      if (!Number.isFinite(instant) || !Number.isFinite(eventInstant))
        return true
      return eventInstant === instant
    })
    if (!sameDate.length) continue
    const exact = title
      ? sameDate.filter(
          (event) => normalizedCancellationTitle(event.title) === title,
        )
      : []
    const narrowed =
      exact.length === 1 ? exact : sameDate.length === 1 ? sameDate : []
    if (narrowed.length === 1) {
      targets.push({
        eventId: narrowed[0].id,
        reason: candidate.reason,
        evidence: candidate.evidence,
      })
    }
  }
  return targets
}

export function updateManualCancellation(
  database,
  id,
  { status = "none", reason = null, evidence = null } = {},
) {
  const existing = getEvent(database, id)
  if (!existing) return null
  const normalized = String(status || "none")
    .trim()
    .toLowerCase()
  if (!["none", "manual_confirmed"].includes(normalized)) {
    throw new Error("invalid manual cancellation status")
  }
  const timestamp = nowIso()
  database
    .prepare(
      `UPDATE events SET
       cancellation_status=?, cancellation_source=?, cancellation_reason=?,
       cancellation_evidence=?, cancellation_source_item_id=NULL,
       cancellation_confidence=NULL, cancellation_at=?, updated_at=?
       WHERE id=?`,
    )
    .run(
      normalized,
      normalized === "manual_confirmed" ? "manual" : null,
      normalized === "manual_confirmed"
        ? String(reason || "").trim() || null
        : null,
      normalized === "manual_confirmed"
        ? String(evidence || "").trim() || null
        : null,
      normalized === "manual_confirmed" ? timestamp : null,
      timestamp,
      String(id),
    )
  return getEvent(database, id)
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

export function listAdminEventsPage(
  database,
  {
    page = 1,
    pageSize = 25,
    includeDeleted = false,
    search = "",
    provenance = "",
    source = "",
    from = "",
    to = "",
  } = {},
) {
  const boundedPageSize = Math.min(
    100,
    Math.max(1, Math.trunc(Number(pageSize)) || 25),
  )
  const normalizedPage = Math.max(1, Math.trunc(Number(page)) || 1)
  const clauses = []
  const values = []
  if (!includeDeleted) clauses.push("deleted_at IS NULL")
  const normalizedSearch = String(search || "").trim()
  if (normalizedSearch) {
    clauses.push(
      "(title LIKE ? ESCAPE '\\' OR detail LIKE ? ESCAPE '\\' OR status LIKE ? ESCAPE '\\')",
    )
    const escaped = normalizedSearch.replace(
      /[\\%_]/gu,
      (character) => `\\${character}`,
    )
    const pattern = `%${escaped}%`
    values.push(pattern, pattern, pattern)
  }
  if (["automatic", "manual"].includes(String(provenance))) {
    clauses.push("provenance = ?")
    values.push(String(provenance))
  }
  if (String(source || "").trim()) {
    clauses.push("source = ?")
    values.push(String(source).trim())
  }
  if (/^\d{4}-\d{2}-\d{2}$/u.test(String(from || ""))) {
    clauses.push("starts_on >= ?")
    values.push(String(from))
  }
  if (/^\d{4}-\d{2}-\d{2}$/u.test(String(to || ""))) {
    clauses.push("starts_on <= ?")
    values.push(String(to))
  }
  const where = clauses.length ? `WHERE ${clauses.join(" AND ")}` : ""
  const total = Number(
    database
      .prepare(`SELECT COUNT(*) AS count FROM events ${where}`)
      .get(...values).count,
  )
  const totalPages = Math.max(1, Math.ceil(total / boundedPageSize))
  const safePage = Math.min(normalizedPage, totalPages)
  const rows = database
    .prepare(
      `SELECT ${eventAdminColumns} FROM events ${where}
       ORDER BY starts_on DESC, COALESCE(starts_at, starts_on) DESC, id ASC
       LIMIT ? OFFSET ?`,
    )
    .all(...values, boundedPageSize, (safePage - 1) * boundedPageSize)
    .map((event) => ({
      ...event,
      manualLocked: Boolean(event.manualLocked),
    }))
  return {
    items: rows,
    events: rows,
    page: safePage,
    pageSize: boundedPageSize,
    total,
    totalPages,
    hasPrevious: safePage > 1,
    hasNext: safePage < totalPages,
  }
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
    ...noCancellation,
    deleted_at: null,
  }
  const create = database.transaction(() => {
    insertEvent(database, value, false)
    insertEventSource(database, value, id)
    if (
      String(
        event.cancellation_status ?? event.cancellationStatus ?? "none",
      ) === "manual_confirmed"
    ) {
      updateManualCancellation(database, id, {
        status: "manual_confirmed",
        reason: event.cancellation_reason ?? event.cancellationReason,
      })
    }
  })
  create()
  return getEvent(database, id)
}

const noCancellation = {
  cancellation_status: "none",
  cancellation_source: null,
  cancellation_reason: null,
  cancellation_evidence: null,
  cancellation_source_item_id: null,
  cancellation_confidence: null,
  cancellation_at: null,
}

/**
 * A manual save decides the cancellation explicitly: `llm_suspected` keeps an
 * unresolved LLM overlay as-is, `none` dismisses it, and `manual_confirmed`
 * replaces it with the operator's reason. Saving an already confirmed
 * cancellation keeps its original decision time.
 */
function manualCancellationFields(existing, event, timestamp) {
  const requested = String(
    event.cancellation_status ?? event.cancellationStatus ?? "none",
  )
    .trim()
    .toLowerCase()
  if (requested === "manual_confirmed") {
    return {
      ...noCancellation,
      cancellation_status: "manual_confirmed",
      cancellation_source: "manual",
      cancellation_reason: nullable(
        event.cancellation_reason ?? event.cancellationReason,
      ),
      cancellation_at:
        existing?.cancellationStatus === "manual_confirmed"
          ? existing.cancellationAt || timestamp
          : timestamp,
    }
  }
  if (
    requested === "llm_suspected" &&
    existing?.cancellationStatus === "llm_suspected"
  ) {
    return {
      cancellation_status: "llm_suspected",
      cancellation_source: existing.cancellationSource,
      cancellation_reason: existing.cancellationReason,
      cancellation_evidence: existing.cancellationEvidence,
      cancellation_source_item_id: existing.cancellationSourceItemId,
      cancellation_confidence: existing.cancellationConfidence,
      cancellation_at: existing.cancellationAt,
    }
  }
  return noCancellation
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
         cancellation_status=@cancellation_status,
         cancellation_source=@cancellation_source,
         cancellation_reason=@cancellation_reason,
         cancellation_evidence=@cancellation_evidence,
         cancellation_source_item_id=@cancellation_source_item_id,
         cancellation_confidence=@cancellation_confidence,
         cancellation_at=@cancellation_at,
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
      ...manualCancellationFields(existing, event, timestamp),
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
       WHERE id=? AND provenance='automatic' AND manual_locked=0`,
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

const llmProviderColumns = `
  id, name, protocol, base_url AS baseUrl, model,
  enabled, vision_capable AS visionCapable,
  capabilities_json AS capabilitiesJson, timeout_ms AS timeoutMs,
  max_retries AS maxRetries, api_key_ciphertext AS apiKeyCiphertext,
  last_status AS lastStatus, last_error AS lastError,
  last_checked_at AS lastCheckedAt, created_at AS createdAt, updated_at AS updatedAt
`
const llmProviderColumnsWithAlias = `
  p.id, p.name, p.protocol, p.base_url AS baseUrl, p.model,
  p.enabled, p.vision_capable AS visionCapable,
  p.capabilities_json AS capabilitiesJson, p.timeout_ms AS timeoutMs,
  p.max_retries AS maxRetries, p.api_key_ciphertext AS apiKeyCiphertext,
  p.last_status AS lastStatus, p.last_error AS lastError,
  p.last_checked_at AS lastCheckedAt, p.created_at AS createdAt, p.updated_at AS updatedAt
`

function mapLlmProvider(row) {
  if (!row) return null
  const {
    apiKeyCiphertext,
    capabilitiesJson,
    routePriority: _routePriority,
    ...publicFields
  } = row
  return {
    ...publicFields,
    enabled: Boolean(row.enabled),
    capabilities: normalizeLlmCapabilities(
      capabilitiesJson,
      Boolean(row.visionCapable),
    ),
    // Kept as a derived compatibility field for older admin clients.
    visionCapable: normalizeLlmCapabilities(
      capabilitiesJson,
      Boolean(row.visionCapable),
    ).includes("image"),
    apiKeyConfigured: Boolean(apiKeyCiphertext),
  }
}

export function getLlmProvider(database, id) {
  return mapLlmProvider(
    database
      .prepare(`SELECT ${llmProviderColumns} FROM llm_providers WHERE id = ?`)
      .get(String(id)),
  )
}

/** Internal credential lookup. The ciphertext is never returned by API payloads. */
export function getLlmProviderSecret(database, id) {
  return (
    database
      .prepare(
        "SELECT id, api_key_ciphertext AS apiKeyCiphertext FROM llm_providers WHERE id = ?",
      )
      .get(String(id)) || null
  )
}

export function listLlmProviders(database, { route = null } = {}) {
  const rows = route
    ? database
        .prepare(
          `SELECT ${llmProviderColumnsWithAlias}, r.priority AS routePriority
           FROM llm_providers p
           JOIN llm_route_providers r ON r.provider_id = p.id
           WHERE r.route = ?
           ORDER BY r.priority ASC, p.id ASC`,
        )
        .all(String(route))
    : database
        .prepare(
          `SELECT ${llmProviderColumns}, NULL AS routePriority FROM llm_providers
           ORDER BY enabled DESC, updated_at DESC, id ASC`,
        )
        .all()
  return rows.map((row) => ({
    ...mapLlmProvider(row),
    ...(row.routePriority == null ? {} : { priority: row.routePriority }),
  }))
}

export function upsertLlmProvider(database, provider = {}) {
  const id = String(provider.id || "").trim()
  if (!/^[a-z0-9][a-z0-9_-]{0,63}$/u.test(id))
    throw new Error("invalid LLM provider id")
  const timestamp = nowIso()
  const protocol = LLM_PROTOCOLS.includes(String(provider.protocol))
    ? String(provider.protocol)
    : "openai-responses"
  const baseUrl = String(provider.baseUrl || provider.base_url || "").trim()
  // The model is the provider's default model (connection test and legacy
  // routes). Providers managed through the model catalog may leave it empty.
  const model = String(provider.model || "").trim()
  if (!baseUrl) throw new Error("LLM provider requires baseUrl")
  if (model && !LLM_MODEL_ID_PATTERN.test(model))
    throw new Error("LLM provider model is invalid")
  const timeoutMs = Math.min(
    120000,
    Math.max(
      1000,
      Number(provider.timeoutMs ?? provider.timeout_ms ?? 30000) || 30000,
    ),
  )
  // Keep accepting the legacy input field, but enforce the three-request
  // provider contract for every persisted configuration.
  const maxRetries = LLM_MAX_RETRIES
  const capabilities = normalizeLlmCapabilities(
    provider.capabilities ?? provider.capabilitiesJson,
    provider.visionCapable !== false,
  )
  const visionCapable = capabilities.includes("image")
  const replaceApiKey = Boolean(provider.replaceApiKey)
  const encryptedCredential = replaceApiKey
    ? nullable(provider.apiKeyCiphertext)
    : null
  database
    .prepare(
      `INSERT INTO llm_providers (
         id, name, protocol, base_url, model, enabled, vision_capable,
         capabilities_json, timeout_ms, max_retries, api_key_ciphertext,
         last_status, last_error,
         last_checked_at, created_at, updated_at
       ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         name=excluded.name, protocol=excluded.protocol, base_url=excluded.base_url,
         model=excluded.model, enabled=excluded.enabled,
         vision_capable=excluded.vision_capable,
         capabilities_json=excluded.capabilities_json,
         timeout_ms=excluded.timeout_ms,
         max_retries=excluded.max_retries,
         api_key_ciphertext=CASE WHEN ? = 1 THEN excluded.api_key_ciphertext
           ELSE llm_providers.api_key_ciphertext END,
         updated_at=excluded.updated_at`,
    )
    .run(
      id,
      String(provider.name || id)
        .trim()
        .slice(0, 120) || id,
      protocol,
      baseUrl,
      model,
      provider.enabled === false ? 0 : 1,
      visionCapable ? 1 : 0,
      JSON.stringify(capabilities),
      timeoutMs,
      maxRetries,
      encryptedCredential,
      timestamp,
      timestamp,
      replaceApiKey ? 1 : 0,
    )
  if (model) syncDefaultModelCapabilities(database, id, model, capabilities)
  return getLlmProvider(database, id)
}

// Keep the default model's routing tags and the provider capability columns
// identical, whichever side an operator edits.
function syncDefaultModelCapabilities(
  database,
  providerId,
  model,
  capabilities,
) {
  const timestamp = nowIso()
  const existing = getLlmModel(database, providerId, model)
  const descriptive = (existing?.tags || []).filter(
    (tag) => !LLM_CAPABILITIES.includes(tag),
  )
  const tags = normalizeLlmModelTags([...capabilities, ...descriptive])
  if (existing) {
    database
      .prepare(
        "UPDATE llm_models SET tags_json = ?, updated_at = ? WHERE provider_id = ? AND model_id = ?",
      )
      .run(JSON.stringify(tags), timestamp, providerId, model)
  } else {
    database
      .prepare(
        `INSERT INTO llm_models
         (provider_id, model_id, name, tags_json, enabled, origin, owned_by, created_at, updated_at)
         VALUES (?, ?, NULL, ?, 1, 'legacy', NULL, ?, ?)`,
      )
      .run(providerId, model, JSON.stringify(tags), timestamp, timestamp)
  }
}

export function updateLlmProviderStatus(
  database,
  id,
  { status = null, error = null } = {},
) {
  const timestamp = nowIso()
  database
    .prepare(
      `UPDATE llm_providers SET last_status=?, last_error=?,
       last_checked_at=?, updated_at=? WHERE id=?`,
    )
    .run(
      nullable(status),
      nullable(error)?.slice(0, 500) || null,
      timestamp,
      timestamp,
      String(id),
    )
  return getLlmProvider(database, id)
}

export function deleteLlmProvider(database, id) {
  const result = database
    .prepare("DELETE FROM llm_providers WHERE id = ?")
    .run(String(id))
  return result.changes > 0
}

export function normalizeLlmModelTags(value, { fallback = [] } = {}) {
  let values = value
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value)
      values = Array.isArray(parsed) ? parsed : value.split(/[,\s]+/u)
    } catch {
      values = value.split(/[,\s]+/u)
    }
  }
  if (!Array.isArray(values)) return [...fallback]
  const selected = new Set(
    values
      .map((item) =>
        String(item || "")
          .trim()
          .toLowerCase(),
      )
      .map((item) => (item === "vision" ? "image" : item)),
  )
  const normalized = LLM_MODEL_TAGS.filter((tag) => selected.has(tag))
  return normalized.length ? normalized : [...fallback]
}

const llmModelColumns = `
  provider_id AS providerId, model_id AS modelId, name, tags_json AS tagsJson,
  enabled, origin, owned_by AS ownedBy, created_at AS createdAt, updated_at AS updatedAt
`

function mapLlmModel(row) {
  if (!row) return null
  const tags = normalizeLlmModelTags(row.tagsJson)
  return {
    providerId: row.providerId,
    id: row.modelId,
    name: row.name || null,
    tags,
    capabilities: tags.filter((tag) => LLM_CAPABILITIES.includes(tag)),
    enabled: Boolean(row.enabled),
    origin: row.origin,
    ownedBy: row.ownedBy || null,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
  }
}

export function getLlmModel(database, providerId, modelId) {
  return mapLlmModel(
    database
      .prepare(
        `SELECT ${llmModelColumns} FROM llm_models WHERE provider_id = ? AND model_id = ?`,
      )
      .get(String(providerId), String(modelId)),
  )
}

export function listLlmModels(database, { providerId = null } = {}) {
  const rows = providerId
    ? database
        .prepare(
          `SELECT ${llmModelColumns} FROM llm_models WHERE provider_id = ?
           ORDER BY model_id COLLATE NOCASE ASC`,
        )
        .all(String(providerId))
    : database
        .prepare(
          `SELECT ${llmModelColumns} FROM llm_models
           ORDER BY provider_id ASC, model_id COLLATE NOCASE ASC`,
        )
        .all()
  return rows.map(mapLlmModel)
}

/**
 * Add catalog models to a provider. Existing rows keep their operator tags
 * and enabled flag unless the caller passes them explicitly.
 */
export function upsertLlmModels(database, providerId, models = []) {
  const provider = getLlmProvider(database, providerId)
  if (!provider) throw new Error(`LLM provider does not exist: ${providerId}`)
  const timestamp = nowIso()
  const write = database.transaction(() => {
    for (const model of models) {
      const id = String(model?.id || "").trim()
      if (!LLM_MODEL_ID_PATTERN.test(id))
        throw new Error(`LLM model ID is invalid: ${id.slice(0, 80)}`)
      const existing = getLlmModel(database, provider.id, id)
      const name =
        model.name === undefined
          ? (existing?.name ?? null)
          : String(model.name || "")
              .trim()
              .slice(0, 120) || null
      const ownedBy =
        model.ownedBy === undefined
          ? (existing?.ownedBy ?? null)
          : String(model.ownedBy || "")
              .trim()
              .slice(0, 120) || null
      const tags =
        model.tags === undefined
          ? (existing?.tags ?? ["text"])
          : normalizeLlmModelTags(model.tags)
      const enabled =
        model.enabled === undefined
          ? (existing?.enabled ?? true)
          : Boolean(model.enabled)
      const origin = ["remote", "manual", "legacy"].includes(model.origin)
        ? model.origin
        : (existing?.origin ?? "manual")
      database
        .prepare(
          `INSERT INTO llm_models
           (provider_id, model_id, name, tags_json, enabled, origin, owned_by, created_at, updated_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT(provider_id, model_id) DO UPDATE SET
             name=excluded.name, tags_json=excluded.tags_json,
             enabled=excluded.enabled, origin=excluded.origin,
             owned_by=excluded.owned_by, updated_at=excluded.updated_at`,
        )
        .run(
          provider.id,
          id,
          name,
          JSON.stringify(tags),
          enabled ? 1 : 0,
          origin,
          ownedBy,
          timestamp,
          timestamp,
        )
      const capabilities = tags.filter((tag) => LLM_CAPABILITIES.includes(tag))
      if (id === provider.model && capabilities.length) {
        database
          .prepare(
            `UPDATE llm_providers SET capabilities_json = ?, vision_capable = ?, updated_at = ?
             WHERE id = ?`,
          )
          .run(
            JSON.stringify(capabilities),
            capabilities.includes("image") ? 1 : 0,
            timestamp,
            provider.id,
          )
      }
    }
  })
  write()
  return listLlmModels(database, { providerId: provider.id })
}

export function deleteLlmModels(database, providerId, modelIds = []) {
  const provider = getLlmProvider(database, providerId)
  if (!provider) return 0
  const ids = [
    ...new Set(modelIds.map((id) => String(id || "").trim())),
  ].filter(Boolean)
  let removed = 0
  const remove = database.transaction(() => {
    for (const id of ids) {
      removed += database
        .prepare(
          "DELETE FROM llm_models WHERE provider_id = ? AND model_id = ?",
        )
        .run(provider.id, id).changes
      database
        .prepare(
          "DELETE FROM llm_route_targets WHERE provider_id = ? AND model_id = ?",
        )
        .run(provider.id, id)
      if (id === provider.model) {
        database
          .prepare(
            "UPDATE llm_providers SET model = '', updated_at = ? WHERE id = ?",
          )
          .run(nowIso(), provider.id)
      }
    }
    for (const route of SCHEDULE_ROUTES) compactRoutePriorities(database, route)
  })
  remove()
  return removed
}

function compactRoutePriorities(database, route) {
  const targets = getLlmRouteTargets(database, route)
  writeRouteTargets(database, route, targets)
}

function routeTargetKey(target) {
  return `${target.providerId}\u0000${target.modelId}`
}

export function getLlmRouteTargets(database, route = SCHEDULE_VISION_ROUTE) {
  return database
    .prepare(
      `SELECT r.provider_id AS providerId, r.model_id AS modelId
       FROM llm_route_targets r
       JOIN llm_providers p ON p.id = r.provider_id
       WHERE r.route = ? ORDER BY r.priority ASC, r.provider_id ASC, r.model_id ASC`,
    )
    .all(String(route))
    .map((row) => ({
      providerId: String(row.providerId),
      modelId: String(row.modelId || ""),
    }))
}

function writeRouteTargets(database, route, targets) {
  const timestamp = nowIso()
  database
    .prepare("DELETE FROM llm_route_targets WHERE route = ?")
    .run(String(route))
  const insert = database.prepare(
    `INSERT INTO llm_route_targets (route, provider_id, model_id, priority, created_at, updated_at)
     VALUES (?, ?, ?, ?, ?, ?)`,
  )
  targets.forEach((target, priority) =>
    insert.run(
      String(route),
      target.providerId,
      target.modelId,
      priority,
      timestamp,
      timestamp,
    ),
  )
}

/**
 * Replace a route's ordered model targets. A target with an empty model ID
 * follows the provider's default model; any other model must exist in the
 * provider's catalog.
 */
export function setLlmRouteTargets(
  database,
  route = SCHEDULE_VISION_ROUTE,
  targets = [],
) {
  if (!SCHEDULE_ROUTES.includes(String(route)))
    throw new Error(`LLM route is invalid: ${route}`)
  const seen = new Set()
  const normalized = []
  for (const target of targets) {
    const providerId = String(target?.providerId || "").trim()
    const modelId = String(target?.modelId || "").trim()
    if (!providerId) continue
    if (!getLlmProvider(database, providerId))
      throw new Error(`LLM provider does not exist: ${providerId}`)
    if (modelId && !getLlmModel(database, providerId, modelId))
      throw new Error(`LLM model does not exist: ${providerId}/${modelId}`)
    const key = routeTargetKey({ providerId, modelId })
    if (seen.has(key)) continue
    seen.add(key)
    normalized.push({ providerId, modelId })
  }
  const update = database.transaction(() =>
    writeRouteTargets(database, route, normalized),
  )
  update()
  return getLlmRouteTargets(database, route)
}

/** Append a provider-default target unless the provider is already routed. */
export function appendLlmRouteProvider(database, route, providerId) {
  const current = getLlmRouteTargets(database, route)
  if (current.some((target) => target.providerId === providerId)) return current
  return setLlmRouteTargets(database, route, [
    ...current,
    { providerId, modelId: "" },
  ])
}

/** Distinct provider IDs in route order, kept for the legacy admin API. */
export function getLlmRouteProviders(database, route = SCHEDULE_VISION_ROUTE) {
  return [
    ...new Set(
      getLlmRouteTargets(database, route).map((target) => target.providerId),
    ),
  ]
}

export function getScheduleProviderOrders(database) {
  const legacy = getLlmRouteProviders(database, SCHEDULE_VISION_ROUTE)
  const orders = {}
  for (const route of [SCHEDULE_MESSAGE_ROUTE, SCHEDULE_BOARD_ROUTE]) {
    const configured = getLlmRouteProviders(database, route)
    orders[route] = configured.length ? configured : [...legacy]
  }
  orders[SCHEDULE_VISION_ROUTE] = legacy
  return orders
}

/** Effective model targets per route, with the legacy route as fallback. */
export function getScheduleRouteTargets(database) {
  const legacy = getLlmRouteTargets(database, SCHEDULE_VISION_ROUTE)
  const targets = {}
  for (const route of [SCHEDULE_MESSAGE_ROUTE, SCHEDULE_BOARD_ROUTE]) {
    const configured = getLlmRouteTargets(database, route)
    targets[route] = configured.length
      ? configured
      : legacy.map((t) => ({ ...t }))
  }
  targets[SCHEDULE_VISION_ROUTE] = legacy
  return targets
}

/** Legacy provider-only writer: every provider follows its default model. */
export function setLlmRouteProviders(
  database,
  route = SCHEDULE_VISION_ROUTE,
  providerIds = [],
) {
  const ids = [
    ...new Set(providerIds.map((id) => String(id).trim()).filter(Boolean)),
  ]
  setLlmRouteTargets(
    database,
    route,
    ids.map((providerId) => ({ providerId, modelId: "" })),
  )
  return getLlmRouteProviders(database, route)
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
  const legacyEnabled = settingBoolean(enabledSetting, true)
  const keywordEnabled = settingBoolean(
    getAppSetting(
      database,
      "schedule_keyword_enabled",
      process.env.SCHEDULE_KEYWORD_ENABLED ?? enabledSetting,
    ),
    legacyEnabled,
  )
  const visionEnabled = settingBoolean(
    getAppSetting(
      database,
      "schedule_vision_enabled",
      process.env.SCHEDULE_VISION_ENABLED ?? enabledSetting,
    ),
    legacyEnabled,
  )
  const messageEnabled = settingBoolean(
    getAppSetting(
      database,
      "schedule_message_enabled",
      process.env.SCHEDULE_MESSAGE_ENABLED ?? enabledSetting,
    ),
    legacyEnabled,
  )
  const config = {
    // The legacy setting only seeds the stage defaults and remains writable
    // through the old API. New callers must be able to enable the message
    // detector independently when an older database left this value off.
    enabled: messageEnabled || (keywordEnabled && visionEnabled),
    keywords: normalizedScheduleKeywords(keywordsSetting),
  }
  // Keep the original enumerable response shape for existing integrations;
  // richer stage fields remain available to server-side callers and are
  // explicitly selected by the admin API.
  Object.defineProperties(config, {
    keywordEnabled: { value: keywordEnabled, enumerable: false },
    visionEnabled: { value: visionEnabled, enumerable: false },
    messageEnabled: { value: messageEnabled, enumerable: false },
    providerOrder: {
      value: getScheduleProviderOrders(database)[SCHEDULE_VISION_ROUTE],
      enumerable: false,
    },
    providerOrders: {
      value: getScheduleProviderOrders(database),
      enumerable: false,
    },
  })
  return config
}

/**
 * Return the complete persisted schedule pipeline configuration.  The legacy
 * getScheduleExtractionConfig() keeps its small enumerable shape for callers
 * that compare the old response verbatim; new admin/API consumers should use
 * this explicit shape instead.
 */
export function getDetailedScheduleExtractionConfig(database) {
  const config = getScheduleExtractionConfig(database)
  return {
    enabled: config.enabled,
    keywordEnabled: config.keywordEnabled,
    visionEnabled: config.visionEnabled,
    messageEnabled: config.messageEnabled,
    keywords: [...config.keywords],
    providerOrder: [...config.providerOrder],
    providerOrders: Object.fromEntries(
      Object.entries(config.providerOrders).map(([route, ids]) => [
        route,
        [...ids],
      ]),
    ),
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
  {
    limit = 20,
    keywords = null,
    detectionType = "board",
    includeAll = false,
  } = {},
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
  const posts = database
    .prepare(
      `SELECT id, source, type, label, text, published_at AS publishedAt, url,
       account_handle AS accountHandle,
       media_url AS mediaUrl, media_alt AS mediaAlt, raw_json AS rawJson
       FROM posts
       WHERE source='x'
       ORDER BY published_at DESC`,
    )
    .all()
    .map(({ rawJson, ...post }) => {
      const raw = parseJson(rawJson)
      const mediaUrls = Array.isArray(raw?.media_urls)
        ? raw.media_urls
        : Array.isArray(raw?.mediaUrls)
          ? raw.mediaUrls
          : post.mediaUrl
            ? [post.mediaUrl]
            : []
      return { ...post, mediaUrls, raw }
    })
  const matchesKeyword = (post) => {
    const haystack =
      `${String(post.label || "")} ${String(post.text || "")}`.toLocaleLowerCase()
    return loweredKeywords.some((keyword) => haystack.includes(keyword))
  }
  const looksLikeScheduleMessage = (post) => {
    const text = String(post.text || "")
    if (!text.trim()) {
      return (
        post.mediaUrls.length > 0 &&
        /(?:notice|schedule|日程|告知|お知らせ|公告)/iu.test(
          `${String(post.type || "")} ${String(post.label || "")}`,
        )
      )
    }
    if (matchesKeyword(post)) return true
    // Keep the message detector inexpensive: only send posts that contain a
    // date/time or a scheduling verb likely to describe a concrete notice.
    return /(?:今日|明日|明後日|今週|来週|(?:\d{1,2})\s*[/:月日]\s*(?:\d{1,2})?|(?:\d{1,2})\s*時)|(?:配信|放送|出演|イベント|ライブ|開始|開催|予定|告知)/iu.test(
      text,
    )
  }
  return posts
    .filter((post) => {
      if (includeAll) return true
      if (detectionType === "message") return looksLikeScheduleMessage(post)
      return matchesKeyword(post)
    })
    .slice(0, boundedLimit)
}

/**
 * Return schedule assets with source-post metadata. The synchronizer uses
 * these rows to identify missing media and recover a source post that a
 * profile page no longer exposes in its HTML.
 */
export function listScheduleAssets(database, { limit = 20 } = {}) {
  const boundedLimit = Math.min(100, Math.max(1, Number(limit) || 20))
  return database
    .prepare(
      `SELECT id, kind, url, source_url AS sourceUrl, alt,
       week_start AS weekStart, source_account AS sourceAccount,
       updated_at AS updatedAt
       FROM assets
       WHERE kind = 'schedule' AND source_url IS NOT NULL
         AND TRIM(source_url) <> ''
       ORDER BY COALESCE(week_start, updated_at) DESC, id ASC
       LIMIT ?`,
    )
    .all(boundedLimit)
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
  { triggeredBy = null, jobId = null } = {},
) {
  if (startedAt && typeof startedAt === "object") {
    const options = startedAt
    startedAt = options.startedAt || new Date().toISOString()
    triggeredBy = options.triggeredBy ?? null
    jobId = options.jobId ?? null
  }
  const result = database
    .prepare(
      `INSERT INTO sync_runs
       (source, started_at, status, triggered_by, job_id)
       VALUES (?, ?, 'running', ?, ?)`,
    )
    .run(source, startedAt, nullable(triggeredBy), nullable(jobId))
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
      `SELECT id, source, started_at AS startedAt, finished_at AS finishedAt,
       status, message, counts_json AS countsJson,
       triggered_by AS triggeredBy, job_id AS jobId
    FROM sync_runs WHERE status != 'running' ORDER BY COALESCE(finished_at, started_at) DESC LIMIT 1`,
    )
    .get()
  if (!row) return null
  const { countsJson: _countsJson, ...summary } = row
  return { ...summary, counts: parseJson(row.countsJson) || {} }
}

export function getSyncRun(database, idOrJobId) {
  const value = String(idOrJobId ?? "").trim()
  if (!value) return null
  const row = database
    .prepare(
      `SELECT id, source, started_at AS startedAt, finished_at AS finishedAt,
       status, message, counts_json AS countsJson,
       triggered_by AS triggeredBy, job_id AS jobId
       FROM sync_runs
       WHERE id = ? OR job_id = ?
       ORDER BY id DESC LIMIT 1`,
    )
    .get(/^[0-9]+$/u.test(value) ? Number(value) : -1, value)
  if (!row) return null
  const { countsJson, ...summary } = row
  return { ...summary, counts: parseJson(countsJson) || {} }
}

export function listSyncRuns(
  database,
  { limit = 20, jobId = null, status = null } = {},
) {
  const boundedLimit = Math.min(100, Math.max(1, Number(limit) || 20))
  const clauses = []
  const values = []
  if (jobId != null && String(jobId).trim()) {
    clauses.push("job_id = ?")
    values.push(String(jobId).trim())
  }
  if (status != null && String(status).trim()) {
    clauses.push("status = ?")
    values.push(String(status).trim())
  }
  const where = clauses.length ? `WHERE ${clauses.join(" AND ")}` : ""
  return database
    .prepare(
      `SELECT id, source, started_at AS startedAt, finished_at AS finishedAt,
       status, message, counts_json AS countsJson,
       triggered_by AS triggeredBy, job_id AS jobId
       FROM sync_runs ${where}
       ORDER BY COALESCE(finished_at, started_at) DESC, id DESC
       LIMIT ?`,
    )
    .all(...values, Math.trunc(boundedLimit))
    .map((row) => {
      const { countsJson, ...summary } = row
      return { ...summary, counts: parseJson(countsJson) || {} }
    })
}

const workflowColumns = `
  id, name, steps_json AS stepsJson, schedule_enabled AS scheduleEnabled,
  interval_minutes AS intervalMinutes, last_run_at AS lastRunAt,
  last_job_id AS lastJobId, last_status AS lastStatus,
  next_run_at AS nextRunAt, created_at AS createdAt, updated_at AS updatedAt
`

function mapWorkflow(row) {
  if (!row) return null
  const { stepsJson, ...workflow } = row
  return {
    ...workflow,
    steps: normalizeWorkflowSteps(stepsJson),
    scheduleEnabled: Boolean(row.scheduleEnabled),
    intervalMinutes: clampWorkflowInterval(row.intervalMinutes),
  }
}

function nextWorkflowRun(fromIso, intervalMinutes) {
  return new Date(
    Date.parse(fromIso) + clampWorkflowInterval(intervalMinutes) * 60_000,
  ).toISOString()
}

export function listWorkflows(database) {
  return database
    .prepare(
      `SELECT ${workflowColumns} FROM workflows ORDER BY created_at ASC, id ASC`,
    )
    .all()
    .map(mapWorkflow)
}

export function getWorkflow(database, id) {
  return mapWorkflow(
    database
      .prepare(`SELECT ${workflowColumns} FROM workflows WHERE id = ?`)
      .get(String(id || "")),
  )
}

/**
 * Create or update a workflow definition. Enabling a schedule (or changing
 * its interval) plans the next run one interval from now instead of firing
 * immediately.
 */
export function upsertWorkflow(
  database,
  workflow = {},
  { now = new Date() } = {},
) {
  const id = String(workflow.id || "").trim()
  if (!/^[a-z0-9][a-z0-9_-]{0,63}$/u.test(id))
    throw new Error("workflow id is invalid")
  const existing = getWorkflow(database, id)
  const steps = normalizeWorkflowSteps(workflow.steps ?? existing?.steps ?? [])
  if (!steps.length) throw new Error("workflow requires at least one step")
  const name =
    String(workflow.name ?? existing?.name ?? id)
      .trim()
      .slice(0, 80) || id
  const scheduleEnabled =
    workflow.scheduleEnabled == null
      ? Boolean(existing?.scheduleEnabled)
      : Boolean(workflow.scheduleEnabled)
  const intervalMinutes = clampWorkflowInterval(
    workflow.intervalMinutes ?? existing?.intervalMinutes ?? 60,
  )
  const timestamp = now.toISOString()
  let nextRunAt = null
  if (scheduleEnabled) {
    const keepPlan =
      existing?.scheduleEnabled &&
      existing.nextRunAt &&
      existing.intervalMinutes === intervalMinutes
    nextRunAt = keepPlan
      ? existing.nextRunAt
      : nextWorkflowRun(timestamp, intervalMinutes)
  }
  database
    .prepare(
      `INSERT INTO workflows
       (id, name, steps_json, schedule_enabled, interval_minutes, next_run_at, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         name=excluded.name, steps_json=excluded.steps_json,
         schedule_enabled=excluded.schedule_enabled,
         interval_minutes=excluded.interval_minutes,
         next_run_at=excluded.next_run_at, updated_at=excluded.updated_at`,
    )
    .run(
      id,
      name,
      JSON.stringify(steps),
      scheduleEnabled ? 1 : 0,
      intervalMinutes,
      nextRunAt,
      timestamp,
      timestamp,
    )
  return getWorkflow(database, id)
}

export function deleteWorkflow(database, id) {
  return (
    database.prepare("DELETE FROM workflows WHERE id = ?").run(String(id || ""))
      .changes > 0
  )
}

export function recordWorkflowStart(
  database,
  id,
  { jobId, startedAt = new Date().toISOString() } = {},
) {
  const workflow = getWorkflow(database, id)
  if (!workflow) return null
  database
    .prepare(
      `UPDATE workflows SET last_run_at = ?, last_job_id = ?, last_status = 'running',
       next_run_at = ?, updated_at = ? WHERE id = ?`,
    )
    .run(
      startedAt,
      nullable(jobId),
      workflow.scheduleEnabled
        ? nextWorkflowRun(startedAt, workflow.intervalMinutes)
        : null,
      startedAt,
      workflow.id,
    )
  return getWorkflow(database, workflow.id)
}

export function recordWorkflowResult(database, id, { jobId, status } = {}) {
  database
    .prepare(
      `UPDATE workflows SET last_status = ?, updated_at = ?
       WHERE id = ? AND (last_job_id = ? OR last_job_id IS NULL)`,
    )
    .run(
      String(status || "unknown").slice(0, 32),
      nowIso(),
      String(id || ""),
      String(jobId || ""),
    )
  return getWorkflow(database, id)
}

export function listDueWorkflows(database, { now = new Date() } = {}) {
  return database
    .prepare(
      `SELECT ${workflowColumns} FROM workflows
       WHERE schedule_enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
       ORDER BY next_run_at ASC, id ASC`,
    )
    .all(now.toISOString())
    .map(mapWorkflow)
}

export function getDashboardRevision(database) {
  const value = getAppSetting(database, "dashboard_revision", "0")
  const revision = Number.parseInt(String(value), 10)
  return Number.isInteger(revision) && revision >= 0 ? revision : 0
}

export function bumpDashboardRevision(database) {
  const timestamp = nowIso()
  database
    .prepare(
      `INSERT INTO app_settings (key, value, updated_at)
       VALUES ('dashboard_revision', '1', ?)
       ON CONFLICT(key) DO UPDATE SET
         value = CAST(COALESCE(app_settings.value, '0') AS INTEGER) + 1,
         updated_at = excluded.updated_at`,
    )
    .run(timestamp)
  return getDashboardRevision(database)
}

const cancelledEventStatuses = new Set([
  "cancelled",
  "canceled",
  "cancel",
  "取消",
  "已取消",
  "中止",
  "キャンセル",
])

export function isCancelledEventStatus(status) {
  const value = String(status || "")
    .trim()
    .toLowerCase()
  return cancelledEventStatuses.has(value) || value.startsWith("cancel")
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
  const postRows = database
    .prepare(
      `SELECT id, source, account_handle AS accountHandle, type, label, text,
       published_at AS publishedAt, url, likes, reposts, replies,
       media_url AS mediaUrl, media_alt AS mediaAlt
    FROM posts ORDER BY published_at DESC`,
    )
    .all()
  const mapPost = (post) => {
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
  }
  const posts = postRows
    .filter((post) => {
      const timestamp = Date.parse(post.publishedAt)
      return Number.isNaN(timestamp) || timestamp >= windowStart
    })
    .map(mapPost)

  const todayKey = dateKeyInJapan(now)
  const events = database
    .prepare(
      `SELECT id, source, source_item_id AS sourceItemId, title, detail,
       starts_on AS startsOn, starts_at AS startsAt, ends_at AS endsAt,
       timezone, time_precision AS timePrecision, status,
       event_type AS eventType, url, provenance, manual_locked AS manualLocked,
       cancellation_status AS cancellationStatus,
       cancellation_source AS cancellationSource,
       cancellation_reason AS cancellationReason,
       cancellation_evidence AS cancellationEvidence,
       cancellation_at AS cancellationAt
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
      `SELECT id, kind, url, source_url AS sourceUrl, alt,
       week_start AS weekStart, source_account AS sourceAccount,
       updated_at AS updatedAt FROM assets ORDER BY id ASC`,
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
  const scheduleReviewsById = new Map(
    listScheduleAssetReviews(database, { limit: 200 }).map((review) => [
      review.id,
      review,
    ]),
  )
  const validAssets = assets.filter(
    (asset) =>
      asset.kind !== "schedule" ||
      Boolean(scheduleReviewsById.get(asset.id)?.approved),
  )
  const scheduleImages = validAssets
    .filter((asset) => asset.kind === "schedule" && asset.url)
    .sort((a, b) => {
      const weekDelta = String(b.weekStart || "").localeCompare(
        String(a.weekStart || ""),
      )
      return (
        weekDelta ||
        String(b.updatedAt || "").localeCompare(String(a.updatedAt || ""))
      )
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
        const activeProfileMedia = profileMediaSlots(database)
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
          avatarUrl: activeProfileMedia.avatar?.publicUrl || avatar.publicUrl,
          avatarId: activeProfileMedia.avatar?.id || avatar.id,
          avatarStatus: activeProfileMedia.avatar?.status || avatar.status,
          avatarSourceUrl:
            activeProfileMedia.avatar?.sourceUrl || avatar.sourceUrl,
          avatarSource: activeProfileMedia.avatar?.source || null,
          bannerUrl: activeProfileMedia.banner?.publicUrl || banner.publicUrl,
          bannerId: activeProfileMedia.banner?.id || banner.id,
          bannerStatus: activeProfileMedia.banner?.status || banner.status,
          bannerSourceUrl:
            activeProfileMedia.banner?.sourceUrl || banner.sourceUrl,
          bannerSource: activeProfileMedia.banner?.source || null,
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

  // The summary answers "what should a visitor look at first" without making
  // every client re-derive it. It only references rows already in the payload,
  // except latestPost, which survives an empty post window.
  const upcomingEvents = events.filter(
    (event) =>
      event.isUpcoming &&
      !isCancelledEventStatus(event.status) &&
      event.cancellationStatus !== "llm_suspected" &&
      event.cancellationStatus !== "manual_confirmed",
  )
  const upcomingVideos = mappedVideos
    .filter((video) => video.isUpcoming)
    .sort((a, b) =>
      String(a.scheduledAt || "").localeCompare(String(b.scheduledAt || "")),
    )
  const summary = {
    nextEvent: upcomingEvents[0] || null,
    nextStream: upcomingVideos[0] || null,
    latestVideo: mappedVideos.find((video) => !video.isUpcoming) || null,
    latestPost: posts[0] || (postRows[0] ? mapPost(postRows[0]) : null),
    counts: {
      posts: posts.length,
      events: events.length,
      upcomingEvents: upcomingEvents.length,
      videos: mappedVideos.length,
      resources: resources.length,
    },
  }

  return {
    profile,
    summary,
    posts,
    events,
    videos: mappedVideos,
    focus,
    timeline,
    resources,
    assets: validAssets,
    scheduleImages,
    profileMedia: profileMediaSlots(database),
    meta: {
      fetchedAt,
      lastSync: latestSync,
      postWindowDays: days,
      generatedAt: now.toISOString(),
      mediaCache,
      revision: getDashboardRevision(database),
      featuredVideoId,
      xAccounts: configuredXHandles(database),
    },
  }
}
