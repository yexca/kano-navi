package websub

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/secretstore"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// Operator settings in app_settings. The secret is sealed with
// LLM_SECRETS_KEY like provider keys and is never returned or logged.
const (
	settingEnabled       = "youtube_websub_enabled"
	settingCallback      = "youtube_websub_callback_url"
	settingSealedSigning = "youtube_websub_secret"
)

// Admin owns /api/admin/youtube-push: it persists the operator settings,
// generates the signing secret and reconfigures the running Service.
type Admin struct {
	store   *sqlite.Store
	db      *sql.DB
	key     string
	service *Service
	// available is false when SKIP_YOUTUBE disables the source.
	available bool
	now       func() time.Time
	// mu serializes operator writes so storage and the service agree.
	mu sync.Mutex
}

var _ httpx.AdminModule = (*Admin)(nil)

// NewAdmin binds the settings to service. key is LLM_SECRETS_KEY.
func NewAdmin(store *sqlite.Store, service *Service, key string, available bool) *Admin {
	return &Admin{store: store, db: store.DB(), key: key, service: service, available: available, now: time.Now}
}

// Load applies the stored settings at startup. A secret that no longer
// decrypts (a changed LLM_SECRETS_KEY) is replaced, which only means the next
// subscription uses a new secret.
func (a *Admin) Load(ctx context.Context) error {
	enabled, callback, secret, err := a.read(ctx)
	if err != nil {
		return err
	}
	if enabled && a.available && secret == "" && a.store.Writable() {
		if secret, err = newSecret(); err != nil {
			return err
		}
		if err = a.save(ctx, enabled, callback, secret); err != nil {
			return err
		}
	}
	if a.service.Configure(enabled && a.available, callback, secret) != nil {
		// Stored settings that no longer validate stay saved but inactive.
		return a.service.Configure(false, callback, secret)
	}
	return nil
}

func (a *Admin) read(ctx context.Context) (enabled bool, callback, secret string, err error) {
	rows, err := a.db.QueryContext(ctx, `SELECT key, value FROM app_settings WHERE key IN (?, ?, ?)`, settingEnabled, settingCallback, settingSealedSigning)
	if err != nil {
		return false, "", "", err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return false, "", "", err
		}
		switch key {
		case settingEnabled:
			enabled = value == "1"
		case settingCallback:
			callback = value
		case settingSealedSigning:
			if plain, decryptErr := secretstore.Decrypt(value, a.key); decryptErr == nil {
				secret = plain
			}
		}
	}
	return enabled, callback, secret, rows.Err()
}

func (a *Admin) save(ctx context.Context, enabled bool, callback, secret string) error {
	sealed, err := secretstore.Encrypt(secret, a.key)
	if err != nil {
		return err
	}
	flag := "0"
	if enabled {
		flag = "1"
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := sqlite.ISOTime(a.now())
	for key, value := range map[string]string{settingEnabled: flag, settingCallback: callback, settingSealedSigning: sealed} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AdminRoutes lists the push settings endpoints. The same status is also part
// of the source overview (Status).
func (a *Admin) AdminRoutes() []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Path: "/sources/youtube/push", Handler: func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteJSON(w, http.StatusOK, a.payload())
		}},
		{Method: http.MethodPut, Path: "/sources/youtube/push", Handler: a.locked(a.update)},
		{Method: http.MethodPost, Path: "/sources/youtube/push/rotate-secret", Handler: a.locked(a.rotate)},
	}
}

// locked serializes settings writes against a writable store.
func (a *Admin) locked(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.store.Writable() {
			httpx.ReadOnly(w)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		next(w, r)
	}
}

// payload is the service status plus whether the source can be enabled.
func (a *Admin) payload() map[string]any {
	status := a.service.Status()
	status["available"] = a.available
	return status
}

// Status is the projection used by the workflow console.
func (a *Admin) Status() map[string]any { return a.payload() }

func (a *Admin) update(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	enabled, callback, secret := a.service.settings()
	if raw, ok := body["enabled"]; ok {
		if json.Unmarshal(raw, &enabled) != nil {
			httpx.WriteError(w, httpx.Invalid("enabled must be a boolean"))
			return
		}
	}
	if raw, ok := body["callbackUrl"]; ok {
		var value *string
		if json.Unmarshal(raw, &value) != nil {
			httpx.WriteError(w, httpx.Invalid("callbackUrl must be a string"))
			return
		}
		callback = ""
		if value != nil {
			callback = strings.TrimSpace(*value)
		}
	}
	switch {
	case callback != "" && !ValidCallback(callback):
		httpx.WriteError(w, httpx.Invalid("callbackUrl must be an absolute HTTPS URL without credentials or a fragment"))
		return
	case enabled && callback == "":
		httpx.WriteError(w, httpx.Invalid("callbackUrl is required to enable YouTube push"))
		return
	case enabled && !a.available:
		httpx.WriteError(w, httpx.Invalid("YouTube synchronization is disabled by SKIP_YOUTUBE"))
		return
	}
	if secret == "" {
		if secret, err = newSecret(); err != nil {
			httpx.WriteCode(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	a.apply(w, r.Context(), enabled, callback, secret)
}

// rotate replaces the secret and resubscribes when enabled. Notifications
// signed with the old secret are ignored until the hub confirms the new one;
// polling covers that gap.
func (a *Admin) rotate(w http.ResponseWriter, r *http.Request) {
	enabled, callback, _ := a.service.settings()
	secret, err := newSecret()
	if err != nil {
		httpx.WriteCode(w, http.StatusInternalServerError, "internal_error")
		return
	}
	a.apply(w, r.Context(), enabled, callback, secret)
}

func (a *Admin) apply(w http.ResponseWriter, ctx context.Context, enabled bool, callback, secret string) {
	if err := a.save(ctx, enabled, callback, secret); err != nil {
		if errors.Is(err, secretstore.ErrKeyRequired) {
			httpx.WriteCode(w, http.StatusServiceUnavailable, "secrets_key_unavailable")
			return
		}
		httpx.WriteCode(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if a.service.Configure(enabled, callback, secret) != nil {
		httpx.WriteError(w, httpx.Invalid("YouTube push settings are invalid"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.payload())
}
