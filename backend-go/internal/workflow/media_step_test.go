package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/mediadownload"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestMediaStepIsPartialWhileImagesRemainQueued(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := range 3 {
		u := fmt.Sprintf("https://pbs.twimg.com/media/step-%d.png", i)
		h := sha256.Sum256([]byte(u))
		testutil.Exec(t, p, `INSERT INTO media_assets(id,source,source_url,status) VALUES(?,'x',?,'pending')`, hex.EncodeToString(h[:]), u)
	}
	client := mediadownload.NewClient(&http.Client{Transport: xTestRT(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\n" + r.URL.Path))}, nil
	})})
	steps := &SourceSteps{Store: s, Media: client, MediaBounds: &mediadownload.Bounds{Batch: 1, Files: 2, Budget: time.Minute}}
	out, err := steps.Execute(context.Background(), StepInput{StepID: "media"})
	if err != nil || out.Status != "partial" || out.Result["ready"] != 2 || out.Result["remaining"] != 1 || out.Result["stopped"] != mediadownload.StoppedFileCap {
		t.Fatalf("first out=%#v err=%v", out, err)
	}
	// The next run continues with what is left and empties the queue.
	out, err = steps.Execute(context.Background(), StepInput{StepID: "media"})
	if err != nil || out.Status != "succeeded" || out.Result["ready"] != 1 || out.Result["remaining"] != 0 || out.Result["stopped"] != nil {
		t.Fatalf("second out=%#v err=%v", out, err)
	}
}
