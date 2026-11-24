package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) queryFocus(ctx context.Context) (*Focus, error) {
	var f Focus
	var vid, dl, desc, img, u, su, up sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,video_id,date_label,title,description,image_url,url,source_url,updated_at FROM focus ORDER BY id LIMIT 1`).Scan(&f.ID, &vid, &dl, &f.Title, &desc, &img, &u, &su, &up)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if vid.Valid {
		f.VideoID = &vid.String
	}
	if dl.Valid {
		f.DateLabel = &dl.String
	}
	if desc.Valid {
		f.Description = &desc.String
	}
	if img.Valid {
		f.ImageURL = &img.String
	}
	if u.Valid {
		f.URL = &u.String
	}
	if su.Valid {
		f.SourceURL = &su.String
	}
	if up.Valid {
		f.UpdatedAt = &up.String
	}
	return &f, nil
}

func (s *Store) queryTimeline(ctx context.Context) ([]TimelineItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,year,title,detail,sort_order FROM timeline ORDER BY sort_order ASC,id ASC`)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []TimelineItem{}
	for rows.Next() {
		var x TimelineItem
		var d sql.NullString
		if err := rows.Scan(&x.ID, &x.Year, &x.Title, &d, &x.SortOrder); err != nil {
			return nil, dbErr(err)
		}
		if d.Valid {
			x.Detail = &d.String
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) queryResources(ctx context.Context) ([]Resource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,detail,icon,tone,url,sort_order FROM resources ORDER BY sort_order ASC,id ASC`)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []Resource{}
	for rows.Next() {
		var x Resource
		var d, i, t sql.NullString
		if err := rows.Scan(&x.ID, &x.Title, &d, &i, &t, &x.URL, &x.SortOrder); err != nil {
			return nil, dbErr(err)
		}
		if d.Valid {
			x.Detail = &d.String
		}
		if i.Valid {
			x.Icon = &i.String
		}
		if t.Valid {
			x.Tone = &t.String
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) queryProfile(ctx context.Context) (*Profile, error) {
	var p Profile
	var rn, bio, av, bn, x, y, up sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,display_name,romanized_name,bio,avatar_url,banner_url,x_url,youtube_url,updated_at FROM profiles ORDER BY id LIMIT 1`).Scan(&p.ID, &p.DisplayName, &rn, &bio, &av, &bn, &x, &y, &up)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if rn.Valid {
		p.RomanizedName = &rn.String
	}
	if bio.Valid {
		p.Bio = &bio.String
	}
	if av.Valid {
		p.AvatarURL = &av.String
	}
	if bn.Valid {
		p.BannerURL = &bn.String
	}
	if x.Valid {
		p.XURL = &x.String
	}
	if y.Valid {
		p.YouTubeURL = &y.String
	}
	if up.Valid {
		p.UpdatedAt = &up.String
	}
	return &p, nil
}

func (s *Store) queryProfileMedia(ctx context.Context) (ProfileMediaSlots, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,slot,source,source_url,active_cache_path,active_sha256,active_mime_type,status,is_active FROM profile_media WHERE is_active=1 AND slot IN ('avatar','banner') ORDER BY slot,id`)
	if err != nil {
		return ProfileMediaSlots{}, dbErr(err)
	}
	defer rows.Close()
	var out ProfileMediaSlots
	for rows.Next() {
		var id, slot, source string
		var su, cp, sha, mime, status sql.NullString
		var active int64
		if err := rows.Scan(&id, &slot, &source, &su, &cp, &sha, &mime, &status, &active); err != nil {
			return out, dbErr(err)
		}
		pm := s.profileMedia(ctx, id, slot, source, su, cp, sha, mime, status.String, active != 0)
		switch slot {
		case "avatar":
			out.Avatar = pm
		case "banner":
			out.Banner = pm
		}
	}
	return out, rows.Err()
}

func (s *Store) profileMedia(ctx context.Context, id, slot, source string, su, cp, sha, mime sql.NullString, status string, active bool) *ProfileMedia {
	var sourceURL *string
	if su.Valid {
		sourceURL = &su.String
	}
	pm := &ProfileMedia{ID: id, Slot: slot, Source: source, SourceURL: sourceURL, Status: status, IsActive: active}
	if active && cp.Valid && safeHash(sha.String) && s.cacheFileExists(ctx, cp.String) && sha.Valid {
		v := "/media/profile/" + slot + "?v=" + sha.String
		pm.PublicURL = &v
		pm.Status = "ready"
	}
	return pm
}

func (s *Store) getFeaturedVideoID(ctx context.Context) (*string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key='featured_video_id'`).Scan(&v)
	// An absent setting falls back to the focus row's video. An explicit
	// empty (or stale) setting is an operator decision and must not fall back.
	if errors.Is(err, sql.ErrNoRows) {
		var legacy sql.NullString
		err = s.db.QueryRowContext(ctx, `SELECT video_id FROM focus ORDER BY id LIMIT 1`).Scan(&legacy)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		v = legacy.String
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if v == "" {
		return nil, nil
	}
	exists, err := s.getVideo(ctx, v, time.Time{})
	if err != nil {
		return nil, err
	}
	if exists == nil {
		return nil, nil
	}
	return &v, nil
}

func (s *Store) getFetchedAt(ctx context.Context) (*string, error) {
	var v sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT finished_at FROM sync_runs WHERE status IN ('success','partial') AND finished_at IS NOT NULL ORDER BY finished_at DESC,id DESC LIMIT 1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) || !v.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return &v.String, nil
}

func (s *Store) getMediaCache(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status,COUNT(*) FROM media_assets GROUP BY status`)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := map[string]int64{"total": 0}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, dbErr(err)
		}
		out[status] = n
		out["total"] += n
	}
	return out, rows.Err()
}

// bilibiliAccountsSQL walks posts_bilibili_account_idx, which holds the
// Bilibili posts alone, so the X posts are never read.
const bilibiliAccountsSQL = `SELECT DISTINCT account_handle FROM posts WHERE source='bilibili' AND hidden_at IS NULL AND account_handle IS NOT NULL ORDER BY account_handle LIMIT 5`

// getBilibiliAccounts lists the accounts of the stored, shown Bilibili posts.
func (s *Store) getBilibiliAccounts(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, bilibiliAccountsSQL)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var account string
		if err = rows.Scan(&account); err != nil {
			return nil, dbErr(err)
		}
		out = append(out, account)
	}
	return out, dbErr(rows.Err())
}

func (s *Store) getXAccounts(ctx context.Context) ([]string, error) {
	accounts, err := s.accountListSetting(ctx, "x_accounts")
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return []string{"kano_2525", "_Kanotic"}, nil
	}
	return accounts, nil
}

// XArchiveAccountsSetting names the X accounts whose posts come only from an
// imported account export: they appear in the post archive's account filter
// but are never synchronized and get no place on the board's wall filter.
const XArchiveAccountsSetting = "x_archive_accounts"

// XArchiveAccounts lists the archive-only X accounts, in stored order.
func (s *Store) XArchiveAccounts(ctx context.Context) ([]string, error) {
	return s.accountListSetting(ctx, XArchiveAccountsSetting)
}

// PostArchiveAccounts are the accounts the post archive filters by: the
// board's X accounts followed by the archive-only ones, and the Bilibili
// accounts of shown posts.
type PostArchiveAccounts struct {
	X        []string
	Bilibili []string
}

// GetPostArchiveAccounts lists the post archive's filter accounts.
func (s *Store) GetPostArchiveAccounts(ctx context.Context) (PostArchiveAccounts, error) {
	if s != nil && s.readDB != nil {
		return s.publicReader().GetPostArchiveAccounts(ctx)
	}
	if s == nil || s.db == nil {
		return PostArchiveAccounts{}, ErrUnavailable
	}
	x, err := s.getXAccounts(ctx)
	if err != nil {
		return PostArchiveAccounts{}, err
	}
	archived, err := s.XArchiveAccounts(ctx)
	if err != nil {
		return PostArchiveAccounts{}, err
	}
	seen := map[string]bool{}
	for _, account := range x {
		seen[strings.ToLower(account)] = true
	}
	for _, account := range archived {
		if !seen[strings.ToLower(account)] {
			seen[strings.ToLower(account)] = true
			x = append(x, account)
		}
	}
	bilibili, err := s.getBilibiliAccounts(ctx)
	if err != nil {
		return PostArchiveAccounts{}, err
	}
	return PostArchiveAccounts{X: x, Bilibili: bilibili}, nil
}

// accountListSetting reads an app setting holding a JSON array (or a comma or
// line separated list) of handles, without "@", duplicates or blanks. A
// missing setting is an empty list.
func (s *Store) accountListSetting(ctx context.Context, key string) ([]string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return parseAccountList(v), nil
}

func parseAccountList(v string) []string {
	v = strings.Trim(strings.TrimSpace(v), "[]\"")
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '"' })
	out := []string{}
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimPrefix(strings.TrimSpace(p), "@")
		if p != "" && !seen[strings.ToLower(p)] {
			seen[strings.ToLower(p)] = true
			out = append(out, p)
		}
	}
	return out
}
