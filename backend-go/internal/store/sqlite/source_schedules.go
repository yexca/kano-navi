package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// SourceSchedule is the timer of one source (ADR-0003). When it is due the
// scheduler submits the source's fixed chain as the ad hoc run
// SourceScheduleWorkflow(source).
type SourceSchedule struct {
	Source          string `json:"source"`
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	// Sync is how X and YouTube are read (ADR-0002); Bilibili has none.
	Sync      *SourceSync        `json:"sync"`
	NextRunAt *string            `json:"nextRunAt"`
	UpdatedAt *string            `json:"updatedAt"`
	LastRun   *SourceScheduleRun `json:"lastRun"`
}

// SourceScheduleRun is the newest run a source schedule submitted.
type SourceScheduleRun struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	At     string `json:"at"`
}

const (
	// SourceScheduleMinMinutes and SourceScheduleMaxMinutes bound every
	// source interval, as they bound workflow timers. Bilibili's lower bound
	// is BILIBILI_MIN_INTERVAL_MINUTES instead.
	SourceScheduleMinMinutes = 15
	SourceScheduleMaxMinutes = 7 * 24 * 60
	// sourceSchedulePrefix names the ad hoc runs of source schedules.
	sourceSchedulePrefix = "@source-"
	// sourceSchedulesMigrated guards the one-time move of workflow timers.
	sourceSchedulesMigrated = "source_schedules_migrated"
)

// ScheduleSources are the sources with a schedule, in catalog order.
var ScheduleSources = []string{"x", "youtube", "bilibili"}

// SourceScheduleWorkflow is the ad hoc workflow ID of a source's runs.
func SourceScheduleWorkflow(source string) string { return sourceSchedulePrefix + source }

// SourceScheduleOf reports the source of a source schedule's run, if it is one.
func SourceScheduleOf(workflowID string) (string, bool) {
	source, found := strings.CutPrefix(workflowID, sourceSchedulePrefix)
	return source, found && slices.Contains(ScheduleSources, source)
}

// SourceScheduleBounds are the interval bounds of a source. floor is the
// Bilibili list interval in minutes.
func SourceScheduleBounds(source string, floor int) (int, int) {
	if source == "bilibili" {
		return min(max(floor, SourceScheduleMinMinutes), SourceScheduleMaxMinutes), SourceScheduleMaxMinutes
	}
	return SourceScheduleMinMinutes, SourceScheduleMaxMinutes
}

func defaultScheduleInterval(source string) int {
	switch source {
	case "x":
		return 30
	case "youtube":
		return 60
	}
	return 180
}

// defaultSourceSchedule is a source's schedule before it is first saved:
// disabled, as a fresh installation keeps it.
func defaultSourceSchedule(source string, floor int) SourceSchedule {
	low, high := SourceScheduleBounds(source, floor)
	schedule := SourceSchedule{Source: source, IntervalMinutes: min(max(defaultScheduleInterval(source), low), high)}
	if slices.Contains(SyncSources, source) {
		schedule.Sync = &SourceSync{Mode: SyncIncremental, Max: DefaultSyncMax}
	}
	return schedule
}

// hasSourceSchedules reports the table, which a read-only snapshot written
// by an earlier release lacks.
func (s *Store) hasSourceSchedules(ctx context.Context) (bool, error) {
	var n int
	if s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='source_schedules'`).Scan(&n) != nil {
		return false, ErrWorkflowStorage
	}
	return n == 1, nil
}

// SourceSchedules lists every source's schedule with its newest run. A
// snapshot without the table reads as the disabled defaults.
func (s *Store) SourceSchedules(ctx context.Context, floor int) ([]SourceSchedule, error) {
	if s == nil || s.db == nil {
		return nil, ErrWorkflowStorage
	}
	out := make([]SourceSchedule, 0, len(ScheduleSources))
	for _, source := range ScheduleSources {
		out = append(out, defaultSourceSchedule(source, floor))
	}
	present, err := s.hasSourceSchedules(ctx)
	if err != nil {
		return nil, err
	}
	if present {
		rows, err := s.db.QueryContext(ctx, `SELECT source,enabled,interval_minutes,sync_json,next_run_at,updated_at FROM source_schedules`)
		if err != nil {
			return nil, ErrWorkflowStorage
		}
		for rows.Next() {
			var source, sync, updated string
			var enabled, interval int
			var next sql.NullString
			if rows.Scan(&source, &enabled, &interval, &sync, &next, &updated) != nil {
				rows.Close()
				return nil, ErrWorkflowStorage
			}
			index := slices.Index(ScheduleSources, source)
			if index < 0 {
				continue
			}
			x := &out[index]
			low, high := SourceScheduleBounds(source, floor)
			x.Enabled = enabled != 0
			x.IntervalMinutes = min(max(interval, low), high)
			if x.Sync != nil {
				setting := readSourceSync(sync)[source]
				x.Sync = &setting
			}
			x.NextRunAt = nullableWorkflowString(next)
			x.UpdatedAt = &updated
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, ErrWorkflowStorage
		}
	}
	if s.WorkflowAvailable() {
		for i := range out {
			var run SourceScheduleRun
			err := s.db.QueryRowContext(ctx, `SELECT id,status,COALESCE(finished_at,started_at,created_at) FROM workflow_runs WHERE workflow_id=? ORDER BY julianday(created_at) DESC,id DESC LIMIT 1`, SourceScheduleWorkflow(out[i].Source)).Scan(&run.ID, &run.Status, &run.At)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, ErrWorkflowStorage
			}
			run.Status = workflowStatus(run.Status, false)
			out[i].LastRun = &run
		}
	}
	return out, nil
}

// SourceScheduleFor is one source's schedule.
func (s *Store) SourceScheduleFor(ctx context.Context, source string, floor int) (SourceSchedule, error) {
	all, err := s.SourceSchedules(ctx, floor)
	if err != nil {
		return SourceSchedule{}, err
	}
	for _, schedule := range all {
		if schedule.Source == source {
			return schedule, nil
		}
	}
	return SourceSchedule{}, ErrWorkflowNotFound
}

// SaveSourceSchedule stores a source's timer. Enabling it or changing its
// interval plans the next run one interval later, never immediately; a sync
// change alone keeps the plan.
func (s *Store) SaveSourceSchedule(ctx context.Context, input SourceSchedule, floor int, now time.Time) (SourceSchedule, error) {
	current, err := s.SourceScheduleFor(ctx, input.Source, floor)
	if err != nil {
		return SourceSchedule{}, err
	}
	low, high := SourceScheduleBounds(input.Source, floor)
	if input.IntervalMinutes < low || input.IntervalMinutes > high || (input.Sync == nil) != (current.Sync == nil) || (input.Sync != nil && !input.Sync.Valid()) {
		return SourceSchedule{}, ErrWorkflowValidation
	}
	tx, err := s.workflowTx(ctx)
	if err != nil {
		return SourceSchedule{}, err
	}
	defer tx.Rollback()
	next := current.NextRunAt
	switch {
	case !input.Enabled:
		next = nil
	case !current.Enabled || current.IntervalMinutes != input.IntervalMinutes || next == nil:
		planned := workflowTime(now.Add(time.Duration(input.IntervalMinutes) * time.Minute))
		next = &planned
	}
	if err = upsertSourceSchedule(ctx, tx, input, next, now); err != nil {
		return SourceSchedule{}, err
	}
	if tx.Commit() != nil {
		return SourceSchedule{}, ErrWorkflowStorage
	}
	return s.SourceScheduleFor(ctx, input.Source, floor)
}

func upsertSourceSchedule(ctx context.Context, tx *sql.Tx, schedule SourceSchedule, next *string, now time.Time) error {
	sync := "{}"
	if schedule.Sync != nil {
		encoded, err := json.Marshal(map[string]SourceSync{schedule.Source: *schedule.Sync})
		if err != nil {
			return ErrWorkflowValidation
		}
		sync = string(encoded)
	}
	var planned any
	if next != nil {
		planned = *next
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_schedules(source,enabled,interval_minutes,sync_json,next_run_at,updated_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(source) DO UPDATE SET enabled=excluded.enabled,interval_minutes=excluded.interval_minutes,sync_json=excluded.sync_json,next_run_at=excluded.next_run_at,updated_at=excluded.updated_at`,
		schedule.Source, schedule.Enabled, schedule.IntervalMinutes, sync, planned, workflowTime(now)); err != nil {
		return ErrWorkflowStorage
	}
	return nil
}

// DueSourceSchedule is the enabled schedule with the earliest next run at or
// before now, leaving out the skipped sources, or nil.
func (s *Store) DueSourceSchedule(ctx context.Context, now time.Time, skip map[string]bool, floor int) (*SourceSchedule, error) {
	if s == nil || s.db == nil {
		return nil, ErrWorkflowStorage
	}
	if present, err := s.hasSourceSchedules(ctx); err != nil || !present {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source FROM source_schedules WHERE enabled=1 AND next_run_at IS NOT NULL AND julianday(next_run_at)<=julianday(?) ORDER BY julianday(next_run_at),source`, workflowTime(now))
	if err != nil {
		return nil, ErrWorkflowStorage
	}
	due := []string{}
	for rows.Next() {
		var source string
		if rows.Scan(&source) != nil {
			rows.Close()
			return nil, ErrWorkflowStorage
		}
		due = append(due, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, ErrWorkflowStorage
	}
	for _, source := range due {
		if skip[source] || !slices.Contains(ScheduleSources, source) {
			continue
		}
		schedule, err := s.SourceScheduleFor(ctx, source, floor)
		if err != nil {
			return nil, err
		}
		return &schedule, nil
	}
	return nil, nil
}

// SourceScheduleSync is how each source schedule reads X and YouTube, which a
// manual run of a saved workflow and a push run read with as well.
func (s *Store) SourceScheduleSync(ctx context.Context) (map[string]SourceSync, error) {
	schedules, err := s.SourceSchedules(ctx, SourceScheduleMinMinutes)
	if err != nil {
		return nil, err
	}
	out := map[string]SourceSync{}
	for _, schedule := range schedules {
		if schedule.Sync != nil {
			out[schedule.Source] = *schedule.Sync
		}
	}
	return out, nil
}

// planSourceSchedule moves a source schedule's next run one interval past a
// claimed run of it.
func planSourceSchedule(ctx context.Context, tx *sql.Tx, source string, now time.Time) error {
	var interval, enabled int
	err := tx.QueryRowContext(ctx, `SELECT interval_minutes,enabled FROM source_schedules WHERE source=?`, source).Scan(&interval, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return ErrWorkflowStorage
	}
	var next any
	if enabled != 0 {
		next = workflowTime(now.Add(time.Duration(min(max(interval, SourceScheduleMinMinutes), SourceScheduleMaxMinutes)) * time.Minute))
	}
	if _, err = tx.ExecContext(ctx, `UPDATE source_schedules SET next_run_at=? WHERE source=?`, next, source); err != nil {
		return ErrWorkflowStorage
	}
	return nil
}

// PrepareSourceSchedules runs at every start of the process that owns the
// timers. floor is BILIBILI_MIN_INTERVAL_MINUTES. Once per database it moves
// the timers of saved workflows that read a source to the source schedules:
// each such source is enabled with the smallest interval among them (raised
// to the floor) and the sourceSync of that workflow, then those workflow
// timers are disabled. Workflows without a source step keep their timers.
// On every start a Bilibili interval below a raised floor is raised to it.
func (s *Store) PrepareSourceSchedules(ctx context.Context, floor int, now time.Time) error {
	tx, err := s.workflowTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var migrated int
	if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_settings WHERE key=?`, sourceSchedulesMigrated).Scan(&migrated) != nil {
		return ErrWorkflowStorage
	}
	if migrated == 0 {
		if err = migrateWorkflowTimers(ctx, tx, floor, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,'1',?)`, sourceSchedulesMigrated, workflowTime(now)); err != nil {
			return ErrWorkflowStorage
		}
	}
	low, _ := SourceScheduleBounds("bilibili", floor)
	if _, err = tx.ExecContext(ctx, `UPDATE source_schedules SET interval_minutes=?,updated_at=? WHERE source='bilibili' AND interval_minutes<?`, low, workflowTime(now), low); err != nil {
		return ErrWorkflowStorage
	}
	if tx.Commit() != nil {
		return ErrWorkflowStorage
	}
	return nil
}

func migrateWorkflowTimers(ctx context.Context, tx *sql.Tx, floor int, now time.Time) error {
	// The retired source_sync_json is read here once, to carry it over.
	rows, err := tx.QueryContext(ctx, `SELECT id,steps_json,interval_minutes,source_sync_json FROM workflows WHERE schedule_enabled=1 ORDER BY created_at,id`)
	if err != nil {
		return ErrWorkflowStorage
	}
	schedules := map[string]*SourceSchedule{}
	moved := []string{}
	for rows.Next() {
		var id, steps, sync string
		var interval int
		if rows.Scan(&id, &steps, &interval, &sync) != nil {
			rows.Close()
			return ErrWorkflowStorage
		}
		var selected []string
		if json.Unmarshal([]byte(steps), &selected) != nil {
			continue
		}
		settings := readSourceSync(sync)
		reads := false
		for _, source := range ScheduleSources {
			if !slices.Contains(selected, source) {
				continue
			}
			reads = true
			low, high := SourceScheduleBounds(source, floor)
			minutes := min(max(interval, low), high)
			if current := schedules[source]; current != nil && current.IntervalMinutes <= minutes {
				continue
			}
			schedule := defaultSourceSchedule(source, floor)
			schedule.Enabled = true
			schedule.IntervalMinutes = minutes
			if schedule.Sync != nil {
				setting := settings[source]
				schedule.Sync = &setting
			}
			schedules[source] = &schedule
		}
		if reads {
			moved = append(moved, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrWorkflowStorage
	}
	for _, source := range ScheduleSources {
		schedule := schedules[source]
		if schedule == nil {
			continue
		}
		next := workflowTime(now.Add(time.Duration(schedule.IntervalMinutes) * time.Minute))
		if err = upsertSourceSchedule(ctx, tx, *schedule, &next, now); err != nil {
			return err
		}
	}
	for _, id := range moved {
		if _, err = tx.ExecContext(ctx, `UPDATE workflows SET schedule_enabled=0,next_run_at=NULL,updated_at=? WHERE id=?`, workflowTime(now), id); err != nil {
			return ErrWorkflowStorage
		}
	}
	return nil
}
