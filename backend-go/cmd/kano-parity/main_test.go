package main

import (
	"encoding/json"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestRenderReadsSyntheticSnapshot(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_upcoming) VALUES ('parity-video','youtube','Synthetic','2026-10-10T00:00:00Z','2026-10-11T04:00:00Z','https://youtube.com/watch?v=parity-video','stream',1)`)
	testutil.Exec(t, path, `INSERT INTO focus (id,video_id,title) VALUES (1,'parity-video','Focus')`)
	encoded, err := render(path, 3, "2026-10-11T03:00:00.000Z")
	if err != nil {
		t.Fatal(err)
	}
	var projection map[string]any
	if err := json.Unmarshal(encoded, &projection); err != nil {
		t.Fatal(err)
	}
	meta := projection["meta"].(map[string]any)
	if meta["featuredVideoId"] != "parity-video" {
		t.Fatalf("featured fallback = %v", meta["featuredVideoId"])
	}
}
