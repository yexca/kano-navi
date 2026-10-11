package publicview

import (
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

type PublicSyncRunResponse struct {
	ID         int64   `json:"id"`
	Source     string  `json:"source"`
	StartedAt  string  `json:"startedAt"`
	FinishedAt *string `json:"finishedAt"`
	Status     string  `json:"status"`
	Message    *string `json:"message"`
}

type PublicHealthResponse struct {
	OK       bool                   `json:"ok"`
	Service  string                 `json:"service"`
	Now      string                 `json:"now"`
	LastSync *PublicSyncRunResponse `json:"lastSync"`
}

type PublicRevisionResponse struct {
	Revision int64 `json:"revision"`
	Changed  bool  `json:"changed"`
}

func PublicSyncRun(run *sqlite.SyncRun) *PublicSyncRunResponse {
	if run == nil {
		return nil
	}
	message, ok := map[string]string{
		"success": "同步完成",
		"partial": "部分数据源不可用或存在警告，保留已有快照",
		"failed":  "同步失败，保留已有快照",
		"running": "同步进行中",
	}[run.Status]
	var publicMessage *string
	if ok {
		publicMessage = &message
	}
	return &PublicSyncRunResponse{
		ID:         run.ID,
		Source:     run.Source,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
		Status:     run.Status,
		Message:    publicMessage,
	}
}

func Health(now time.Time, run *sqlite.SyncRun) PublicHealthResponse {
	return PublicHealthResponse{
		OK:       true,
		Service:  "kano-status-board",
		Now:      now.UTC().Format("2006-01-02T15:04:05.000Z"),
		LastSync: PublicSyncRun(run),
	}
}

func Revision(revision int64, since *int64) PublicRevisionResponse {
	changed := true
	if since != nil {
		changed = revision != *since
	}
	return PublicRevisionResponse{Revision: revision, Changed: changed}
}
