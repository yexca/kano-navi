package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
)

func TestSourceChainsDefaultToTheFormerOrderAndRoundTrip(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	// An upgrade reads the order the recent steps always used.
	x := scheduleOf(t, s, "x", 180)
	if got := x.Chain.Enabled(); len(got) != 2 || got[0] != "timeline" || got[1] != "profile" || x.Chain.APIGate != DefaultXAPIGate || x.Chain.APIDailyCap != DefaultXAPIDailyCap {
		t.Fatalf("x chain=%+v", x.Chain)
	}
	if got := scheduleOf(t, s, "youtube", 180).Chain.Enabled(); len(got) != 3 || got[0] != "api" || got[2] != "videos_page" {
		t.Fatalf("youtube chain=%v", got)
	}
	if b := scheduleOf(t, s, "bilibili", 180).Chain; len(b.Methods) != 1 || b.Methods[0].ID != "list" {
		t.Fatalf("bilibili chain=%+v", b)
	}
	x.Chain = SourceChain{Methods: []ChainMethod{{"api", true}, {"timeline", true}, {"profile", false}}, APIGate: 0, APIDailyCap: 40}
	if _, err := s.SaveSourceSchedule(ctx, x, 180, now); err != nil {
		t.Fatal(err)
	}
	if got := scheduleOf(t, s, "x", 180).Chain; got.Problem("x") != "" || got.Methods[0].ID != "api" || got.Methods[2].Enabled || got.APIDailyCap != 40 || got.APIGate != 0 {
		t.Fatalf("saved=%+v", got)
	}
	for name, chain := range map[string]SourceChain{
		"unknown":      {Methods: []ChainMethod{{"timeline", true}, {"profile", true}, {"search", true}}, APIGate: 3, APIDailyCap: 1},
		"duplicate":    {Methods: []ChainMethod{{"timeline", true}, {"timeline", true}, {"api", true}}, APIGate: 3, APIDailyCap: 1},
		"missing":      {Methods: []ChainMethod{{"timeline", true}, {"api", true}}, APIGate: 3, APIDailyCap: 1},
		"all disabled": {Methods: []ChainMethod{{"timeline", false}, {"profile", false}, {"api", false}}, APIGate: 3, APIDailyCap: 1},
		"no cap":       {Methods: []ChainMethod{{"timeline", true}, {"profile", true}, {"api", true}}, APIGate: 3},
	} {
		x.Chain = chain
		if _, err := s.SaveSourceSchedule(ctx, x, 180, now); !errors.Is(err, ErrWorkflowValidation) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	// A stored chain that no longer validates reads as the default.
	if _, err := s.DB().Exec(`UPDATE source_schedules SET chain_json='{"methods":[{"id":"gone","enabled":true}]}' WHERE source='youtube'`); err != nil {
		t.Fatal(err)
	}
	if got := scheduleOf(t, s, "youtube", 180).Chain; got.Problem("youtube") != "" || got.Methods[0].ID != "api" {
		t.Fatalf("unreadable chain=%+v", got)
	}
}

func TestRunsFreezeTheirChains(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	frozen := SourceChain{Methods: []ChainMethod{{"rss", true}, {"api", false}, {"videos_page", true}}}
	run, err := s.CreateWorkflowRun(ctx, WorkflowDefinition{ID: SourceScheduleWorkflow("youtube"), Steps: []string{"youtube"}, SourceChain: map[string]SourceChain{"youtube": frozen}}, "scheduler", "", map[string]bool{"youtube": true}, now)
	if err != nil {
		t.Fatal(err)
	}
	// A later edit of the schedule does not reach the queued run.
	youtube := scheduleOf(t, s, "youtube", 180)
	youtube.Chain = DefaultSourceChain("youtube")
	if _, err = s.SaveSourceSchedule(ctx, youtube, 180, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWorkflowRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if chain := got.SourceChain["youtube"]; len(chain.Methods) != 3 || chain.Methods[0].ID != "rss" || chain.Uses("api") {
		t.Fatalf("frozen=%+v", got.SourceChain)
	}
}

func TestXAPIUsageCountsPerJapanDayAndStreaksPerAccount(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	// 15:30 UTC is already the next day in Japan.
	late := time.Date(2026, 3, 2, 15, 30, 0, 0, time.UTC)
	if JapanDay(late) != "2026-03-03" || JapanDay(late.Add(-time.Hour)) != "2026-03-02" {
		t.Fatalf("days=%s %s", JapanDay(late), JapanDay(late.Add(-time.Hour)))
	}
	for _, n := range []int{3, 0, 4} {
		if err := s.AddXAPIUsage(ctx, "2026-03-02", n); err != nil {
			t.Fatal(err)
		}
	}
	if usage, err := s.XAPIUsageOn(ctx, "2026-03-02"); err != nil || usage.Posts != 7 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	// A new day starts over.
	if err := s.AddXAPIUsage(ctx, "2026-03-03", 2); err != nil {
		t.Fatal(err)
	}
	if usage, _ := s.XAPIUsageOn(ctx, "2026-03-02"); usage.Posts != 0 {
		t.Fatalf("old day=%+v", usage)
	}
	if err := s.SetXTimelineFailures(ctx, "kano", 2); err != nil {
		t.Fatal(err)
	}
	state, err := s.XAPIStatus(ctx, []string{"kano", "other"}, late)
	if err != nil || state.Usage.Posts != 2 || state.TimelineFailures["kano"] != 2 || state.TimelineFailures["other"] != 0 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestGapCeilingsAreTheOldestStoredStatusAboveEachFloor(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	posts := []xsource.Post{}
	for _, id := range []string{"999999999999999999", "1900000000000000020", "1900000000000000030", "1900000000000000005"} {
		posts = append(posts, xsource.Post{ID: id, AccountHandle: "kano", Type: "daily", Text: "t", PublishedAt: "2026-03-01T00:00:00Z", URL: "https://x.com/kano/status/" + id, Raw: map[string]any{}})
	}
	gap := &xsource.Gap{Floor: "1900000000000000010", Older: &xsource.Gap{Floor: "999999999999999998"}}
	if err := s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", Posts: posts, Gap: gap}, true); err != nil {
		t.Fatal(err)
	}
	state, err := s.XRecentState(ctx, "kano")
	if err != nil {
		t.Fatal(err)
	}
	if state.GapCeilings["1900000000000000010"] != "1900000000000000020" || state.GapCeilings["999999999999999998"] != "999999999999999999" {
		t.Fatalf("ceilings=%v", state.GapCeilings)
	}
}
