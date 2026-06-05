package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	hostctx "github.com/PatchMon/PatchMon/server-source-code/internal/context"
	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/PatchMon/PatchMon/server-source-code/internal/serverident"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// rebootScheduleTolerance is how far past its slot a schedule still fires.
// The dispatcher runs every minute, so this only needs to absorb short
// restarts or dispatch hiccups. Anything older is deliberately dropped:
// a server that was down for hours must not reboot fleets on catch-up.
// One-shot schedules that miss their window are disabled and marked missed.
const rebootScheduleTolerance = 5 * time.Minute

// RebootSchedulesDispatchHandler fires due reboot schedules. Execution per
// host goes through the same safety layers as the bulk reboot endpoint:
// server self-exclusion (fail closed when unconfigured), the allow_reboot
// allowlist (non-allowlisted hosts are skipped, not failed), an audit intent
// entry written before any task is enqueued (fail closed), and the per-host
// reboot task cooldown via TaskID + retention.
type RebootSchedulesDispatchHandler struct {
	defaultDB *database.DB
	poolCache *hostctx.PoolCache
	qc        *asynq.Client
	log       *slog.Logger
}

// NewRebootSchedulesDispatchHandler creates the handler.
func NewRebootSchedulesDispatchHandler(defaultDB *database.DB, poolCache *hostctx.PoolCache, qc *asynq.Client, log *slog.Logger) *RebootSchedulesDispatchHandler {
	return &RebootSchedulesDispatchHandler{defaultDB: defaultDB, poolCache: poolCache, qc: qc, log: log}
}

// ProcessTask implements asynq.Handler. Mirrors the scheduled-reports
// dispatcher: a payload pins one tenant DB, no payload means the default DB
// plus every cached tenant pool.
func (h *RebootSchedulesDispatchHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
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

func (h *RebootSchedulesDispatchHandler) processDB(ctx context.Context, d *database.DB, tenantHost string) {
	now := time.Now()
	schedules, err := d.Queries.ListEnabledRebootSchedules(ctx)
	if err != nil {
		h.logError("reboot_schedules_dispatch: list enabled", "error", err)
		return
	}
	for _, s := range schedules {
		slot, due, missed, err := rebootScheduleSlot(s, now)
		if err != nil {
			h.logError("reboot_schedules_dispatch: invalid schedule", "schedule_id", s.ID, "error", err)
			continue
		}
		if missed {
			// One-shot schedule whose window passed while the server was down.
			// Expire it instead of rebooting hours late.
			if err := d.Queries.MarkRebootScheduleMissed(ctx, db.MarkRebootScheduleMissedParams{
				ID:       s.ID,
				MissedAt: pgtime.From(slot),
			}); err != nil {
				h.logError("reboot_schedules_dispatch: mark missed", "schedule_id", s.ID, "error", err)
				continue
			}
			h.logWarn("reboot schedule missed its window, disabled without running",
				"schedule_id", s.ID, "schedule_name", s.Name, "slot", slot)
			continue
		}
		if !due {
			continue
		}
		// Dedupe: last_run_at at or after the slot means this slot already ran.
		if s.LastRunAt.Valid && !s.LastRunAt.Time.Before(slot) {
			continue
		}
		h.runSchedule(ctx, d, s, slot, now, tenantHost)
	}
}

// rebootScheduleSlot evaluates a schedule against now and returns the slot
// time (UTC), whether it is due, and - for one-shot schedules - whether the
// window was missed entirely.
func rebootScheduleSlot(s db.RebootSchedule, now time.Time) (slot time.Time, due bool, missed bool, err error) {
	switch s.ScheduleType {
	case "once":
		if !s.RunAt.Valid {
			return time.Time{}, false, false, fmt.Errorf("schedule type 'once' without run_at")
		}
		slot = s.RunAt.Time // stored as UTC
		if now.Before(slot) {
			return slot, false, false, nil
		}
		if now.Sub(slot) > rebootScheduleTolerance {
			// Already-run slots are filtered by the caller's last_run_at
			// dedupe before execution; for missed marking, skip schedules
			// whose slot was consumed (re-listing race after run).
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
		// timezone that is not in the future. time.Date normalizes
		// nonexistent wall-clock times around DST transitions.
		nowLoc := now.In(loc)
		daysBack := (int(nowLoc.Weekday()) - int(*s.Weekday) + 7) % 7
		cand := time.Date(nowLoc.Year(), nowLoc.Month(), nowLoc.Day()-daysBack, tod.Hour(), tod.Minute(), 0, 0, loc)
		if cand.After(now) {
			cand = cand.AddDate(0, 0, -7)
		}
		slot = cand.UTC()
		// Weekly schedules are never marked missed: an overdue slot simply
		// stays silent until the next week's occurrence.
		return slot, now.Sub(slot) <= rebootScheduleTolerance, false, nil
	default:
		return time.Time{}, false, false, fmt.Errorf("unknown schedule_type %q", s.ScheduleType)
	}
}

// runSchedule executes one due schedule: resolve the group's hosts, apply
// self-exclusion and the allow_reboot allowlist, audit, consume the slot and
// enqueue the per-host reboot tasks.
func (h *RebootSchedulesDispatchHandler) runSchedule(ctx context.Context, d *database.DB, s db.RebootSchedule, slot, now time.Time, tenantHost string) {
	if h.qc == nil {
		// Leave the slot unconsumed; without a queue client nothing can run.
		h.logError("reboot schedule: no queue client available", "schedule_id", s.ID)
		return
	}
	// Fail closed: without a known server machine identity the self-exclusion
	// check cannot work, so no scheduled reboot may run at all. The slot is
	// consumed so a config problem does not turn into a reboot storm once
	// fixed hours later.
	if len(serverident.MachineIDs()) == 0 {
		h.logError("reboot schedule refused: self-exclusion not configured (no DMI product UUID, no /run/host-machine-id mount, no PM_SERVER_MACHINE_ID)",
			"schedule_id", s.ID, "schedule_name", s.Name)
		if err := h.writeAudit(ctx, d, s, "host_reboot_scheduled_run", false, map[string]interface{}{
			"schedule_id":   s.ID,
			"schedule_name": s.Name,
			"slot":          slot,
			"error":         "self-exclusion is not configured",
		}); err != nil {
			h.logError("reboot schedule: audit write failed", "schedule_id", s.ID, "error", err)
			return
		}
		h.consumeSlot(ctx, d, s, now)
		return
	}

	hosts, err := d.Queries.ListRebootScheduleGroupHosts(ctx, s.HostGroupID)
	if err != nil {
		// Transient lookup failure: leave the slot unconsumed, the next
		// minute's dispatch retries within the tolerance window.
		h.logError("reboot schedule: list group hosts", "schedule_id", s.ID, "error", err)
		return
	}

	type rebootTarget struct {
		apiID    string
		hostID   string
		hostname string
	}
	targets := []rebootTarget{}
	requested := []map[string]string{}
	skipped := []map[string]string{}
	for _, row := range hosts {
		hostname := row.FriendlyName
		if row.Hostname != nil && *row.Hostname != "" {
			hostname = *row.Hostname
		}
		if serverident.IsSelf(row.MachineID) {
			skipped = append(skipped, map[string]string{"hostId": row.ID, "hostname": hostname, "reason": "PatchMon server host cannot be rebooted"})
			continue
		}
		if !row.AllowReboot {
			skipped = append(skipped, map[string]string{"hostId": row.ID, "hostname": hostname, "reason": "Reboot not allowed for this host (allow_reboot is not set)"})
			continue
		}
		targets = append(targets, rebootTarget{apiID: row.ApiID, hostID: row.ID, hostname: hostname})
		requested = append(requested, map[string]string{"hostId": row.ID, "hostname": hostname})
	}

	// Audit the intent BEFORE enqueueing, attributed to the schedule's
	// creator. Fail closed: a reboot that cannot be audited must not happen;
	// the slot stays unconsumed so the next dispatch retries.
	if err := h.writeAudit(ctx, d, s, "host_reboot_scheduled_run", len(requested) > 0, map[string]interface{}{
		"schedule_id":      s.ID,
		"schedule_name":    s.Name,
		"host_group_id":    s.HostGroupID,
		"slot":             slot,
		"only_if_required": s.OnlyIfRequired,
		"requested":        requested,
		"skipped":          skipped,
	}); err != nil {
		h.logError("reboot schedule refused: audit log write failed", "schedule_id", s.ID, "error", err)
		return
	}

	// Consume the slot before enqueueing to narrow the double-dispatch
	// window; the per-host TaskID cooldown in NewRebootTask closes the rest.
	h.consumeSlot(ctx, d, s, now)

	enqueued := 0
	failed := []map[string]string{}
	for _, t := range targets {
		task, err := NewRebootTask(RebootPayload{
			ApiID:          t.apiID,
			Host:           tenantHost,
			OnlyIfRequired: s.OnlyIfRequired,
		})
		if err != nil {
			failed = append(failed, map[string]string{"hostId": t.hostID, "hostname": t.hostname, "reason": "Failed to create reboot task"})
			continue
		}
		if _, err := h.qc.Enqueue(task); err != nil {
			reason := "Failed to enqueue reboot task"
			if err == asynq.ErrDuplicateTask || err == asynq.ErrTaskIDConflict {
				reason = "Reboot already pending for this host (cooldown)"
			}
			failed = append(failed, map[string]string{"hostId": t.hostID, "hostname": t.hostname, "reason": reason})
			continue
		}
		enqueued++
	}

	// Record the effective outcome, mirroring the bulk endpoint's
	// intent/result audit pair. Best effort: the action already happened.
	if err := h.writeAudit(ctx, d, s, "host_reboot_scheduled_enqueued", enqueued > 0, map[string]interface{}{
		"schedule_id":   s.ID,
		"schedule_name": s.Name,
		"enqueued":      enqueued,
		"failed":        failed,
	}); err != nil {
		h.logError("reboot schedule: result audit write failed", "schedule_id", s.ID, "error", err)
	}

	h.logInfo("reboot schedule executed",
		"schedule_id", s.ID, "schedule_name", s.Name, "slot", slot,
		"enqueued", enqueued, "skipped", len(skipped), "failed", len(failed))
}

// consumeSlot records the run and disables one-shot schedules.
func (h *RebootSchedulesDispatchHandler) consumeSlot(ctx context.Context, d *database.DB, s db.RebootSchedule, now time.Time) {
	if err := d.Queries.UpdateRebootScheduleLastRun(ctx, db.UpdateRebootScheduleLastRunParams{
		ID:        s.ID,
		LastRunAt: pgtime.From(now),
	}); err != nil {
		h.logError("reboot schedule: update last_run_at failed", "schedule_id", s.ID, "error", err)
	}
	if s.ScheduleType == "once" {
		if _, err := d.Queries.SetRebootScheduleEnabled(ctx, db.SetRebootScheduleEnabledParams{ID: s.ID, Enabled: false}); err != nil {
			h.logError("reboot schedule: disable one-shot failed", "schedule_id", s.ID, "error", err)
		}
	}
}

// writeAudit inserts an audit_logs entry attributed to the schedule's creator.
// Scheduled runs have no HTTP request context, so IP, user agent and request
// ID stay empty.
func (h *RebootSchedulesDispatchHandler) writeAudit(ctx context.Context, d *database.DB, s db.RebootSchedule, event string, success bool, detail map[string]interface{}) error {
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

func (h *RebootSchedulesDispatchHandler) logError(msg string, args ...any) {
	if h.log != nil {
		h.log.Error(msg, args...)
	}
}

func (h *RebootSchedulesDispatchHandler) logWarn(msg string, args ...any) {
	if h.log != nil {
		h.log.Warn(msg, args...)
	}
}

func (h *RebootSchedulesDispatchHandler) logInfo(msg string, args ...any) {
	if h.log != nil {
		h.log.Info(msg, args...)
	}
}
