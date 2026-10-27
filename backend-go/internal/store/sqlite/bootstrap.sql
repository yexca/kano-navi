CREATE TABLE app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );

CREATE TABLE assets (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    url TEXT NOT NULL,
    source_url TEXT,
    alt TEXT,
    week_start TEXT,
    source_account TEXT,
    updated_at TEXT,
    raw_json TEXT
  , asset_version INTEGER NOT NULL DEFAULT 1, period_start TEXT, period_end TEXT, period_basis TEXT NOT NULL DEFAULT 'legacy', manual_period_start TEXT, manual_period_end TEXT, manual_period_reason TEXT);

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
  , merged_into TEXT, merge_source TEXT, merge_reason TEXT, merged_at TEXT);

CREATE TABLE focus (
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

CREATE TABLE llm_models (
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

CREATE TABLE llm_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    protocol TEXT NOT NULL DEFAULT 'openai-responses',
    base_url TEXT NOT NULL,
    model TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    vision_capable INTEGER NOT NULL DEFAULT 1,
    capabilities_json TEXT NOT NULL DEFAULT '["text","image"]',
    timeout_ms INTEGER NOT NULL DEFAULT 30000,
    max_retries INTEGER NOT NULL DEFAULT 2,
    api_key_ciphertext TEXT,
    last_status TEXT,
    last_error TEXT,
    last_checked_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );

CREATE TABLE llm_route_providers (
    route TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES llm_providers (id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (route, provider_id),
    UNIQUE (route, priority)
  );

CREATE TABLE llm_route_targets (
    route TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES llm_providers (id) ON DELETE CASCADE,
    model_id TEXT NOT NULL DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (route, provider_id, model_id),
    UNIQUE (route, priority)
  );

CREATE TABLE media_assets (
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
    retry_attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    raw_json TEXT
  );

CREATE TABLE media_links (
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

CREATE TABLE media_versions (
    media_id TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    cache_path TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    byte_size INTEGER,
    fetched_at TEXT,
    replaced_at TEXT NOT NULL,
    PRIMARY KEY (media_id, sha256)
  );

CREATE TABLE post_llm_states (
    post_id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'never',
    route TEXT,
    last_attempt_at TEXT,
    last_processed_at TEXT,
    last_error TEXT,
    last_extraction_id INTEGER,
    reprocess_requested INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    failure_count INTEGER NOT NULL DEFAULT 0,
    failure_fingerprint TEXT,
    next_attempt_at TEXT
  );

CREATE TABLE posts (
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

CREATE TABLE profile_media (
    id TEXT PRIMARY KEY,
    slot TEXT NOT NULL,
    source TEXT NOT NULL,
    source_ref TEXT,
    source_url TEXT,
    cache_path TEXT,
    active_cache_path TEXT,
    active_sha256 TEXT,
    active_mime_type TEXT,
    download_status TEXT,
    last_download_at TEXT,
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

CREATE TABLE profiles (
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

CREATE TABLE resources (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    detail TEXT,
    icon TEXT,
    tone TEXT,
    url TEXT NOT NULL,
    sort_order INTEGER DEFAULT 0,
    raw_json TEXT
  );

CREATE TABLE schedule_asset_reviews (
    asset_id TEXT PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    llm_status TEXT NOT NULL DEFAULT 'pending',
    llm_confidence REAL,
    llm_reason TEXT,
    llm_evidence TEXT,
    llm_model TEXT,
    llm_checked_at TEXT,
    llm_input_fingerprint TEXT,
    manual_status TEXT NOT NULL DEFAULT 'unreviewed',
    manual_reason TEXT,
    manual_checked_at TEXT,
    updated_at TEXT NOT NULL
  );

CREATE TABLE schedule_extractions (
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

CREATE TABLE source_schedules (
    source TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    interval_minutes INTEGER NOT NULL,
    sync_json TEXT NOT NULL DEFAULT '{}',
    next_run_at TEXT,
    updated_at TEXT NOT NULL
  );

CREATE TABLE sync_runs (
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

CREATE TABLE sync_state (
    source TEXT NOT NULL,
    account_id TEXT NOT NULL,
    cursor_id TEXT,
    cursor_time TEXT,
    last_success_at TEXT,
    metadata_json TEXT,
    PRIMARY KEY (source, account_id)
  );

CREATE TABLE timeline (
    id INTEGER PRIMARY KEY,
    year TEXT NOT NULL,
    title TEXT NOT NULL,
    detail TEXT,
    sort_order INTEGER DEFAULT 0,
    raw_json TEXT
  );

CREATE TABLE translation_states (
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    field TEXT NOT NULL,
    source_hash TEXT,
    source_language TEXT,
    status TEXT NOT NULL DEFAULT 'never',
    last_attempt_at TEXT,
    last_error TEXT,
    requested INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (owner_type, owner_id, field)
  );

CREATE TABLE translations (
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    field TEXT NOT NULL,
    locale TEXT NOT NULL,
    origin TEXT NOT NULL,
    platform TEXT NOT NULL DEFAULT '',
    text TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    provider_id TEXT,
    model_id TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (owner_type, owner_id, field, locale, origin, platform)
  );

CREATE TABLE video_revisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id TEXT NOT NULL,
    origin TEXT NOT NULL,
    title TEXT,
    thumbnail_url TEXT,
    thumbnail_media_id TEXT,
    thumbnail_sha256 TEXT,
    kind TEXT,
    replaced_at TEXT NOT NULL
  );

CREATE TABLE videos (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    title TEXT NOT NULL,
    published_at TEXT,
    scheduled_at TEXT,
    url TEXT NOT NULL,
    thumbnail_url TEXT,
    kind TEXT,
    is_upcoming INTEGER DEFAULT 0,
    raw_json TEXT,
    live_status TEXT,
    live_checked_at TEXT,
    duration_seconds INTEGER,
    manual_title TEXT,
    manual_kind TEXT,
    manual_thumbnail_media_id TEXT,
    manual_thumbnail_sha256 TEXT,
    manual_updated_at TEXT,
    refresh_requested_at TEXT,
    merged_into TEXT,
    merge_source TEXT,
    merge_reason TEXT,
    merged_at TEXT,
    is_short INTEGER,
    manual_is_short INTEGER,
    is_premiere INTEGER
  );

CREATE TABLE workflows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    steps_json TEXT NOT NULL,
    source_lookback_json TEXT NOT NULL DEFAULT '{}',
    source_sync_json TEXT NOT NULL DEFAULT '{}',
    schedule_enabled INTEGER NOT NULL DEFAULT 0,
    interval_minutes INTEGER NOT NULL DEFAULT 60,
    last_run_at TEXT,
    last_job_id TEXT,
    last_status TEXT,
    next_run_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );

CREATE INDEX event_sources_event_idx ON event_sources (event_id);

CREATE UNIQUE INDEX event_sources_identity_idx
    ON event_sources (source, source_item_id, source_key);

CREATE INDEX events_instant_idx ON events (COALESCE(julianday(starts_at), julianday(starts_on || 'T23:59:59.999+09:00')), id) WHERE deleted_at IS NULL;

CREATE INDEX events_merged_into_idx ON events (merged_into) WHERE merged_into IS NOT NULL;

CREATE INDEX events_source_item_idx ON events (source, source_item_id, id);

CREATE UNIQUE INDEX events_source_key_idx
      ON events (source, source_item_id, source_key)
      WHERE source_item_id IS NOT NULL AND source_key IS NOT NULL;

CREATE INDEX events_starts_at_idx
      ON events (starts_on, starts_at);

CREATE INDEX events_stream_identity_idx ON events (stream_identity(url), id) WHERE url IS NOT NULL;

CREATE INDEX llm_route_providers_order_idx
    ON llm_route_providers (route, priority);

CREATE UNIQUE INDEX media_assets_source_url_idx ON media_assets (source_url);

CREATE INDEX media_assets_status_idx ON media_assets (status, updated_at DESC);

CREATE INDEX media_links_media_idx ON media_links (media_id);

CREATE UNIQUE INDEX media_links_owner_position_idx ON media_links (owner_type, owner_id, role, position);

CREATE INDEX post_llm_states_status_idx
    ON post_llm_states (status, reprocess_requested, updated_at DESC);

CREATE INDEX posts_account_handle_idx ON posts (account_handle, published_at DESC);

CREATE INDEX posts_instant_idx ON posts (julianday(published_at) DESC, id);

CREATE INDEX posts_published_at_idx ON posts (published_at DESC);

CREATE INDEX posts_url_published_at_idx ON posts (url, published_at DESC);

CREATE INDEX profile_media_slot_idx
    ON profile_media (slot, is_active, updated_at DESC);

CREATE INDEX schedule_asset_reviews_status_idx
    ON schedule_asset_reviews (llm_status, manual_status, updated_at DESC);

CREATE UNIQUE INDEX schedule_extractions_identity_idx
    ON schedule_extractions (source, source_item_id, content_fingerprint, extractor_version);

CREATE INDEX sync_runs_finished_at_idx ON sync_runs (finished_at DESC);

CREATE INDEX sync_runs_job_idx ON sync_runs (job_id, id DESC);

CREATE INDEX video_revisions_video_idx ON video_revisions (video_id, id DESC);

CREATE INDEX videos_date_idx ON videos (COALESCE(scheduled_at, published_at) DESC);

CREATE INDEX videos_instant_idx ON videos (julianday(COALESCE(scheduled_at, published_at)) DESC, id);

CREATE INDEX videos_merged_into_idx ON videos (merged_into) WHERE merged_into IS NOT NULL;

CREATE INDEX videos_schedule_instant_idx ON videos (julianday(scheduled_at), id);
