package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	hostctx "github.com/PatchMon/PatchMon/server-source-code/internal/context"
	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
)

// patchScheduleTolerance is how far past its slot a schedule still fires. The
// dispatcher runs every minute, so this only needs to absorb short restarts or
// dispatch hiccups. Anything older is deliberately dropped: a server that was
// down for hours must not patch fleets on catch-up. One-shot schedules that
// miss their window are disabled and marked missed.
const patchScheduleTolerance = 5 * time.Minute

// scheduledPatchType is the patch type used for scheduled runs: a scheduled
// patch always upgrades everything in the group (no per-package scheduling).
const scheduledPatchType = "patch_all"

// PatchSchedulesDispatchHandler fires due patch schedules. At each due slot it
// creates a patch_run per host in the schedule's group and enqueues a run_patch
// task, mirroring the manual /patching/trigger path. Unlike reboot schedules
// there is no allowlist and no server self-exclusion: patching a host (incl.
// the PatchMon host) is a normal package upgrade. The schedule creator's
// can_manage_patching permission is revalidated at execution time, and an audit
// intent entry is written before any task is enqueued.
type PatchSchedulesDispatchHandler struct {
	defaultDB *database.DB
	poolCache *hostctx.PoolCache
	qc        *asynq.Client
	log       *slog.Logger
}

// NewPatchSchedulesDispatchHandler creates the handler.
func NewPatchSchedulesDispatchHandler(defaultDB *database.DB, poolCache *hostctx.PoolCache, qc *asynq.Client, log *slog.Logger) *PatchSchedulesDispatchHandler {
	return &PatchSchedulesDispatchHandler{defaultDB: defaultDB, poolCache: poolCache, qc: qc, log: log}
}

// ProcessTask implements asynq.Handler. Mirrors the reboot-schedule dispatcher:
// a payload pins one tenant DB, no payload means the default DB plus every
// cached tenant pool. Tenants whose pool is not currently cached are not
// evaluated; their due slots expire fail-closed (no patch) until traffic
// re-populates the pool.
func (h *PatchSchedulesDispatchHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	payload := t.Payload()
	if len(payload) > 0 && h.poolCache != nil {
		var p AutomationPayload
		if err := json.Unmarshal(payload, &p); err == nil && strings.TrimSpace(p.Host) != "" {
			if resolved, err := h.poolCache.GetOrCreate(ctx, p.Host); err == nil && resolved != nil {
				h.processDB(ctx, resolved, p.Host)
				return nil
			}
		}
	}
	h.processDB(ctx, h.defaultDB, "")
	if h.poolCache != nil {
		for _, host := range h.poolCache.ListHosts() {
			d, err := h.poolCache.GetOrCreate(ctx, host)
			if err != nil || d == nil {
				continue
			}
			h.processDB(ctx, d, host)
		}
	}
	return nil
}

func (h *PatchSchedulesDispatchHandler) processDB(ctx context.Context, d *database.DB, tenantHost string) {
	now := time.Now()
	schedules, err := d.Queries.ListEnabledPatchSchedules(ctx)
	if err != nil {
		h.logError("patch_schedules_dispatch: list enabled", "error", err)
		return
	}
	for _, s := range schedules {
		slot, due, missed, err := patchScheduleSlot(s, now)
		if err != nil {
			h.logError("patch_schedules_dispatch: invalid schedule", "schedule_id", s.ID, "error", err)
			continue
		}
		if missed {
			// One-shot schedule whose window passed while the server was down.
			// Expire it instead of patching hours late.
			if err := d.Queries.MarkPatchScheduleMissed(ctx, db.MarkPatchScheduleMissedParams{
				ID:       s.ID,
				MissedAt: pgtime.From(slot),
			}); err != nil {
				h.logError("patch_schedules_dispatch: mark missed", "schedule_id", s.ID, "error", err)
				continue
			}
			h.logWarn("patch schedule missed its window, disabled without running",
				"schedule_id", s.ID, "schedule_name", s.Name, "slot", slot)
			continue
		}
		if !due {
			continue
		}
		// Dedupe: last_run_at at or after the slot means this slot already ran.
		// (Repeated authoritatively by the compare-and-set in runSchedule.)
		if s.LastRunAt.Valid && !s.LastRunAt.Time.Before(slot) {
			continue
		}
		// A slot older than the schedule's last config change never fires:
		// otherwise creating or enabling "Thursday 03:00" shortly after 03:00
		// would patch the group immediately instead of next week.
		if patchSlotPredatesConfig(s, slot) {
			continue
		}
		h.runSchedule(ctx, d, s, slot, now, tenantHost)
	}
}

// patchSlotPredatesConfig reports whether the slot lies before the schedule's
// last configuration change (create, edit or enable all bump updated_at;
// claiming a slot intentionally does not).
func patchSlotPredatesConfig(s db.PatchSchedule, slot time.Time) bool {
	return s.UpdatedAt.Valid && slot.Before(s.UpdatedAt.Time)
}

// patchScheduleSlot evaluates a schedule against now and returns the slot time
// (UTC), whether it is due, and - for one-shot schedules - whether the window
// was missed entirely. Identical timezone handling to the reboot dispatcher.
func patchScheduleSlot(s db.PatchSchedule, now time.Time) (slot time.Time, due bool, missed bool, err error) {
	switch s.ScheduleType {
	case "once":
		if !s.RunAt.Valid {
			return time.Time{}, false, false, fmt.Errorf("schedule type 'once' without run_at")
		}
		slot = s.RunAt.Time // stored as UTC
		if now.Before(slot) {
			return slot, false, false, nil
		}
		if now.Sub(slot) > patchScheduleTolerance {
			// Already-run slots are filtered by the caller's last_run_at dedupe
			// before execution; for missed marking, skip schedules whose slot
			// was consumed (re-listing race after run).
			if s.LastRunAt.Valid && !s.LastRunAt.Time.Before(slot) {
				return slot, false, false, nil
			}
			return slot, false, true, nil
		}
		return slot, true, false, nil
	case "weekly":
		if s.Weekday == nil || *s.Weekday < 0 || *s.Weekday > 6 {
			return time.Time{}, false, false, fmt.Errorf("schedule type 'weekly' without valid weekday")
		}
		if s.TimeOfDay == nil {
			return time.Time{}, false, false, fmt.Errorf("schedule type 'weekly' without time_of_day")
		}
		tod, perr := time.Parse("15:04", *s.TimeOfDay)
		if perr != nil {
			return time.Time{}, false, false, fmt.Errorf("invalid time_of_day %q: %w", *s.TimeOfDay, perr)
		}
		loc, lerr := time.LoadLocation(s.Timezone)
		if lerr != nil {
			return time.Time{}, false, false, fmt.Errorf("invalid timezone %q: %w", s.Timezone, lerr)
		}
		// Most recent occurrence of weekday+time_of_day in the schedule's
		// timezone that is not in the future. time.Date normalizes nonexistent
		// wall-clock times around DST transitions.
		nowLoc := now.In(loc)
		daysBack := (int(nowLoc.Weekday()) - int(*s.Weekday) + 7) % 7
		cand := time.Date(nowLoc.Year(), nowLoc.Month(), nowLoc.Day()-daysBack, tod.Hour(), tod.Minute(), 0, 0, loc)
		if cand.After(now) {
			cand = cand.AddDate(0, 0, -7)
		}
		slot = cand.UTC()
		// Weekly schedules are never marked missed: an overdue slot simply
		// stays silent until the next week's occurrence.
		return slot, now.Sub(slot) <= patchScheduleTolerance, false, nil
	case "daily":
		if s.TimeOfDay == nil {
			return time.Time{}, false, false, fmt.Errorf("schedule type 'daily' without time_of_day")
		}
		tod, perr := time.Parse("15:04", *s.TimeOfDay)
		if perr != nil {
			return time.Time{}, false, false, fmt.Errorf("invalid time_of_day %q: %w", *s.TimeOfDay, perr)
		}
		loc, lerr := time.LoadLocation(s.Timezone)
		if lerr != nil {
			return time.Time{}, false, false, fmt.Errorf("invalid timezone %q: %w", s.Timezone, lerr)
		}
		// Most recent daily occurrence of time_of_day in the schedule's
		// timezone that is not in the future. time.Date normalizes nonexistent
		// wall-clock times around DST transitions.
		nowLoc := now.In(loc)
		cand := time.Date(nowLoc.Year(), nowLoc.Month(), nowLoc.Day(), tod.Hour(), tod.Minute(), 0, 0, loc)
		if cand.After(now) {
			cand = cand.AddDate(0, 0, -1)
		}
		slot = cand.UTC()
		// Daily schedules are never marked missed: an overdue slot simply
		// stays silent until the next day's occurrence.
		return slot, now.Sub(slot) <= patchScheduleTolerance, false, nil
	default:
		return time.Time{}, false, false, fmt.Errorf("unknown schedule_type %q", s.ScheduleType)
	}
}

// runSchedule executes one due schedule. The slot is claimed first via an
// atomic compare-and-set so that at most one dispatcher (concurrent task or
// extra replica) ever runs a given slot. After the claim every refusal is final
// for this slot: a refused or failed run expires instead of retrying, matching
// the no-catch-up policy for missed schedules.
func (h *PatchSchedulesDispatchHandler) runSchedule(ctx context.Context, d *database.DB, s db.PatchSchedule, slot, now time.Time, tenantHost string) {
	if h.qc == nil {
		// Transient infra gap: leave the slot unclaimed so the next dispatch
		// within the tolerance window can retry.
		h.logError("patch schedule: no queue client available", "schedule_id", s.ID)
		return
	}

	// Claim the slot. The CAS on last_run_at also disables one-shot schedules,
	// so a claim can never leave them armed.
	if _, err := d.Queries.ConsumePatchScheduleSlot(ctx, db.ConsumePatchScheduleSlotParams{
		ID:        s.ID,
		LastRunAt: pgtime.From(now),
		Slot:      pgtime.From(slot),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Another dispatcher claimed this slot.
			return
		}
		// Real DB error: leave unclaimed, next dispatch retries in-window.
		h.logError("patch schedule: slot claim failed", "schedule_id", s.ID, "error", err)
		return
	}

	// refuse records a slot that was claimed but must not run. Best effort:
	// nothing has happened (or will happen) on this path.
	refuse := func(reason string) {
		h.logError("patch schedule refused: "+reason, "schedule_id", s.ID, "schedule_name", s.Name)
		if err := h.writeAudit(ctx, d, s, "patch_scheduled_run", false, map[string]interface{}{
			"schedule_id":   s.ID,
			"schedule_name": s.Name,
			"slot":          slot,
			"error":         reason,
		}); err != nil {
			h.logError("patch schedule: audit write failed", "schedule_id", s.ID, "error", err)
		}
	}

	// Revalidate the creator at execution time: can_manage_patching only gates
	// the HTTP routes, so a schedule must stop firing once its creator is
	// deleted, deactivated or loses the permission. Such schedules are disabled
	// outright, not just skipped.
	creatorAllowed := false
	if s.CreatedBy != nil {
		allowed, err := d.Queries.UserCanManagePatching(ctx, *s.CreatedBy)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			refuse("failed to revalidate schedule creator: " + err.Error())
			return
		}
		creatorAllowed = err == nil && allowed != nil && *allowed
	}
	if !creatorAllowed {
		refuse("schedule creator no longer exists, is inactive or lacks can_manage_patching; schedule disabled")
		if _, err := d.Queries.SetPatchScheduleEnabled(ctx, db.SetPatchScheduleEnabledParams{ID: s.ID, Enabled: false}); err != nil {
			h.logError("patch schedule: disable failed", "schedule_id", s.ID, "error", err)
		}
		return
	}

	hosts, err := d.Queries.ListPatchScheduleGroupHosts(ctx, s.HostGroupID)
	if err != nil {
		refuse("failed to resolve host group members: " + err.Error())
		return
	}

	type patchTarget struct {
		apiID    string
		hostID   string
		hostname string
	}
	targets := make([]patchTarget, 0, len(hosts))
	requested := []map[string]string{}
	for _, row := range hosts {
		hostname := row.FriendlyName
		if row.Hostname != nil && *row.Hostname != "" {
			hostname = *row.Hostname
		}
		targets = append(targets, patchTarget{apiID: row.ApiID, hostID: row.ID, hostname: hostname})
		requested = append(requested, map[string]string{"hostId": row.ID, "hostname": hostname})
	}

	// Audit the intent BEFORE enqueueing, attributed to the schedule's creator.
	// The slot is already claimed, so a failed audit expires the run.
	if err := h.writeAudit(ctx, d, s, "patch_scheduled_run", len(requested) > 0, map[string]interface{}{
		"schedule_id":   s.ID,
		"schedule_name": s.Name,
		"host_group_id": s.HostGroupID,
		"slot":          slot,
		"requested":     requested,
	}); err != nil {
		h.logError("patch schedule refused: audit log write failed", "schedule_id", s.ID, "error", err)
		return
	}

	enqueued := 0
	failed := []map[string]string{}
	for _, t := range targets {
		patchRunID := uuid.New().String()
		jobID := "patch-run-" + patchRunID
		snapshot, _ := json.Marshal(map[string]interface{}{
			"patch_delay_type": "scheduled",
			"schedule_id":      s.ID,
			"schedule_name":    s.Name,
		})
		// Create the patch_run row first (status "queued"), mirroring the
		// manual trigger path; the worker updates it as the agent reports.
		if err := d.Queries.CreatePatchRun(ctx, db.CreatePatchRunParams{
			ID:                patchRunID,
			HostID:            t.hostID,
			JobID:             jobID,
			PatchType:         scheduledPatchType,
			Status:            "queued",
			ShellOutput:       "",
			TriggeredByUserID: s.CreatedBy,
			DryRun:            false,
			ScheduledAt:       pgtime.From(slot),
			PolicySnapshot:    snapshot,
		}); err != nil {
			failed = append(failed, map[string]string{"hostId": t.hostID, "hostname": t.hostname, "reason": "Failed to create patch run"})
			continue
		}

		task, err := NewRunPatchTask(RunPatchPayload{
			HostID:     t.hostID,
			Host:       tenantHost,
			ApiID:      t.apiID,
			PatchRunID: patchRunID,
			PatchType:  scheduledPatchType,
			DryRun:     false,
		})
		if err != nil {
			failed = append(failed, map[string]string{"hostId": t.hostID, "hostname": t.hostname, "reason": "Failed to create patch task"})
			continue
		}
		if _, err := h.qc.Enqueue(task); err != nil {
			reason := "Failed to enqueue patch task"
			if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
				reason = "Patch already pending for this host"
			}
			failed = append(failed, map[string]string{"hostId": t.hostID, "hostname": t.hostname, "reason": reason})
			continue
		}
		enqueued++
	}

	// Record the effective outcome. Best effort: the action already happened.
	if err := h.writeAudit(ctx, d, s, "patch_scheduled_enqueued", enqueued > 0, map[string]interface{}{
		"schedule_id":   s.ID,
		"schedule_name": s.Name,
		"enqueued":      enqueued,
		"failed":        failed,
	}); err != nil {
		h.logError("patch schedule: result audit write failed", "schedule_id", s.ID, "error", err)
	}

	h.logInfo("patch schedule executed",
		"schedule_id", s.ID, "schedule_name", s.Name, "slot", slot,
		"enqueued", enqueued, "failed", len(failed))
}

// writeAudit inserts an audit_logs entry attributed to the schedule's creator.
// Scheduled runs have no HTTP request context, so IP, user agent and request ID
// stay empty.
func (h *PatchSchedulesDispatchHandler) writeAudit(ctx context.Context, d *database.DB, s db.PatchSchedule, event string, success bool, detail map[string]interface{}) error {
	var details *string
	if b, err := json.Marshal(detail); err == nil {
		str := string(b)
		details = &str
	}
	return d.Queries.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ID:      uuid.New().String(),
		Event:   event,
		UserID:  s.CreatedBy,
		Details: details,
		Success: success,
	})
}

func (h *PatchSchedulesDispatchHandler) logError(msg string, args ...any) {
	if h.log != nil {
		h.log.Error(msg, args...)
	}
}

func (h *PatchSchedulesDispatchHandler) logWarn(msg string, args ...any) {
	if h.log != nil {
		h.log.Warn(msg, args...)
	}
}

func (h *PatchSchedulesDispatchHandler) logInfo(msg string, args ...any) {
	if h.log != nil {
		h.log.Info(msg, args...)
	}
}
