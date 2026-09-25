package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/notifications"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// Report run triggers (fork_report_archive.trigger_kind).
const (
	ReportTriggerScheduled = "scheduled"
	ReportTriggerManual    = "manual"
)

// ScheduledReportRunPayload identifies one report run. A scheduled run is the
// report plus the slot (the next_run_at it was enqueued for); a manual run
// carries a fresh uuid. Payloads without Trigger come from images before
// increment D and are discarded by the handler.
type ScheduledReportRunPayload struct {
	ReportID string     `json:"report_id"`
	Host     string     `json:"host,omitempty"`
	Trigger  string     `json:"trigger,omitempty"`
	SlotAt   *time.Time `json:"slot_at,omitempty"`
	RunID    string     `json:"run_id,omitempty"`
}

// RunKey is the archive's unique run identity and the base of the asynq task id.
func (p ScheduledReportRunPayload) RunKey() (string, error) {
	if strings.TrimSpace(p.ReportID) == "" {
		return "", errors.New("report run: missing report id")
	}
	switch p.Trigger {
	case ReportTriggerScheduled:
		if p.SlotAt == nil {
			return "", errors.New("report run: scheduled run without slot")
		}
		return "sched:" + p.Host + ":" + p.ReportID + ":" + strconv.FormatInt(p.SlotAt.Unix(), 10), nil
	case ReportTriggerManual:
		if p.RunID == "" {
			return "", errors.New("report run: manual run without run id")
		}
		return "manual:" + p.Host + ":" + p.ReportID + ":" + p.RunID, nil
	}
	return "", fmt.Errorf("report run: unknown trigger %q", p.Trigger)
}

func scheduledReportTaskID(runKey string) string { return "srr:" + runKey }

// NewScheduledReportRunTask builds the asynq task; the id is the run key, so a
// second enqueue for the same slot is a no-op while the first is still queued.
func NewScheduledReportRunTask(p ScheduledReportRunPayload) (*asynq.Task, error) {
	key, err := p.RunKey()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeScheduledReportRun, b,
		asynq.Queue(QueueScheduledReports),
		asynq.MaxRetry(3),
		asynq.TaskID(scheduledReportTaskID(key)),
	), nil
}

// EnqueueScheduledReportAt enqueues the scheduled run for one slot. Slots are
// whole seconds in UTC (matches ForkClaimScheduledReportSlot). Duplicates for
// the same slot are silently ignored; a past slot runs immediately.
func EnqueueScheduledReportAt(qc *asynq.Client, reportID, host string, slot time.Time) error {
	if qc == nil {
		return nil
	}
	slot = slot.UTC().Truncate(time.Second)
	task, err := NewScheduledReportRunTask(ScheduledReportRunPayload{ReportID: reportID, Host: host, Trigger: ReportTriggerScheduled, SlotAt: &slot})
	if err != nil {
		return err
	}
	_, err = qc.Enqueue(task, asynq.ProcessAt(slot))
	if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// EnqueueScheduledReportManual enqueues a "run now" and returns its run id.
func EnqueueScheduledReportManual(qc *asynq.Client, reportID, host string) (string, error) {
	if qc == nil {
		return "", errors.New("queue not configured")
	}
	runID := uuid.NewString()
	task, err := NewScheduledReportRunTask(ScheduledReportRunPayload{ReportID: reportID, Host: host, Trigger: ReportTriggerManual, RunID: runID})
	if err != nil {
		return "", err
	}
	if _, err := qc.Enqueue(task); err != nil {
		return "", err
	}
	return runID, nil
}

// nextReportSlot is the next cron instant after from (24 h fallback on a bad expression).
func nextReportSlot(cronExpr, tz string, from time.Time) time.Time {
	if tz == "" {
		tz = "UTC"
	}
	next, err := notifications.NextCronRun(cronExpr, tz, from)
	if err != nil {
		return from.Add(24 * time.Hour)
	}
	return next
}

// enqueueReportAtStoredSlot enqueues the run for the report's stored
// next_run_at (the slot the worker will claim). A NULL next_run_at is filled
// with the next cron instant first. A past slot fires immediately: the claim
// makes duplicates impossible, so a slot missed while the server was down is
// delivered once, late, instead of skipped.
func enqueueReportAtStoredSlot(ctx context.Context, d *database.DB, qc *asynq.Client, r db.ScheduledReport, host string, now time.Time, log *slog.Logger) {
	slot := r.NextRunAt.Time
	if !r.NextRunAt.Valid {
		slot = nextReportSlot(r.CronExpr, r.Timezone, now)
		if _, err := d.Queries.ForkSetScheduledReportNextRunIfNull(ctx, db.ForkSetScheduledReportNextRunIfNullParams{ID: r.ID, Next: pgtime.From(slot)}); err != nil {
			if log != nil {
				log.Error("scheduled_report: set next_run_at failed", "report_id", r.ID, "error", err)
			}
			return
		}
	}
	if err := EnqueueScheduledReportAt(qc, r.ID, host, slot); err != nil && log != nil {
		log.Error("scheduled_report: enqueue failed", "report_id", r.ID, "slot", slot, "error", err)
	}
}
