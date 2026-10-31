// Package jobs describes operator-triggered work in the shape the admin
// console and MCP control tools already consume. The durable workflow queue
// implements Manager; consumers depend only on these types.
package jobs

import (
	"context"

	"github.com/yexca/kano-navi/backend/internal/source/history"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

const (
	KindSync     = "sync"
	KindScan     = "scan"
	KindWorkflow = "workflow"
	// KindLive is the YouTube live-status probe, started by its own timer.
	KindLive = "live"
	// KindTranslate runs the machine translation step alone, as an
	// operator's incremental translation does.
	KindTranslate = "translate"

	// StepTranslate translates post text and video titles.
	StepTranslate = "translate"

	// StepLive is the probe's only step. It is not part of StepOrder, so
	// step runs and full synchronizations never select it.
	StepLive = "live"
	// TriggeredByLiveProbe labels probe runs in the run history.
	TriggeredByLiveProbe = "live-probe"

	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"

	ReasonAlreadyRunning = "already_running"
	ReasonNoSteps        = "no_steps"
)

// Job is one single-flight unit of work. Progress maps each step to pending,
// running, completed, partial, failed, skipped or cancelled.
type Job struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
	StartedAt   *string `json:"startedAt"`
	FinishedAt  *string `json:"finishedAt"`
	TriggeredBy string  `json:"triggeredBy"`
	// WorkflowID names the saved workflow a stored run belongs to; every
	// new run has none (ADR-0003).
	WorkflowID   *string        `json:"workflowId"`
	Steps        []string       `json:"steps"`
	FetchWindows map[string]any `json:"fetchWindows"`
	// SourceSchedule names the source whose chain the run is, from its timer
	// (triggeredBy "source-schedule") or the operator's Run now.
	SourceSchedule *string           `json:"sourceSchedule"`
	Progress       map[string]string `json:"progress"`
	Error          *string           `json:"error"`
	Result         map[string]any    `json:"result"`
	Run            map[string]any    `json:"run"`
}

// StartResult mirrors the accepted/reason/job triple returned to operators.
// Reason is nil when the job was accepted.
type StartResult struct {
	Accepted bool    `json:"accepted"`
	Reason   *string `json:"reason"`
	Job      *Job    `json:"job"`
}

// Options narrows a workflow job. Source, Steps, FetchWindows and SourceSync
// are ignored for the other kinds.
type Options struct {
	// Source runs that source's fixed chain once (Run now) instead of Steps.
	Source       string
	Steps        []string
	FetchWindows map[string]history.Window
	// SourceSync freezes sync settings on the run. Source steps it leaves
	// out, and that have no window, read as their source schedule does.
	SourceSync map[string]sqlite.SourceSync
}

type Manager interface {
	// Start reserves the single-flight slot. A busy queue is reported through
	// StartResult, not through the error.
	Start(ctx context.Context, kind, triggeredBy string, options Options) (StartResult, error)
	Get(ctx context.Context, id string) (*Job, error)
	List(ctx context.Context, limit int) ([]Job, error)
	Active(ctx context.Context) (*Job, error)
}
