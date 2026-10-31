// Package contentadmin owns the operator routes for profile media, schedule
// image reviews, post reprocess requests and post quote decisions; see
// docs/architecture/admin-api.md.
package contentadmin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// Options configures a Module. Every field is optional.
type Options struct {
	// HTTPClient performs profile discovery and candidate downloads. Its
	// transport is reused; the timeout and manual redirect policy are always
	// replaced so each hop is revalidated against the host allowlists.
	HTTPClient *http.Client
	// Now supplies stored timestamps. It defaults to time.Now.
	Now func() time.Time
	// PublicAssetsDir is the tracked /assets directory used to resolve fixed
	// schedule images. It defaults to ../public/assets next to the data root.
	PublicAssetsDir string
}

// Module implements httpx.AdminModule.
type Module struct {
	store     *sqlite.Store
	db        *sql.DB
	dataRoot  string
	assetsDir string
	client    *http.Client
	now       func() time.Time
	// selectMu serializes selected-file replacement with its metadata update.
	selectMu sync.Mutex
}

var _ httpx.AdminModule = (*Module)(nil)

// New builds the content admin module over an opened store.
func New(store *sqlite.Store, opts Options) *Module {
	m := &Module{store: store, db: store.DB(), dataRoot: store.DataRoot(), now: opts.Now, assetsDir: opts.PublicAssetsDir}
	if m.now == nil {
		m.now = time.Now
	}
	if m.assetsDir == "" && m.dataRoot != "" {
		m.assetsDir = filepath.Join(filepath.Dir(m.dataRoot), "public", "assets")
	}
	client := http.Client{}
	if opts.HTTPClient != nil {
		client = *opts.HTTPClient
	}
	client.Timeout = fetchTimeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	m.client = &client
	return m
}

// opError is an expected operation failure. It surfaces as a bare 500
// internal_error; the fixed message is only stored as last_error.
type opError struct{ message string }

func (e *opError) Error() string { return e.message }

func failure(message string) error { return &opError{message: message} }

func (m *Module) timestamp() string { return sqlite.ISOTime(m.now()) }

// AdminRoutes lists the profile media, schedule image, post reprocess and
// post quote endpoints. The router has already authenticated the session and applied the
// origin guard.
func (m *Module) AdminRoutes() []httpx.Route {
	id := func(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { next(w, r, r.PathValue("id")) }
	}
	return []httpx.Route{
		{Method: http.MethodGet, Path: "/profile-media", Handler: m.guard(m.listProfileMediaRoute)},
		{Method: http.MethodPost, Path: "/profile-media", Handler: m.guard(m.addProfileMedia)},
		{Method: http.MethodPost, Path: "/profile-media/discover", Handler: m.guard(m.discoverProfileMediaRoute)},
		{Method: http.MethodPost, Path: "/profile-media/upload", Handler: m.guard(func(w http.ResponseWriter, r *http.Request) {
			m.uploadProfileMedia(w, r, r.URL.Query().Get("slot"))
		})},
		{Method: http.MethodGet, Path: "/profile-media/{id}/preview", Handler: m.guard(id(m.previewProfileMedia))},
		{Method: http.MethodPost, Path: "/profile-media/{id}/download", Handler: m.guard(id(m.downloadProfileMediaRoute))},
		{Method: http.MethodPost, Path: "/profile-media/{id}/select", Handler: m.guard(id(m.selectProfileMediaRoute))},
		{Method: http.MethodGet, Path: "/schedule-images", Handler: m.guard(m.listScheduleAssetsRoute)},
		{Method: http.MethodPost, Path: "/schedule-images/{id}/review", Handler: m.guard(id(m.manualReviewRoute))},
		{Method: http.MethodPut, Path: "/schedule-images/{id}/period", Handler: m.guard(id(m.periodRoute))},
		{Method: http.MethodPost, Path: "/posts/{id}/reprocess", Handler: m.guard(id(m.reprocessPost))},
		{Method: http.MethodPut, Path: "/posts/{id}/quote/hidden", Handler: m.guard(id(m.quoteHiddenRoute))},
		{Method: http.MethodPost, Path: "/posts/{id}/refresh", Handler: m.guard(id(m.refreshPostRoute))},
	}
}

// guard rejects every route without a database and every write against a
// read-only one.
func (m *Module) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case m == nil || m.db == nil:
			httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		case r.Method != http.MethodGet && r.Method != http.MethodHead && !m.store.Writable():
			httpx.ReadOnly(w)
		default:
			next(w, r)
		}
	}
}

// writeFailure reports validation failures with their message and everything
// else with the fixed internal code; raw causes are never serialized.
func writeFailure(w http.ResponseWriter, err error) {
	var op *opError
	if errors.As(err, &op) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		httpx.WriteCode(w, http.StatusInternalServerError, "internal_error")
		return
	}
	httpx.WriteError(w, err)
}

// queryer is satisfied by both *sql.DB and *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// inTx runs fn in one write transaction. It must never wrap a network request
// or a large file write: the pool has a single connection.
func (m *Module) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
