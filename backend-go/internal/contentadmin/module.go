// Package contentadmin owns the operator routes for profile media, schedule
// image reviews and post reprocess requests. Wire shapes follow the original
// admin API so the existing console works unchanged.
package contentadmin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
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

// opError is an expected operation failure. TypeScript surfaces these as a
// bare 500 internal_error; the fixed message is only stored as last_error.
type opError struct{ message string }

func (e *opError) Error() string { return e.message }

func failure(message string) error { return &opError{message: message} }

func (m *Module) timestamp() string { return sqlite.ISOTime(m.now()) }

// ServeAdmin dispatches the routes owned by this module. The router has
// already authenticated the session and applied the origin guard.
func (m *Module) ServeAdmin(w http.ResponseWriter, r *http.Request, path string) bool {
	segments, ok := routeSegments(r, path)
	if !ok || len(segments) == 0 {
		return false
	}
	if m == nil || m.db == nil {
		return false
	}
	switch segments[0] {
	case "profile-media":
		return m.serveProfileMedia(w, r, segments[1:])
	case "schedule-assets":
		return m.serveScheduleAssets(w, r, segments[1:])
	case "posts":
		if len(segments) == 4 && segments[2] == "llm" && segments[3] == "reprocess" {
			if m.mutation(w, r) {
				m.reprocessPost(w, r, segments[1])
			}
			return true
		}
	}
	return false
}

func (m *Module) serveProfileMedia(w http.ResponseWriter, r *http.Request, rest []string) bool {
	switch {
	case len(rest) == 0:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			m.listProfileMediaRoute(w, r)
		case http.MethodPost:
			if m.writable(w) {
				m.addProfileMedia(w, r)
			}
		default:
			httpx.MethodNotAllowed(w, "GET, POST")
		}
		return true
	case len(rest) == 1 && rest[0] == "discover":
		if m.mutation(w, r) {
			m.discoverProfileMediaRoute(w, r)
		}
		return true
	case len(rest) == 2 && rest[0] == "upload":
		// Registered before the :id routes, exactly like the Express table.
		if m.mutation(w, r) {
			m.uploadProfileMedia(w, r, rest[1])
		}
		return true
	case len(rest) == 2 && rest[1] == "preview":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpx.MethodNotAllowed(w, "GET")
			return true
		}
		m.previewProfileMedia(w, r, rest[0])
		return true
	case len(rest) == 2 && rest[1] == "download":
		if m.mutation(w, r) {
			m.downloadProfileMediaRoute(w, r, rest[0])
		}
		return true
	case len(rest) == 2 && rest[1] == "select":
		if m.mutation(w, r) {
			m.selectProfileMediaRoute(w, r, rest[0])
		}
		return true
	}
	return false
}

func (m *Module) serveScheduleAssets(w http.ResponseWriter, r *http.Request, rest []string) bool {
	switch {
	case len(rest) == 0:
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpx.MethodNotAllowed(w, "GET")
			return true
		}
		m.listScheduleAssetsRoute(w, r)
		return true
	case len(rest) == 2 && rest[1] == "manual-review":
		if m.mutation(w, r) {
			m.manualReviewRoute(w, r, rest[0])
		}
		return true
	case len(rest) == 2 && rest[1] == "period":
		if m.mutation(w, r) {
			m.periodRoute(w, r, rest[0])
		}
		return true
	}
	return false
}

// mutation accepts only POST and only when operator writes are enabled.
func (m *Module) mutation(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w, "POST")
		return false
	}
	return m.writable(w)
}

func (m *Module) writable(w http.ResponseWriter) bool {
	if !m.store.Writable() {
		httpx.WriteCode(w, http.StatusNotImplemented, "capability_not_migrated")
		return false
	}
	return true
}

// routeSegments splits the module path into decoded segments. The escaped
// request path is preferred so an encoded slash stays inside its parameter,
// which is how Express decodes route parameters.
func routeSegments(r *http.Request, path string) ([]string, bool) {
	const prefix = "/api/admin"
	raw, escaped := path, false
	if r != nil && r.URL != nil {
		if full := r.URL.EscapedPath(); strings.HasPrefix(full, prefix+"/") {
			raw, escaped = strings.TrimPrefix(full, prefix), true
		}
	}
	raw = strings.TrimSuffix(strings.TrimPrefix(raw, "/"), "/")
	if raw == "" {
		return nil, false
	}
	segments := strings.Split(raw, "/")
	for i, segment := range segments {
		if escaped {
			decoded, err := url.PathUnescape(segment)
			if err != nil {
				return nil, false
			}
			segment = decoded
			segments[i] = decoded
		}
		if segment == "" {
			return nil, false
		}
	}
	return segments, true
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
