// Package jobs describes operator-triggered work in the shape the admin
// console and MCP control tools already consume. The durable workflow queue
// implements Manager; consumers depend only on these types.
package jobs

import (
	"context"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
)

const (
	KindSync     = "sync"
	KindScan     = "scan"
	KindWorkflow = "workflow"
	// KindLive is the YouTube live-status probe, started by its own timer.
	KindLive = "live"

	// StepLive is the probe's only step. It is not part of StepOrder, so
	// saved workflows and full synchronizations never select it.
	StepLive = "live"
	// TriggeredByLiveProbe labels probe runs in the run history.
	TriggeredByLiveProbe = "live-probe"

	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"

	ReasonAlreadyRunning   = "already_running"
	ReasonNoSteps          = "no_steps"
	ReasonWorkflowNotFound = "workflow_not_found"
)

// Job is one single-flight unit of work. Progress maps each step to pending,
// running, completed, partial, failed, skipped or cancelled.
type Job struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Status       string            `json:"status"`
	CreatedAt    string            `json:"createdAt"`
	StartedAt    *string           `json:"startedAt"`
	FinishedAt   *string           `json:"finishedAt"`
	TriggeredBy  string            `json:"triggeredBy"`
	WorkflowID   *string           `json:"workflowId"`
	WorkflowName *string           `json:"workflowName"`
	Steps        []string          `json:"steps"`
	FetchWindows map[string]any    `json:"fetchWindows"`
	Progress     map[string]string `json:"progress"`
	Error        *string           `json:"error"`
	Result       map[string]any    `json:"result"`
	Run          map[string]any    `json:"run"`
}

// StartResult mirrors the accepted/reason/job triple returned to operators.
// Reason is nil when the job was accepted.
type StartResult struct {
	Accepted bool    `json:"accepted"`
	Reason   *string `json:"reason"`
	Job      *Job    `json:"job"`
}

// Options narrows a workflow job. Steps and FetchWindows are ignored for the
// sync and scan kinds.
type Options struct {
	WorkflowID   string
	Steps        []string
	FetchWindows map[string]history.Window
}

type Manager interface {
	// Start reserves the single-flight slot. A busy queue is reported through
	// StartResult, not through the error.
	Start(ctx context.Context, kind, triggeredBy string, options Options) (StartResult, error)
	Get(ctx context.Context, id string) (*Job, error)
	List(ctx context.Context, limit int) ([]Job, error)
	Active(ctx context.Context) (*Job, error)
}
