package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DB exposes the store's single SQLite connection to sibling feature packages
// (LLM configuration, profile media, schedule extraction, MCP). The pool holds
// exactly one connection: close every *sql.Rows before issuing another
// statement, and never keep a transaction open across a network request.
func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// Writable reports whether operator mutations are enabled for this process.
func (s *Store) Writable() bool { return s != nil && s.writable }

// DataRoot is the runtime data directory that contains database/, x/,
// youtube/ and avatar/.
func (s *Store) DataRoot() string {
	if s == nil {
		return ""
	}
	return s.dataRoot
}

// ISOTime formats an instant the way JavaScript's Date#toISOString does, which
// is the timestamp format stored throughout the snapshot.
func ISOTime(t time.Time) string { return workflowTime(t) }

// BumpRevisionTx advances the dashboard revision inside a caller transaction.
func BumpRevisionTx(ctx context.Context, tx *sql.Tx, now string) error {
	return bumpRevision(ctx, tx, now)
}

// BumpDashboardRevision tells polling browsers that the snapshot changed.
func (s *Store) BumpDashboardRevision(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	if !s.writable && !s.workflowWritable {
		return ErrReadOnly
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: transaction", ErrUnavailable)
	}
	defer tx.Rollback()
	if err = bumpRevision(ctx, tx, ISOTime(time.Now())); err != nil {
		return fmt.Errorf("%w: revision", ErrUnavailable)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("%w: revision", ErrUnavailable)
	}
	return nil
}
