package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func openSyncSettingsStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := testutil.NewSnapshot(t)
	s, err := OpenAdminWrites(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

// The first start imports usable environment values once; later starts and
// later environment changes leave stored settings alone.
func TestImportSyncSettingsStoresEnvironmentValuesOnce(t *testing.T) {
	s, _ := openSyncSettingsStore(t)
	ctx := context.Background()
	env := map[string]string{
		"X_MAX_STATUS_REQUESTS":     "20",
		"X_BOOTSTRAP_DAYS":          "90", // out of range and not clamped: skipped
		"SOURCE_HISTORY_MAX_PAGES":  "99", // clamped to 20, as the variable was read
		"TRANSLATION_LOOKBACK_DAYS": "not a number",
	}
	if err := s.ImportSyncSettings(ctx, func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
	got, err := s.SyncSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultSyncSettings()
	want.XMaxStatusRequests, want.FetchMaxPages = 20, 20
	if got != want {
		t.Fatalf("imported=%+v want %+v", got, want)
	}

	env["X_MAX_STATUS_REQUESTS"], env["X_REFRESH_KNOWN"] = "3", "4"
	if err = s.ImportSyncSettings(ctx, func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.SyncSettings(ctx); got.XMaxStatusRequests != 20 || got.XRefreshKnown != 4 {
		t.Fatalf("a stored value was replaced or a new one skipped: %+v", got)
	}
}

func TestSaveSyncSettingsChecksEveryBound(t *testing.T) {
	s, path := openSyncSettingsStore(t)
	ctx := context.Background()
	settings := DefaultSyncSettings()
	settings.FetchTimeoutMS = 999
	if err := s.SaveSyncSettings(ctx, settings); !errors.Is(err, ErrWorkflowValidation) {
		t.Fatalf("err=%v", err)
	}
	settings.FetchTimeoutMS, settings.TranslationScanLimit = 15000, 50
	if err := s.SaveSyncSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SyncSettings(ctx); got != settings {
		t.Fatalf("stored=%+v", got)
	}
	// An out-of-range stored value reads as its default.
	testutil.Exec(t, path, `UPDATE app_settings SET value='0' WHERE key='sync_translation_scan_limit'`)
	if got, _ := s.SyncSettings(ctx); got.TranslationScanLimit != 20 {
		t.Fatalf("scan limit=%d", got.TranslationScanLimit)
	}
}
