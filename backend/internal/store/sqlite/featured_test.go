package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func TestFeaturedVideoSettingAndLegacyFocus(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured *string
		focus      *string
		want       *string
	}{
		{"legacy focus", nil, strPtr("legacy"), strPtr("legacy")},
		{"explicit empty", strPtr(""), strPtr("legacy"), nil},
		{"stale setting", strPtr("missing"), strPtr("legacy"), nil},
		{"configured priority", strPtr("selected"), strPtr("legacy"), strPtr("selected")},
		{"stale focus", nil, strPtr("missing"), nil},
		{"null focus", nil, nil, nil},
		{"no focus", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := testutil.NewSnapshot(t)
			for _, id := range []string{"legacy", "selected"} {
				testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,url,kind) VALUES (?,'youtube',?,'2020-01-01T00:00:00.000Z',?,'video')`, id, id, "https://example.invalid/"+id)
			}
			// Put Featured outside the usual recent-30/latest-video shortlist.
			for i := range 31 {
				id := fmt.Sprintf("recent-%02d", i)
				testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,url,kind) VALUES (?,'youtube',?,'2026-10-10T00:00:00.000Z',?,'video')`, id, id, "https://example.invalid/"+id)
			}
			if tc.name != "no focus" {
				testutil.Exec(t, path, `INSERT INTO focus (id,video_id,title) VALUES (1,?,'Focus')`, tc.focus)
			}
			if tc.configured != nil {
				testutil.Exec(t, path, `INSERT INTO app_settings (key,value,updated_at) VALUES ('featured_video_id',?,'2026-10-11T00:00:00.000Z')`, *tc.configured)
			}
			store, err := OpenReadOnly(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			dashboard, err := store.GetDashboard(context.Background(), 3, time.Date(2026, 10, 11, 3, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			got := dashboard.Meta.FeaturedVideoID
			if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
				t.Fatalf("featured = %v, want %v", got, tc.want)
			}
			for _, id := range []string{"legacy", "selected"} {
				wantIncluded := tc.want != nil && *tc.want == id
				if containsVideo(dashboard.Videos, id) != wantIncluded {
					t.Fatalf("video %q inclusion did not follow Featured selection", id)
				}
			}
		})
	}
}
