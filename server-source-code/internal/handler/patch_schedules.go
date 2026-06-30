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

// PatchSchedulesHandler manages patch schedules: scheduled patch runs (patch
// all) scoped to one host group. All routes sit behind can_manage_patching and
// the patching module. The schedules only gate WHEN a patch run fires; the
// execution-time creator revalidation and audit live in the queue dispatcher.
type PatchSchedulesHandler struct {
	db database.DBProvider
}

// NewPatchSchedulesHandler creates the handler.
func NewPatchSchedulesHandler(db database.DBProvider) *PatchSchedulesHandler {
	return &PatchSchedulesHandler{db: db}
}

func (h *PatchSchedulesHandler) q(r *http.Request) *db.Queries {
	return h.db.DB(r.Context()).Queries
}

const maxPatchScheduleNameLen = 200

// patchScheduleRequest is the create/update payload. Pointer fields distinguish
// "absent" from zero values so updates can be partial.
type patchScheduleRequest struct {
	Name         *string `json:"name"`
	HostGroupID  *string `json:"host_group_id"`
	ScheduleType *string `json:"schedule_type"`
	RunAt        *string `json:"run_at"` // RFC3339, for 'once'
	Weekday      *int32  `json:"weekday"`
	TimeOfDay    *string `json:"time_of_day"` // 'HH:MM', for 'weekly'
	Timezone     *string `json:"timezone"`
	Enabled      *bool   `json:"enabled"`
}

// validatePatchSchedule checks the merged schedule fields. A future run_at is
// only enforced for enabled one-shot schedules: an operator must be able to
// disable (or look at) an expired schedule without being forced to move its
// run_at first.
func validatePatchSchedule(s *db.PatchSchedule, now time.Time) error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return fmt.Errorf("name required")
	}
	if len(s.Name) > maxPatchScheduleNameLen {
		return fmt.Errorf("name too long (max %d characters)", maxPatchScheduleNameLen)
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

// applyPatchScheduleRequest merges the request into the schedule.
func applyPatchScheduleRequest(s *db.PatchSchedule, req *patchScheduleRequest) error {
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
	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}
	return nil
}

func patchScheduleToMap(row db.PatchSchedule, hostGroupName string) map[string]interface{} {
	return map[string]interface{}{
		"id":              row.ID,
		"name":            row.Name,
		"host_group_id":   row.HostGroupID,
		"host_group_name": hostGroupName,
		"schedule_type":   row.ScheduleType,
		"run_at":          pgTime(row.RunAt),
		"weekday":         row.Weekday,
		"time_of_day":     row.TimeOfDay,
		"timezone":        row.Timezone,
		"enabled":         row.Enabled,
		"last_run_at":     pgTime(row.LastRunAt),
		"missed_at":       pgTime(row.MissedAt),
		"created_at":      pgTime(row.CreatedAt),
		"updated_at":      pgTime(row.UpdatedAt),
	}
}

// List handles GET /patch-schedules.
func (h *PatchSchedulesHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.q(r).ListPatchSchedules(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to list patch schedules")
		return
	}
	out := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		out[i] = patchScheduleToMap(db.PatchSchedule{
			ID:           row.ID,
			Name:         row.Name,
			HostGroupID:  row.HostGroupID,
			ScheduleType: row.ScheduleType,
			RunAt:        row.RunAt,
			Weekday:      row.Weekday,
			TimeOfDay:    row.TimeOfDay,
			Timezone:     row.Timezone,
			Enabled:      row.Enabled,
			LastRunAt:    row.LastRunAt,
			MissedAt:     row.MissedAt,
			CreatedBy:    row.CreatedBy,
			CreatedAt:    row.CreatedAt,
			UpdatedAt:    row.UpdatedAt,
		}, row.HostGroupName)
	}
	JSON(w, http.StatusOK, out)
}

// Create handles POST /patch-schedules.
func (h *PatchSchedulesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req patchScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	// Defaults for absent fields, matching the table defaults.
	s := db.PatchSchedule{Timezone: "UTC", Enabled: true}
	if err := applyPatchScheduleRequest(&s, &req); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePatchSchedule(&s, time.Now()); err != nil {
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
	row, err := h.q(r).CreatePatchSchedule(r.Context(), db.CreatePatchScheduleParams{
		ID:           uuid.New().String(),
		Name:         s.Name,
		HostGroupID:  s.HostGroupID,
		ScheduleType: s.ScheduleType,
		RunAt:        s.RunAt,
		Weekday:      s.Weekday,
		TimeOfDay:    s.TimeOfDay,
		Timezone:     s.Timezone,
		Enabled:      s.Enabled,
		CreatedBy:    createdBy,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to create patch schedule")
		return
	}
	h.audit(r, "patch_schedule_created", row)
	JSON(w, http.StatusCreated, patchScheduleToMap(row, group.Name))
}

// Update handles PUT /patch-schedules/{id}. Partial: absent fields keep their
// stored values. Editing clears a missed_at marker.
func (h *PatchSchedulesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req patchScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s, err := h.q(r).GetPatchScheduleByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "Patch schedule not found")
		return
	}
	if err := applyPatchScheduleRequest(&s, &req); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePatchSchedule(&s, time.Now()); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	group, err := h.q(r).GetHostGroupByID(r.Context(), s.HostGroupID)
	if err != nil {
		Error(w, http.StatusBadRequest, "Host group not found")
		return
	}
	row, err := h.q(r).UpdatePatchSchedule(r.Context(), db.UpdatePatchScheduleParams{
		ID:           id,
		Name:         s.Name,
		HostGroupID:  s.HostGroupID,
		ScheduleType: s.ScheduleType,
		RunAt:        s.RunAt,
		Weekday:      s.Weekday,
		TimeOfDay:    s.TimeOfDay,
		Timezone:     s.Timezone,
		Enabled:      s.Enabled,
		MissedAt:     pgtype.Timestamp{}, // editing acknowledges a missed run
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update patch schedule")
		return
	}
	h.audit(r, "patch_schedule_updated", row)
	JSON(w, http.StatusOK, patchScheduleToMap(row, group.Name))
}

// Delete handles DELETE /patch-schedules/{id}.
func (h *PatchSchedulesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := h.q(r).GetPatchScheduleByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "Patch schedule not found")
		return
	}
	if err := h.q(r).DeletePatchSchedule(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to delete patch schedule")
		return
	}
	h.audit(r, "patch_schedule_deleted", s)
	JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// audit writes a best-effort audit_logs entry for schedule management. Schedule
// changes arm future patch runs, so they are recorded; unlike execution they
// are not fail-closed because the change itself is not destructive.
func (h *PatchSchedulesHandler) audit(r *http.Request, event string, s db.PatchSchedule) {
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
		"schedule_id":   s.ID,
		"schedule_name": s.Name,
		"host_group_id": s.HostGroupID,
		"schedule_type": s.ScheduleType,
		"run_at":        pgTime(s.RunAt),
		"weekday":       s.Weekday,
		"time_of_day":   s.TimeOfDay,
		"timezone":      s.Timezone,
		"enabled":       s.Enabled,
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
