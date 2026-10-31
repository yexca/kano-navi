package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/source/history"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

// A source step reads with the sync settings stored when it starts.
func TestSourceStepAppliesStoredSyncSettings(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for key, value := range map[string]string{
		"sync_x_bootstrap_days": "3", "sync_x_max_status_requests": "0", "sync_x_refresh_known": "2",
		"sync_youtube_max_detail_requests": "5", "sync_fetch_max_pages": "9",
		"sync_fetch_timeout_ms": "15000", "sync_fetch_request_delay_ms": "250",
	} {
		testutil.Exec(t, p, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,'2026-10-31T00:00:00Z')`, key, value)
	}
	fetcher := history.NewFetcher(s, history.DefaultOptions(), nil)
	steps := &SourceSteps{
		Store:   s,
		X:       &XRecentExecutor{Store: s, Options: xsource.OptionsFromEnv()},
		Youtube: ytsource.NewAdapter(nil, ytsource.OptionsFromEnv()),
		History: &HistorySource{Fetcher: fetcher, Store: s, RequestLimit: 12},
	}
	if err = steps.applySettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	x := steps.X.Options
	if x.BootstrapDays != 3 || x.MaxDetailRequests != 0 || x.RefreshKnown != 2 || x.DiscoveryLimit != 50 || len(x.Handles) == 0 {
		t.Fatalf("x=%+v", x)
	}
	if steps.Youtube.Options.MaxDetailRequests != 5 {
		t.Fatalf("youtube detail requests=%d", steps.Youtube.Options.MaxDetailRequests)
	}
	// The manual X page budget keeps at least one page.
	if steps.History.RequestLimit != 1 || fetcher.Options.MaxPages != 9 || fetcher.Options.RequestDelay != 250*time.Millisecond ||
		fetcher.Options.Timeout != 15*time.Second || fetcher.HTTP.Timeout != 15*time.Second {
		t.Fatalf("history=%d fetcher=%+v timeout=%s", steps.History.RequestLimit, fetcher.Options, fetcher.HTTP.Timeout)
	}
}
