package workflow

import (
	"context"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

type retryOnce struct{ calls int }

func (x *retryOnce) Execute(_ context.Context, in StepInput) (AttemptResult, error) {
	x.calls++
	if x.calls == 1 {
		return AttemptResult{}, NewStepError("temporary_dependency", ErrDependency)
	}
	return AttemptResult{Status: "succeeded", Result: map[string]any{"step": in.StepID}}, nil
}
func TestExecutorPersistsOrderedSyntheticRunAndRetry(t *testing.T) {
	path := testutil.NewSnapshot(t)
	bootstrap, err := sqlite.OpenWorkflowCore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap.Close()
	testutil.Exec(t, path, `INSERT INTO workflows(id,name,steps_json,source_lookback_json,created_at,updated_at) VALUES ('w','Workflow','["x","schedule"]','{}','2026-10-12T00:00:00Z','2026-10-12T00:00:00Z')`)
	store, err := sqlite.OpenWorkflowCore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	x := &retryOnce{}
	ex := &Executor{Store: store, Steps: x, OwnerID: "test", MaxAttempts: 2}
	run, err := ex.Run(context.Background(), "w", TriggerOperator, "idem")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "succeeded" || x.calls != 3 {
		t.Fatalf("run=%#v calls=%d", run, x.calls)
	}
	got, err := store.GetWorkflowRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps) != 2 || len(got.Steps[0].Attempts) != 2 || got.Steps[1].StepID != "schedule" {
		t.Fatalf("steps=%#v", got.Steps)
	}
}
