package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type sourceLeaseKey struct{}

func WithSourceLease(ctx context.Context, lease WorkflowLease) context.Context {
	return context.WithValue(ctx, sourceLeaseKey{}, lease)
}
func fenceSourceWrite(ctx context.Context, tx *sql.Tx) error {
	if l, ok := ctx.Value(sourceLeaseKey{}).(WorkflowLease); ok {
		return fenceWorkflow(ctx, tx, l, time.Now())
	}
	return nil
}
func bumpRevision(ctx context.Context, tx *sql.Tx, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES('dashboard_revision','1',?) ON CONFLICT(key) DO UPDATE SET value=CAST(MAX(0,CAST(app_settings.value AS INTEGER))+1 AS TEXT),updated_at=excluded.updated_at`, now)
	return err
}
func mediaIdentity(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", "", ErrSourceWrite
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	normalized := u.String()
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:]), normalized, nil
}
func replaceMediaLink(ctx context.Context, tx *sql.Tx, source, raw, ownerType, ownerID, role string, pos int, now string) error {
	id, u, err := mediaIdentity(raw)
	if err != nil {
		return err
	}
	var existing string
	lookupErr := tx.QueryRowContext(ctx, `SELECT id FROM media_assets WHERE source_url=?`, u).Scan(&existing)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return ErrSourceWrite
	}
	if existing != "" && existing != id {
		if !regexp.MustCompile(`^x-[a-f0-9]{32}$`).MatchString(existing) {
			return ErrSourceWrite
		}
		if err = repairMediaIdentity(ctx, tx, existing, id); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO media_assets(id,source,source_url,status,created_at,updated_at,last_seen_at) VALUES(?,?,?,'pending',?,?,?) ON CONFLICT(id) DO UPDATE SET last_seen_at=excluded.last_seen_at,updated_at=excluded.updated_at`, id, source, u, now, now, now)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM media_links WHERE owner_type=? AND owner_id=? AND role=? AND position=?`, ownerType, ownerID, role, pos); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO media_links(media_id,owner_type,owner_id,role,position,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, id, ownerType, ownerID, role, pos, now, now)
	return err
}

func repairMediaIdentity(ctx context.Context, tx *sql.Tx, oldID, newID string) error {
	// Defer FK checks until both the primary key and its references agree.
	// No row or cached file is deleted or recreated.
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		return ErrSourceWrite
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_assets SET id=? WHERE id=?`, newID, oldID); err != nil {
		return ErrSourceWrite
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_links SET media_id=? WHERE media_id=?`, newID, oldID); err != nil {
		return ErrSourceWrite
	}
	return nil
}

func (s *Store) RepairLegacyXMedia(ctx context.Context) error {
	tx, err := s.workflowTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,source_url FROM media_assets WHERE source='x' AND id LIKE 'x-%' AND length(id)=34`)
	if err != nil {
		return ErrSourceWrite
	}
	assets := []PendingMedia{}
	for rows.Next() {
		var a PendingMedia
		if rows.Scan(&a.ID, &a.SourceURL) != nil {
			rows.Close()
			return ErrSourceWrite
		}
		assets = append(assets, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrSourceWrite
	}
	changed := false
	for _, a := range assets {
		if !regexp.MustCompile(`^x-[a-f0-9]{32}$`).MatchString(a.ID) {
			continue
		}
		id, _, err := mediaIdentity(a.SourceURL)
		if err != nil {
			continue
		}
		if err = repairMediaIdentity(ctx, tx, a.ID, id); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		if err = bumpRevision(ctx, tx, workflowTime(time.Now())); err != nil {
			return ErrSourceWrite
		}
	}
	if tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}
