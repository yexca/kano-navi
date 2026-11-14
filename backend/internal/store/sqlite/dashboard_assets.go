package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func (s *Store) queryScheduleAssets(ctx context.Context, from, to string) ([]Asset, error) {
	period := SchedulePeriodSQL("a")
	q := `SELECT a.id,a.kind,a.alt,` + period.WeekStart + `,` + period.Start + `,` + period.End + `,` + period.Basis + `,a.source_account,a.updated_at,a.url,a.source_url,COALESCE(r.manual_status,'unreviewed'),COALESCE(r.llm_status,'pending'),(SELECT COALESCE(json_extract(p.raw_json,'$.search_text'),p.text) FROM posts p WHERE p.url=a.source_url ORDER BY p.published_at DESC LIMIT 1) FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id=a.id WHERE a.kind='schedule' AND ` + LiveScheduleAssetSQL("a") + ` AND ` + period.Start + `<=? AND ` + period.End + `>=? AND (r.manual_status='schedule' OR (COALESCE(r.manual_status,'unreviewed')='unreviewed' AND r.llm_status='schedule')) ORDER BY a.updated_at DESC,a.id ASC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, q, to, from)
	if err != nil {
		return nil, dbErr(err)
	}
	assets := []Asset{}
	for rows.Next() {
		a, manualStatus, llmStatus, sourceText, err := scanScheduleAsset(rows)
		if err != nil {
			return nil, err
		}
		if manualStatus.String == "unreviewed" && llmStatus.String == "schedule" && sourceText.Valid && !scheduleBoardText.MatchString(sourceText.String) {
			continue
		}
		assets = append(assets, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, dbErr(err)
	}
	_ = rows.Close()
	if err = s.prefetchAssetMedia(ctx, assets); err != nil {
		return nil, err
	}
	out := []Asset{}
	for _, a := range assets {
		ref, e := s.resolveMedia(ctx, a.URL)
		if e != nil {
			return nil, e
		}
		a.MediaID, a.MediaStatus, a.MediaSourceURL, a.URL = ref.ID, ref.Status, ref.SourceURL, ref.PublicURL
		if a.SourceURL == nil {
			a.SourceURL = ref.SourceURL
		}
		out = append(out, a)
	}
	return out, nil
}

var scheduleBoardText = regexp.MustCompile(`(?i)(\b(?:weekly[\s_-]+)?schedule(?:[\s_-]+(?:board|table))?\b|\bthis\s+week(?:'s)?\s+schedule\b|スケジュール|予定表|(?:今週|来週|今月|週間).{0,4}(?:予定|スケジュール))`)

func (s *Store) queryOtherAssets(ctx context.Context) ([]Asset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,alt,NULL,NULL,NULL,NULL,source_account,updated_at,url,source_url FROM assets WHERE kind!='schedule' ORDER BY id LIMIT 100`)
	if err != nil {
		return nil, dbErr(err)
	}
	assets := []Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, dbErr(err)
	}
	_ = rows.Close()
	if err = s.prefetchAssetMedia(ctx, assets); err != nil {
		return nil, err
	}
	out := []Asset{}
	for _, a := range assets {
		ref, e := s.resolveMedia(ctx, a.URL)
		if e != nil {
			return nil, e
		}
		a.MediaID, a.MediaStatus, a.MediaSourceURL, a.URL = ref.ID, ref.Status, ref.SourceURL, ref.PublicURL
		out = append(out, a)
	}
	return out, nil
}

func scanAsset(row scanner) (Asset, error) {
	var a Asset
	var alt, ws, ps, pe, pb, sa, ua, u, su sql.NullString
	if err := row.Scan(&a.ID, &a.Kind, &alt, &ws, &ps, &pe, &pb, &sa, &ua, &u, &su); err != nil {
		return a, dbErr(err)
	}
	if alt.Valid {
		a.Alt = &alt.String
	}
	if ws.Valid {
		a.WeekStart = &ws.String
	}
	if ps.Valid {
		a.PeriodStart = &ps.String
	}
	if pe.Valid {
		a.PeriodEnd = &pe.String
	}
	if pb.Valid {
		a.PeriodBasis = &pb.String
	}
	if sa.Valid {
		a.SourceAccount = &sa.String
	}
	if ua.Valid {
		a.UpdatedAt = &ua.String
	}
	if u.Valid {
		a.URL = &u.String
	}
	if su.Valid {
		a.SourceURL = &su.String
	}
	return a, nil
}

func scanScheduleAsset(row scanner) (Asset, sql.NullString, sql.NullString, sql.NullString, error) {
	var a Asset
	var alt, ws, ps, pe, pb, sa, ua, u, su, manual, llm, sourceText sql.NullString
	if err := row.Scan(&a.ID, &a.Kind, &alt, &ws, &ps, &pe, &pb, &sa, &ua, &u, &su, &manual, &llm, &sourceText); err != nil {
		return a, manual, llm, sourceText, dbErr(err)
	}
	if alt.Valid {
		a.Alt = &alt.String
	}
	if ws.Valid {
		a.WeekStart = &ws.String
	}
	if ps.Valid {
		a.PeriodStart = &ps.String
	}
	if pe.Valid {
		a.PeriodEnd = &pe.String
	}
	if pb.Valid {
		a.PeriodBasis = &pb.String
	}
	if sa.Valid {
		a.SourceAccount = &sa.String
	}
	if ua.Valid {
		a.UpdatedAt = &ua.String
	}
	if u.Valid {
		a.URL = &u.String
	}
	if su.Valid {
		a.SourceURL = &su.String
	}
	return a, manual, llm, sourceText, nil
}

func filterReadyAssets(in []Asset) []Asset {
	out := []Asset{}
	for _, a := range in {
		if a.URL != nil {
			out = append(out, a)
		}
	}
	return out
}

func (s *Store) resolveMedia(ctx context.Context, source *string) (*mediaReference, error) {
	if source == nil || strings.TrimSpace(*source) == "" {
		return &mediaReference{Status: "missing"}, nil
	}
	v := strings.TrimSpace(*source)
	if strings.HasPrefix(v, "/assets/") {
		if s.publicAssetExists(v) {
			return &mediaReference{Status: "ready", PublicURL: strPtr(v)}, nil
		}
		return &mediaReference{Status: "missing"}, nil
	}
	norm := normalizeSourceURL(v)
	if norm == "" {
		return &mediaReference{Status: "missing"}, nil
	}
	if memo := readMemoFrom(ctx); memo != nil {
		if a, known := memo.assets[norm]; known {
			if a == nil {
				id := sha256Hex(norm)
				return &mediaReference{ID: &id, Status: "pending", SourceURL: &norm}, nil
			}
			return s.mediaReference(ctx, *a), nil
		}
	}
	a, err := scanMediaAsset(s.db.QueryRowContext(ctx, `SELECT id,source_url,cache_path,mime_type,byte_size,sha256,NULLIF(width,0),NULLIF(height,0),status FROM media_assets WHERE source_url=?`, norm))
	if errors.Is(err, sql.ErrNoRows) {
		id := sha256Hex(norm)
		return &mediaReference{ID: &id, Status: "pending", SourceURL: &norm}, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return s.mediaReference(ctx, a), nil
}

func (s *Store) mediaReference(ctx context.Context, a mediaAsset) *mediaReference {
	if a.ID == "" {
		return nil
	}
	r := &mediaReference{Status: a.Status, SourceURL: strPtr(a.SourceURL), MIMEType: a.MIMEType, ByteSize: a.ByteSize, Width: a.Width, Height: a.Height}
	if a.Status == "ready" && a.CachePath != nil && a.SHA256 != nil && safeHash(*a.SHA256) && s.cacheFileExists(ctx, *a.CachePath) {
		r.Status = "ready"
		r.ID = &a.ID
		v := "/media/" + a.ID + "?v=" + *a.SHA256
		r.PublicURL = &v
	} else if a.Status == "ready" {
		r.Status = "missing"
	}
	r.ID = &a.ID
	return r
}

func (s *Store) safeDataPath(relative string) bool {
	if filepath.IsAbs(relative) {
		return false
	}
	base, err := filepath.EvalSymlinks(s.dataRoot)
	if err != nil {
		return false
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(s.dataRoot, filepath.FromSlash(relative)))
	if err != nil {
		return false
	}
	base, _ = filepath.Abs(base)
	candidate, _ = filepath.Abs(candidate)
	return candidate == base || strings.HasPrefix(candidate, base+string(os.PathSeparator))
}

func (s *Store) publicAssetExists(v string) bool {
	rel, err := url.PathUnescape(strings.TrimPrefix(v, "/assets/"))
	if err != nil {
		return false
	}
	if rel == "" || strings.Contains(rel, "..") || strings.ContainsRune(rel, 0) {
		return false
	}
	root := filepath.Join(filepath.Dir(s.dataRoot), "public", "assets")
	candidate := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(candidate)
	return err == nil && info.Mode().IsRegular()
}
