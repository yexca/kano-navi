package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrMediaNotFound        = errors.New("media not found")
	ErrMediaNotReady        = errors.New("media not ready")
	ErrMediaVersionNotFound = errors.New("media version not found")
)

// Source media downloads are capped at 10 MiB by the TypeScript media
// downloader. Public reads enforce the same bound before allocating a byte
// slice, so a corrupt or operator-edited cache row cannot trigger an
// unbounded read.
const maxCachedMediaBytes = 10 * 1024 * 1024

type MediaFile struct {
	Content      []byte
	MIMEType     string
	SHA256       string
	Size         int64
	LastModified time.Time
}

type ProfileMediaFile struct {
	Content  []byte
	MIMEType string
	SHA256   string
	Size     int64
}

func allowedMediaMIME(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	switch normalized {
	case "image/avif", "image/gif", "image/jpeg", "image/png", "image/webp":
		return normalized, true
	default:
		return "", false
	}
}

func (s *Store) ReadMedia(ctx context.Context, id, version string) (*MediaFile, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	if !safeHash(id) {
		return nil, ErrMediaNotFound
	}
	var cachePath, mimeType, contentHash, status, fetchedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT cache_path,mime_type,sha256,status,fetched_at FROM media_assets WHERE id = ?`, id).Scan(&cachePath, &mimeType, &contentHash, &status, &fetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMediaNotReady
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if status.String != "ready" || !contentHash.Valid || !safeHash(contentHash.String) || !cachePath.Valid {
		return nil, ErrMediaNotReady
	}
	if version != "" {
		if !safeHash(version) {
			return nil, ErrMediaVersionNotFound
		}
		// An earlier file a revalidation replaced stays servable by its hash,
		// so an operator can keep showing it.
		if version != contentHash.String {
			err = s.db.QueryRowContext(ctx, `SELECT cache_path,mime_type,sha256,fetched_at FROM media_versions WHERE media_id=? AND sha256=?`, id, version).Scan(&cachePath, &mimeType, &contentHash, &fetchedAt)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrMediaVersionNotFound
			}
			if err != nil {
				return nil, dbErr(err)
			}
		}
	}
	mime, ok := allowedMediaMIME(mimeType.String)
	if !ok {
		return nil, ErrMediaNotFound
	}
	path, err := s.resolveMediaPath(cachePath.String)
	if err != nil {
		return nil, ErrMediaNotFound
	}
	content, modTime, err := readContainedFile(path, maxCachedMediaBytes)
	if err != nil {
		return nil, ErrMediaNotFound
	}
	actual := sha256.Sum256(content)
	actualHash := hex.EncodeToString(actual[:])
	if actualHash != contentHash.String {
		return nil, ErrMediaNotFound
	}
	lastModified := modTime
	if fetchedAt.Valid {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, fetchedAt.String); parseErr == nil {
			lastModified = parsed
		}
	}
	return &MediaFile{Content: content, MIMEType: mime, SHA256: contentHash.String, Size: int64(len(content)), LastModified: lastModified}, nil
}

func (s *Store) ReadProfileMedia(ctx context.Context, slot, version string) (*ProfileMediaFile, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	slot = strings.ToLower(strings.TrimSpace(slot))
	if slot != "avatar" && slot != "banner" {
		return nil, ErrMediaNotFound
	}
	var cachePath, activeHash, activeMIME, status sql.NullString
	var active int64
	err := s.db.QueryRowContext(ctx, `SELECT active_cache_path,active_sha256,active_mime_type,status,is_active FROM profile_media WHERE slot = ? AND is_active = 1 LIMIT 1`, slot).Scan(&cachePath, &activeHash, &activeMIME, &status, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMediaNotReady
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if active == 0 || !cachePath.Valid || !activeHash.Valid || !safeHash(activeHash.String) {
		return nil, ErrMediaNotReady
	}
	if version != "" {
		if !safeHash(version) || version != activeHash.String {
			return nil, ErrMediaVersionNotFound
		}
	}
	path, err := s.resolveProfileMediaPath(cachePath.String)
	if err != nil {
		return nil, ErrMediaNotFound
	}
	content, _, err := readContainedFile(path, 15*1024*1024)
	if err != nil {
		return nil, ErrMediaNotFound
	}
	mime := sniffImageMIME(content)
	if mime == "" || !isAllowedMIME(mime) || !activeMIME.Valid || mime != activeMIME.String {
		return nil, ErrMediaNotFound
	}
	actual := sha256.Sum256(content)
	actualHash := hex.EncodeToString(actual[:])
	if actualHash != activeHash.String {
		return nil, ErrMediaNotFound
	}
	return &ProfileMediaFile{Content: content, MIMEType: mime, SHA256: activeHash.String, Size: int64(len(content))}, nil
}

func isAllowedMIME(value string) bool {
	_, ok := allowedMediaMIME(value)
	return ok
}

func (s *Store) resolveMediaPath(relative string) (string, error) {
	normalized := strings.ReplaceAll(relative, `\`, "/")
	if normalized == "" || strings.ContainsRune(normalized, 0) || filepath.IsAbs(normalized) {
		return "", ErrMediaNotFound
	}
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		// Empty components are harmless and are normalized by Join; parent
		// components are never accepted from database-controlled paths.
		if part == ".." {
			return "", ErrMediaNotFound
		}
	}
	root := filepath.Clean(s.dataRoot)
	if strings.HasPrefix(normalized, "sha256/") {
		root = filepath.Join(root, "cache", "media")
	} else {
		if len(parts) == 0 || (parts[0] != "x" && parts[0] != "youtube" && parts[0] != "avatar") {
			return "", ErrMediaNotFound
		}
	}
	candidate := filepath.Join(root, filepath.FromSlash(normalized))
	if !pathInside(candidate, root) {
		return "", ErrMediaNotFound
	}
	return containedRegularPath(candidate, root)
}

func (s *Store) resolveProfileMediaPath(relative string) (string, error) {
	normalized := strings.ReplaceAll(relative, `\`, "/")
	if normalized == "" || strings.ContainsRune(normalized, 0) || filepath.IsAbs(normalized) {
		return "", ErrMediaNotFound
	}
	parts := strings.Split(normalized, "/")
	if len(parts) < 2 || parts[0] != "avatar" {
		return "", ErrMediaNotFound
	}
	for _, part := range parts {
		if part == ".." || part == "" {
			return "", ErrMediaNotFound
		}
	}
	root := filepath.Join(s.dataRoot, "avatar")
	candidate := filepath.Join(s.dataRoot, filepath.FromSlash(normalized))
	if !pathInside(candidate, root) {
		return "", ErrMediaNotFound
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil || resolvedRoot != filepath.Clean(root) {
		// The selected-media namespace itself must be a real directory. A
		// symlinked avatar root would make an otherwise contained path escape
		// the configured runtime namespace.
		return "", ErrMediaNotFound
	}
	return containedRegularPath(candidate, root)
}

func pathInside(candidate, root string) bool {
	candidate, _ = filepath.Abs(candidate)
	root, _ = filepath.Abs(root)
	return candidate == root || strings.HasPrefix(candidate, root+string(filepath.Separator))
}

func containedRegularPath(candidate, root string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || !pathInside(resolved, resolvedRoot) {
		return "", ErrMediaNotFound
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrMediaNotFound
	}
	return resolved, nil
}

func readContainedFile(path string, maxBytes int64) ([]byte, time.Time, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || maxBytes > 0 && info.Size() > maxBytes {
		return nil, time.Time{}, errors.New("invalid media file")
	}
	if info.Size() > int64(^uint(0)>>1) {
		return nil, time.Time{}, errors.New("media file too large")
	}
	content, err := io.ReadAll(file)
	if err != nil || int64(len(content)) != info.Size() {
		return nil, time.Time{}, errors.New("media file could not be read")
	}
	return content, info.ModTime(), nil
}

func sniffImageMIME(content []byte) string {
	if len(content) >= 3 && content[0] == 0xff && content[1] == 0xd8 && content[2] == 0xff {
		return "image/jpeg"
	}
	if len(content) >= 8 && string(content[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	if len(content) >= 6 && (string(content[:6]) == "GIF87a" || string(content[:6]) == "GIF89a") {
		return "image/gif"
	}
	if len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WEBP" {
		return "image/webp"
	}
	if len(content) >= 12 && string(content[4:8]) == "ftyp" && (string(content[8:12]) == "avif" || string(content[8:12]) == "avis") {
		return "image/avif"
	}
	return ""
}
