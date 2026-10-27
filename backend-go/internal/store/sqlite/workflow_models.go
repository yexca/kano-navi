package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
)

var (
	ErrWorkflowUnavailable = errors.New("workflow capability unavailable")
	ErrWorkflowBusy        = errors.New("workflow already active")
	ErrWorkflowNotFound    = errors.New("workflow not found")
	ErrWorkflowValidation  = errors.New("workflow validation failed")
	ErrLeaseLost           = errors.New("workflow lease lost")
	ErrWorkflowStorage     = errors.New("workflow storage unavailable")
)

type WorkflowDefinition struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Steps []string `json:"steps"`
	// SourceSync is how each source step of a run reads, from the source
	// schedules (ADR-0003). It is frozen on the run, never saved with a
	// definition.
	SourceSync      map[string]SourceSync `json:"-"`
	ScheduleEnabled bool                  `json:"scheduleEnabled"`
	IntervalMinutes int                   `json:"intervalMinutes"`
	LastRunAt       *string               `json:"lastRunAt"`
	LastJobID       *string               `json:"lastJobId"`
	LastStatus      *string               `json:"lastStatus"`
	NextRunAt       *string               `json:"nextRunAt"`
	CreatedAt       string                `json:"createdAt"`
	UpdatedAt       string                `json:"updatedAt"`
	// SourceLookbackDays are the days of an operator's recent fetch windows.
	// Saved definitions no longer use them.
	SourceLookbackDays map[string]int `json:"-"`
	// FetchWindows are operator-selected source windows of an unsaved fetch.
	// They are frozen on the run and never stored with a saved definition.
	FetchWindows map[string]history.Window `json:"-"`
}
type WorkflowAttempt struct {
	AttemptNumber int     `json:"attemptNumber"`
	Status        string  `json:"status"`
	ErrorCode     *string `json:"errorCode"`
	Retryable     bool    `json:"retryable"`
	StartedAt     string  `json:"startedAt"`
	FinishedAt    *string `json:"finishedAt"`
	DurationMS    *int64  `json:"durationMs"`
}
type WorkflowStep struct {
	ID         int64             `json:"-"`
	StepID     string            `json:"stepId"`
	StepOrder  int               `json:"stepOrder"`
	Status     string            `json:"status"`
	Capability string            `json:"capability"`
	StartedAt  *string           `json:"startedAt"`
	FinishedAt *string           `json:"finishedAt"`
	ErrorCode  *string           `json:"errorCode"`
	Retryable  bool              `json:"retryable"`
	Attempts   []WorkflowAttempt `json:"attempts"`
}
type WorkflowRun struct {
	ID                      string                    `json:"id"`
	WorkflowID              string                    `json:"workflowId"`
	Status                  string                    `json:"status"`
	TriggerSource           string                    `json:"triggerSource"`
	ExecutionSource         string                    `json:"executionSource"`
	CreatedAt               string                    `json:"createdAt"`
	StartedAt               *string                   `json:"startedAt"`
	FinishedAt              *string                   `json:"finishedAt"`
	ErrorCode               *string                   `json:"errorCode"`
	CancellationRequestedAt *string                   `json:"cancellationRequestedAt"`
	RecoveryCount           int                       `json:"recoveryCount"`
	SourceLookbackDays      map[string]int            `json:"sourceLookbackDays,omitempty"`
	SourceSync              map[string]SourceSync     `json:"sourceSync,omitempty"`
	FetchWindows            map[string]history.Window `json:"fetchWindows,omitempty"`
	Steps                   []WorkflowStep            `json:"steps,omitempty"`
}
type WorkflowPage struct {
	Workflows  []WorkflowDefinition `json:"workflows"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"pageSize"`
	Total      int                  `json:"total"`
	TotalPages int                  `json:"totalPages"`
	HasNext    bool                 `json:"hasNext"`
}
type WorkflowRunPage struct {
	Runs       []WorkflowRun `json:"runs"`
	Page       int           `json:"page"`
	PageSize   int           `json:"pageSize"`
	Total      int           `json:"total"`
	TotalPages int           `json:"totalPages"`
	HasNext    bool          `json:"hasNext"`
}
type WorkflowLease struct {
	RunID, OwnerID string
	Generation     int
	ExpiresAt      time.Time
}
type WorkflowOutcome struct {
	Status, Code string
	Retryable    bool
}

func workflowTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
func nullableWorkflowString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	x := v.String
	return &x
}
func workflowCode(raw sql.NullString) *string {
	if !raw.Valid {
		return nil
	}
	codes := map[string]bool{"permanent": true, "retryable": true, "timeout": true, "rate_limited": true, "dependency_unavailable": true, "schema_failure": true, "validation_failure": true, "cancelled": true, "blocked": true, "capability_not_migrated": true, "lease_expired": true, "internal": true}
	if !codes[raw.String] {
		v := "internal"
		return &v
	}
	v := raw.String
	return &v
}
func workflowStatus(v string, step bool) string {
	if v == "queued" && !step || v == "pending" && step {
		return v
	}
	switch v {
	case "running", "succeeded", "partial", "failed", "cancelled", "blocked":
		return v
	}
	return "blocked"
}
func workflowTrigger(v string) string {
	switch v {
	case "operator", "scheduler", "recovery":
		return v
	}
	return "operator"
}
func (s *Store) WorkflowAvailable() bool {
	var n int
	return s != nil && s.db != nil && s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='workflow_migrations'`).Scan(&n) == nil && n == 1
}
func (s *Store) requireWorkflow() error {
	if !s.WorkflowAvailable() {
		return ErrWorkflowUnavailable
	}
	return nil
}
func (s *Store) workflowTx(ctx context.Context) (*sql.Tx, error) {
	if s == nil || !s.workflowWritable {
		return nil, ErrWorkflowUnavailable
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	return tx, nil
}
func affected(result sql.Result, err error) error {
	if err != nil {
		return ErrWorkflowStorage
	}
	n, e := result.RowsAffected()
	if e != nil {
		return ErrWorkflowStorage
	}
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}
