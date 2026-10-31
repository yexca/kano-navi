package contentadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// ProfileMedia is one profile image candidate as the admin console reads it.
// cachePath values are relative to the runtime data directory.
type ProfileMedia struct {
	ID              string  `json:"id"`
	Slot            string  `json:"slot"`
	Source          string  `json:"source"`
	SourceRef       *string `json:"sourceRef"`
	SourceURL       *string `json:"sourceUrl"`
	CachePath       *string `json:"cachePath"`
	ActiveCachePath *string `json:"activeCachePath"`
	ActiveSHA256    *string `json:"activeSha256"`
	ActiveMIMEType  *string `json:"activeMimeType"`
	DownloadStatus  *string `json:"downloadStatus"`
	LastDownloadAt  *string `json:"lastDownloadAt"`
	MIMEType        *string `json:"mimeType"`
	Extension       *string `json:"extension"`
	ByteSize        *int64  `json:"byteSize"`
	SHA256          *string `json:"sha256"`
	Width           *int64  `json:"width"`
	Height          *int64  `json:"height"`
	Status          string  `json:"status"`
	IsActive        bool    `json:"isActive"`
	LastError       *string `json:"lastError"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	PublicURL       *string `json:"publicUrl"`
}

// profileMediaListItem adds the admin-only preview link used by list payloads.
type profileMediaListItem struct {
	ProfileMedia
	PreviewURL *string `json:"previewUrl"`
}

type profileMediaPayload struct {
	Items  []profileMediaListItem `json:"items"`
	Active struct {
		Avatar *profileMediaListItem `json:"avatar"`
		Banner *profileMediaListItem `json:"banner"`
	} `json:"active"`
}

const profileMediaColumns = `id, slot, source, source_ref, source_url, cache_path, active_cache_path,
  active_sha256, active_mime_type, download_status, last_download_at, mime_type, extension,
  byte_size, sha256, width, height, status, is_active, last_error, created_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func textPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func intPointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func scanProfileMedia(row rowScanner) (*ProfileMedia, error) {
	var item ProfileMedia
	var sourceRef, sourceURL, cachePath, activeCachePath, activeSHA, activeMIME, downloadStatus, lastDownloadAt sql.NullString
	var mimeType, extension, contentHash, lastError, createdAt, updatedAt sql.NullString
	var byteSize, width, height, active sql.NullInt64
	err := row.Scan(&item.ID, &item.Slot, &item.Source, &sourceRef, &sourceURL, &cachePath, &activeCachePath,
		&activeSHA, &activeMIME, &downloadStatus, &lastDownloadAt, &mimeType, &extension,
		&byteSize, &contentHash, &width, &height, &item.Status, &active, &lastError, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	item.SourceRef, item.SourceURL, item.CachePath = textPointer(sourceRef), textPointer(sourceURL), textPointer(cachePath)
	item.ActiveCachePath, item.ActiveSHA256, item.ActiveMIMEType = textPointer(activeCachePath), textPointer(activeSHA), textPointer(activeMIME)
	item.DownloadStatus, item.LastDownloadAt = textPointer(downloadStatus), textPointer(lastDownloadAt)
	item.MIMEType, item.Extension, item.SHA256, item.LastError = textPointer(mimeType), textPointer(extension), textPointer(contentHash), textPointer(lastError)
	item.ByteSize, item.Width, item.Height = intPointer(byteSize), intPointer(width), intPointer(height)
	item.CreatedAt, item.UpdatedAt = createdAt.String, updatedAt.String
	item.IsActive = active.Valid && active.Int64 != 0
	if item.IsActive && item.ActiveCachePath != nil && *item.ActiveCachePath != "" && item.ActiveSHA256 != nil && safeHash(*item.ActiveSHA256) {
		publicURL := "/media/profile/" + item.Slot + "?v=" + *item.ActiveSHA256
		item.PublicURL = &publicURL
	}
	return &item, nil
}

// getProfileMedia returns nil without an error when the candidate is unknown.
func getProfileMedia(ctx context.Context, q queryer, id string) (*ProfileMedia, error) {
	if id == "" {
		return nil, nil
	}
	item, err := scanProfileMedia(q.QueryRowContext(ctx, `SELECT `+profileMediaColumns+` FROM profile_media WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// listProfileMedia reads every row and closes the cursor before returning so
// the single pooled connection is free for the next statement.
func listProfileMedia(ctx context.Context, q queryer, slot string) ([]ProfileMedia, error) {
	query := `SELECT ` + profileMediaColumns + ` FROM profile_media ORDER BY slot ASC, is_active DESC, updated_at DESC, id ASC`
	args := []any{}
	if slot != "" {
		query = `SELECT ` + profileMediaColumns + ` FROM profile_media WHERE slot = ? ORDER BY is_active DESC, updated_at DESC, id ASC`
		args = append(args, slot)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProfileMedia{}
	for rows.Next() {
		item, scanErr := scanProfileMedia(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, rows.Close()
}

func (m *Module) profilePayload(ctx context.Context) (*profileMediaPayload, error) {
	items, err := listProfileMedia(ctx, m.db, "")
	if err != nil {
		return nil, err
	}
	payload := &profileMediaPayload{Items: make([]profileMediaListItem, 0, len(items))}
	for _, item := range items {
		entry := profileMediaListItem{ProfileMedia: item}
		if item.Status == "ready" {
			preview := "/api/admin/profile-media/" + encodeURIComponent(item.ID) + "/preview"
			entry.PreviewURL = &preview
		}
		payload.Items = append(payload.Items, entry)
	}
	for i := range payload.Items {
		entry := &payload.Items[i]
		if !entry.IsActive {
			continue
		}
		if entry.Slot == "avatar" && payload.Active.Avatar == nil {
			payload.Active.Avatar = entry
		}
		if entry.Slot == "banner" && payload.Active.Banner == nil {
			payload.Active.Banner = entry
		}
	}
	return payload, nil
}

func normalizedProfileSlot(value string) string {
	slot := strings.ToLower(jsTrim(value))
	if slot == "avatar" || slot == "banner" {
		return slot
	}
	return ""
}

func normalizedProfileSource(value string) string {
	source := strings.ToLower(jsTrim(value))
	if source == "x" || source == "youtube" || source == "upload" {
		return source
	}
	return ""
}

func profileCandidateID(slot, sourceURL string) string {
	return sha256Hex([]byte(slot + "\x00" + sourceURL))
}

type profileCandidate struct {
	Slot      string  `json:"slot"`
	Source    string  `json:"source"`
	SourceRef *string `json:"sourceRef"`
	SourceURL *string `json:"sourceUrl,omitempty"`
}

// upsertProfileCandidate registers a candidate without downloading it. A
// ready candidate keeps its cached file and status when it is seen again.
func upsertProfileCandidate(ctx context.Context, q queryer, candidate profileCandidate, now string) (string, error) {
	slot := normalizedProfileSlot(candidate.Slot)
	source := normalizedProfileSource(candidate.Source)
	if slot == "" || source == "" {
		return "", failure("invalid profile media candidate")
	}
	sourceURL := ""
	if candidate.SourceURL != nil {
		sourceURL, _ = normalizeSourceURL(*candidate.SourceURL)
	}
	if source != "upload" && sourceURL == "" {
		return "", failure("profile media requires an HTTP(S) source URL")
	}
	id := ""
	if sourceURL != "" {
		id = profileCandidateID(slot, sourceURL)
	} else {
		uuid, err := randomUUID()
		if err != nil {
			return "", err
		}
		id = "upload-" + uuid
	}
	stored := profileCandidate{Slot: slot, Source: source, SourceRef: candidate.SourceRef}
	if sourceURL != "" {
		stored.SourceURL = &sourceURL
	}
	rawJSON, err := json.Marshal(stored)
	if err != nil {
		return "", err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO profile_media (
	     id, slot, source, source_ref, source_url, status, is_active, created_at, updated_at, raw_json
	   ) VALUES (?, ?, ?, ?, ?, 'pending', 0, ?, ?, ?)
	   ON CONFLICT(id) DO UPDATE SET
	     slot=excluded.slot, source=excluded.source,
	     source_ref=COALESCE(excluded.source_ref, profile_media.source_ref),
	     source_url=COALESCE(excluded.source_url, profile_media.source_url),
	     status=CASE WHEN profile_media.status = 'ready' THEN 'ready' ELSE 'pending' END,
	     last_error=CASE WHEN profile_media.status = 'ready' THEN profile_media.last_error ELSE NULL END,
	     updated_at=excluded.updated_at,
	     raw_json=COALESCE(excluded.raw_json, profile_media.raw_json)`,
		id, slot, source, nullString(candidate.SourceRef), nullString(&sourceURL), now, now, string(rawJSON))
	return id, err
}

// markProfileMediaReady records verified bytes that are already on disk.
func markProfileMediaReady(ctx context.Context, q queryer, id string, image *storedImage, now string) error {
	_, err := q.ExecContext(ctx, `UPDATE profile_media SET cache_path=?, mime_type=?, extension=?, byte_size=?,
	     sha256=?, width=?, height=?, status='ready', download_status='success', last_download_at=?, last_error=NULL, updated_at=?
	   WHERE id=?`,
		image.relativePath, image.mimeType, image.extension, image.byteSize, image.sha256, nullInt(image.width), nullInt(image.height), now, now, id)
	return err
}

// markProfileMediaFailed keeps a previously ready candidate usable and stores
// only a fixed, sanitized description of the failure.
func markProfileMediaFailed(ctx context.Context, q queryer, id, message, now string) error {
	if message == "" {
		message = "profile media download failed"
	}
	_, err := q.ExecContext(ctx, `UPDATE profile_media SET status=CASE WHEN cache_path IS NOT NULL AND sha256 IS NOT NULL THEN 'ready' ELSE 'failed' END,
	     download_status='failed', last_download_at=?, last_error=?, updated_at=? WHERE id=?`,
		now, jsSlice(message, 500), now, id)
	return err
}
