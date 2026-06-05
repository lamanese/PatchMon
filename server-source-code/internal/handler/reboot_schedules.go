package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/middleware"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// RebootSchedulesHandler manages reboot schedules: scheduled remote reboots
// scoped to one host group. All routes sit behind can_reboot_hosts. The
// schedules only gate WHEN a reboot fires; the execution-time safety layers
// (allow_reboot allowlist, server self-exclusion, audit) live in the queue
// dispatcher.
type RebootSchedulesHandler struct {
	db database.DBProvider
}

// NewRebootSchedulesHandler creates the handler.
func NewRebootSchedulesHandler(db database.DBProvider) *RebootSchedulesHandler {
	return &RebootSchedulesHandler{db: db}
}

func (h *RebootSchedulesHandler) q(r *http.Request) *db.Queries {
	return h.db.DB(r.Context()).Queries
}

const maxRebootScheduleNameLen = 200

// rebootScheduleRequest is the create/update payload. Pointer fields
// distinguish "absent" from zero values so updates can be partial.
type rebootScheduleRequest struct {
	Name           *string `json:"name"`
	HostGroupID    *string `json:"host_group_id"`
	ScheduleType   *string `json:"schedule_type"`
	RunAt          *string `json:"run_at"` // RFC3339, for 'once'
	Weekday        *int32  `json:"weekday"`
	TimeOfDay      *string `json:"time_of_day"` // 'HH:MM', for 'weekly'
	Timezone       *string `json:"timezone"`
	OnlyIfRequired *bool   `json:"only_if_required"`
	Enabled        *bool   `json:"enabled"`
}

// validateRebootSchedule checks the merged schedule fields. requireFutureRun
// is only enforced for enabled one-shot schedules: an operator must be able
// to disable (or look at) an expired schedule without being forced to move
// its run_at first.
func validateRebootSchedule(s *db.RebootSchedule, now time.Time) error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return fmt.Errorf("name required")
	}
	if len(s.Name) > maxRebootScheduleNameLen {
		return fmt.Errorf("name too long (max %d characters)", maxRebootScheduleNameLen)
	}
	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("invalid timezone %q", s.Timezone)
	}
	switch s.ScheduleType {
	case "once":
		if !s.RunAt.Valid {
			return fmt.Errorf("run_at required for schedule_type 'once'")
		}
		if s.Enabled && !s.RunAt.Time.After(now) {
			return fmt.Errorf("run_at must be in the future")
		}
		s.Weekday = nil
		s.TimeOfDay = nil
	case "weekly":
		if s.Weekday == nil || *s.Weekday < 0 || *s.Weekday > 6 {
			return fmt.Errorf("weekday (0-6) required for schedule_type 'weekly'")
		}
		if s.TimeOfDay == nil {
			return fmt.Errorf("time_of_day required for schedule_type 'weekly'")
		}
		if _, err := time.Parse("15:04", *s.TimeOfDay); err != nil {
			return fmt.Errorf("invalid time_of_day %q (expected HH:MM)", *s.TimeOfDay)
		}
		s.RunAt = pgtype.Timestamp{}
	default:
		return fmt.Errorf("schedule_type must be 'once' or 'weekly'")
	}
	return nil
}

// applyRebootScheduleRequest merges the request into the schedule.
func applyRebootScheduleRequest(s *db.RebootSchedule, req *rebootScheduleRequest) error {
	if req.Name != nil {
		s.Name = *req.Name
	}
	if req.HostGroupID != nil {
		s.HostGroupID = *req.HostGroupID
	}
	if req.ScheduleType != nil {
		s.ScheduleType = *req.ScheduleType
	}
	if req.RunAt != nil {
		if *req.RunAt == "" {
			s.RunAt = pgtype.Timestamp{}
		} else {
			t, err := time.Parse(time.RFC3339, *req.RunAt)
			if err != nil {
				return fmt.Errorf("invalid run_at (expected RFC3339)")
			}
			s.RunAt = pgtime.From(t)
		}
	}
	if req.Weekday != nil {
		s.Weekday = req.Weekday
	}
	if req.TimeOfDay != nil {
		s.TimeOfDay = req.TimeOfDay
	}
	if req.Timezone != nil {
		s.Timezone = *req.Timezone
	}
	if req.OnlyIfRequired != nil {
		s.OnlyIfRequired = *req.OnlyIfRequired
	}
	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}
	return nil
}

func rebootScheduleToMap(row db.RebootSchedule, hostGroupName string) map[string]interface{} {
	return map[string]interface{}{
		"id":               row.ID,
		"name":             row.Name,
		"host_group_id":    row.HostGroupID,
		"host_group_name":  hostGroupName,
		"schedule_type":    row.ScheduleType,
		"run_at":           pgTime(row.RunAt),
		"weekday":          row.Weekday,
		"time_of_day":      row.TimeOfDay,
		"timezone":         row.Timezone,
		"only_if_required": row.OnlyIfRequired,
		"enabled":          row.Enabled,
		"last_run_at":      pgTime(row.LastRunAt),
		"missed_at":        pgTime(row.MissedAt),
		"created_at":       pgTime(row.CreatedAt),
		"updated_at":       pgTime(row.UpdatedAt),
	}
}

// List handles GET /reboot-schedules.
func (h *RebootSchedulesHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.q(r).ListRebootSchedules(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to list reboot schedules")
		return
	}
	out := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		out[i] = rebootScheduleToMap(db.RebootSchedule{
			ID:             row.ID,
			Name:           row.Name,
			HostGroupID:    row.HostGroupID,
			ScheduleType:   row.ScheduleType,
			RunAt:          row.RunAt,
			Weekday:        row.Weekday,
			TimeOfDay:      row.TimeOfDay,
			Timezone:       row.Timezone,
			OnlyIfRequired: row.OnlyIfRequired,
			Enabled:        row.Enabled,
			LastRunAt:      row.LastRunAt,
			MissedAt:       row.MissedAt,
			CreatedBy:      row.CreatedBy,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		}, row.HostGroupName)
	}
	JSON(w, http.StatusOK, out)
}

// Create handles POST /reboot-schedules.
func (h *RebootSchedulesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req rebootScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	// Defaults for absent fields, matching the table defaults.
	s := db.RebootSchedule{Timezone: "UTC", OnlyIfRequired: true, Enabled: true}
	if err := applyRebootScheduleRequest(&s, &req); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateRebootSchedule(&s, time.Now()); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	group, err := h.q(r).GetHostGroupByID(r.Context(), s.HostGroupID)
	if err != nil {
		Error(w, http.StatusBadRequest, "Host group not found")
		return
	}
	var createdBy *string
	if uid, _ := r.Context().Value(middleware.UserIDKey).(string); uid != "" {
		createdBy = &uid
	}
	row, err := h.q(r).CreateRebootSchedule(r.Context(), db.CreateRebootScheduleParams{
		ID:             uuid.New().String(),
		Name:           s.Name,
		HostGroupID:    s.HostGroupID,
		ScheduleType:   s.ScheduleType,
		RunAt:          s.RunAt,
		Weekday:        s.Weekday,
		TimeOfDay:      s.TimeOfDay,
		Timezone:       s.Timezone,
		OnlyIfRequired: s.OnlyIfRequired,
		Enabled:        s.Enabled,
		CreatedBy:      createdBy,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to create reboot schedule")
		return
	}
	h.audit(r, "reboot_schedule_created", row)
	JSON(w, http.StatusCreated, rebootScheduleToMap(row, group.Name))
}

// Update handles PUT /reboot-schedules/{id}. Partial: absent fields keep
// their stored values. Editing clears a missed_at marker.
func (h *RebootSchedulesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req rebootScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s, err := h.q(r).GetRebootScheduleByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "Reboot schedule not found")
		return
	}
	if err := applyRebootScheduleRequest(&s, &req); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateRebootSchedule(&s, time.Now()); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	group, err := h.q(r).GetHostGroupByID(r.Context(), s.HostGroupID)
	if err != nil {
		Error(w, http.StatusBadRequest, "Host group not found")
		return
	}
	row, err := h.q(r).UpdateRebootSchedule(r.Context(), db.UpdateRebootScheduleParams{
		ID:             id,
		Name:           s.Name,
		HostGroupID:    s.HostGroupID,
		ScheduleType:   s.ScheduleType,
		RunAt:          s.RunAt,
		Weekday:        s.Weekday,
		TimeOfDay:      s.TimeOfDay,
		Timezone:       s.Timezone,
		OnlyIfRequired: s.OnlyIfRequired,
		Enabled:        s.Enabled,
		MissedAt:       pgtype.Timestamp{}, // editing acknowledges a missed run
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update reboot schedule")
		return
	}
	h.audit(r, "reboot_schedule_updated", row)
	JSON(w, http.StatusOK, rebootScheduleToMap(row, group.Name))
}

// Delete handles DELETE /reboot-schedules/{id}.
func (h *RebootSchedulesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := h.q(r).GetRebootScheduleByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "Reboot schedule not found")
		return
	}
	if err := h.q(r).DeleteRebootSchedule(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to delete reboot schedule")
		return
	}
	h.audit(r, "reboot_schedule_deleted", s)
	JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// audit writes a best-effort audit_logs entry for schedule management.
// Schedule changes arm future reboots, so they are recorded like the
// allowlist changes; unlike execution they are not fail-closed because the
// change itself is not destructive.
func (h *RebootSchedulesHandler) audit(r *http.Request, event string, s db.RebootSchedule) {
	ctx := r.Context()
	d := h.db.DB(ctx)
	if d == nil {
		return
	}
	var userID *string
	if uid, _ := ctx.Value(middleware.UserIDKey).(string); uid != "" {
		userID = &uid
	}
	var requestID *string
	if rid, _ := ctx.Value(middleware.RequestIDKey).(string); rid != "" {
		requestID = &rid
	}
	ip := clientIPFromRequest(r)
	ua := r.UserAgent()
	var details *string
	if b, err := json.Marshal(map[string]interface{}{
		"schedule_id":      s.ID,
		"schedule_name":    s.Name,
		"host_group_id":    s.HostGroupID,
		"schedule_type":    s.ScheduleType,
		"run_at":           pgTime(s.RunAt),
		"weekday":          s.Weekday,
		"time_of_day":      s.TimeOfDay,
		"timezone":         s.Timezone,
		"only_if_required": s.OnlyIfRequired,
		"enabled":          s.Enabled,
	}); err == nil {
		str := string(b)
		details = &str
	}
	_ = d.Queries.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ID:        uuid.New().String(),
		Event:     event,
		UserID:    userID,
		IpAddress: &ip,
		UserAgent: &ua,
		RequestID: requestID,
		Details:   details,
		Success:   true,
	})
}
