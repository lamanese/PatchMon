package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/branding"
	"github.com/PatchMon/PatchMon/server-source-code/internal/config"
	hostctx "github.com/PatchMon/PatchMon/server-source-code/internal/context"
	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/notifications"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/PatchMon/PatchMon/server-source-code/internal/queue"
	"github.com/PatchMon/PatchMon/server-source-code/internal/reports"
	"github.com/PatchMon/PatchMon/server-source-code/internal/store"
	"github.com/PatchMon/PatchMon/server-source-code/internal/util"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// NotificationsHandler manages notification destinations, routes, logs, and scheduled reports.
type NotificationsHandler struct {
	db       database.DBProvider
	enc      *util.Encryption
	emit     *notifications.Emitter
	resolved *config.ResolvedConfig
	cfg      *config.Config
	settings *store.SettingsStore
	qc       *asynq.Client
}

// NewNotificationsHandler creates the handler.
func NewNotificationsHandler(db database.DBProvider, enc *util.Encryption, emit *notifications.Emitter, resolved *config.ResolvedConfig, cfg *config.Config, settings *store.SettingsStore, qc *asynq.Client) *NotificationsHandler {
	return &NotificationsHandler{db: db, enc: enc, emit: emit, resolved: resolved, cfg: cfg, settings: settings, qc: qc}
}

func (h *NotificationsHandler) q(ctx context.Context) *db.Queries {
	return h.db.DB(ctx).Queries
}

// timezoneForRequest resolves the timezone from the context's DB settings per-request,
// so each context gets its own timezone in multi-context mode. Falls back to the
// startup-resolved value (single-context) or "UTC".
func (h *NotificationsHandler) timezoneForRequest(ctx context.Context) string {
	if h.settings != nil {
		if s, err := h.settings.GetFirst(ctx); err == nil {
			return config.ResolveTimezone(s.Timezone, h.cfg)
		}
	}
	if h.resolved != nil && h.resolved.Timezone != "" {
		return h.resolved.Timezone
	}
	return "UTC"
}

// ListDestinations GET /notifications/destinations
func (h *NotificationsHandler) ListDestinations(w http.ResponseWriter, r *http.Request) {
	rows, err := h.q(r.Context()).ListNotificationDestinations(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to list destinations")
		return
	}
	out := make([]map[string]interface{}, len(rows))
	for i, d := range rows {
		out[i] = map[string]interface{}{
			"id":           d.ID,
			"channel_type": d.ChannelType,
			"display_name": d.DisplayName,
			"enabled":      d.Enabled,
			"has_secret":   d.ConfigEncrypted != "",
			"created_at":   pgTime(d.CreatedAt),
			"updated_at":   pgTime(d.UpdatedAt),
		}
	}
	JSON(w, http.StatusOK, out)
}

// CreateDestination POST /notifications/destinations
func (h *NotificationsHandler) CreateDestination(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelType string                 `json:"channel_type"`
		DisplayName string                 `json:"display_name"`
		Config      map[string]interface{} `json:"config"`
		Enabled     *bool                  `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ChannelType == "" || req.DisplayName == "" {
		Error(w, http.StatusBadRequest, "channel_type, display_name, and config required")
		return
	}
	cfgJSON, _ := json.Marshal(req.Config)
	encStr := string(cfgJSON)
	if h.enc != nil {
		if e, err := h.enc.Encrypt(encStr); err == nil {
			encStr = e
		}
	}
	en := true
	if req.Enabled != nil {
		en = *req.Enabled
	}
	row, err := h.q(r.Context()).CreateNotificationDestination(r.Context(), db.CreateNotificationDestinationParams{
		ID:              uuid.New().String(),
		ChannelType:     req.ChannelType,
		DisplayName:     req.DisplayName,
		ConfigEncrypted: encStr,
		Enabled:         en,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to create destination")
		return
	}
	JSON(w, http.StatusCreated, map[string]interface{}{
		"id": row.ID, "channel_type": row.ChannelType, "display_name": row.DisplayName, "enabled": row.Enabled,
	})
}

// UpdateDestination PUT /notifications/destinations/{id}
func (h *NotificationsHandler) UpdateDestination(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		Error(w, http.StatusBadRequest, "id required")
		return
	}
	var req struct {
		DisplayName string                 `json:"display_name"`
		Config      map[string]interface{} `json:"config"`
		Enabled     *bool                  `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	existing, err := h.q(r.Context()).GetNotificationDestinationByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "Not found")
		return
	}
	encStr := existing.ConfigEncrypted
	if req.Config != nil {
		b, _ := json.Marshal(req.Config)
		encStr = string(b)
		if h.enc != nil {
			if e, err := h.enc.Encrypt(encStr); err == nil {
				encStr = e
			}
		}
	}
	en := existing.Enabled
	if req.Enabled != nil {
		en = *req.Enabled
	}
	dn := existing.DisplayName
	if req.DisplayName != "" {
		dn = req.DisplayName
	}
	row, err := h.q(r.Context()).UpdateNotificationDestination(r.Context(), db.UpdateNotificationDestinationParams{
		ID:              id,
		DisplayName:     dn,
		ConfigEncrypted: encStr,
		Enabled:         en,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update")
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"id": row.ID, "display_name": row.DisplayName, "enabled": row.Enabled})
}

// GetDestinationConfig GET /notifications/destinations/{id}/config
// Returns the decrypted config for editing. Protected by can_manage_notifications.
func (h *NotificationsHandler) GetDestinationConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		Error(w, http.StatusBadRequest, "id required")
		return
	}
	dest, err := h.q(r.Context()).GetNotificationDestinationByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "Not found")
		return
	}
	plain := "{}"
	if dest.ConfigEncrypted != "" && h.enc != nil {
		if d, err := h.enc.Decrypt(dest.ConfigEncrypted); err == nil {
			plain = d
		}
	} else if dest.ConfigEncrypted != "" {
		plain = dest.ConfigEncrypted
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		cfg = map[string]interface{}{}
	}
	JSON(w, http.StatusOK, cfg)
}

// DeleteDestination DELETE /notifications/destinations/{id}
func (h *NotificationsHandler) DeleteDestination(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "internal-alerts" {
		Error(w, http.StatusBadRequest, "The Internal Alerts destination cannot be deleted. You can disable it instead.")
		return
	}
	if err := h.q(r.Context()).DeleteNotificationDestination(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to delete")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// ListRoutes GET /notifications/routes
func (h *NotificationsHandler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	rows, err := h.q(r.Context()).ListNotificationRoutes(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to list routes")
		return
	}
	out := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		out[i] = map[string]interface{}{
			"id":                       row.ID,
			"destination_id":           row.DestinationID,
			"event_types":              jsonOrEmpty(row.EventTypes),
			"min_severity":             row.MinSeverity,
			"host_group_ids":           jsonOrEmpty(row.HostGroupIds),
			"host_ids":                 jsonOrEmpty(row.HostIds),
			"match_rules":              json.RawMessage(row.MatchRules),
			"enabled":                  row.RouteEnabled,
			"channel_type":             row.ChannelType,
			"destination_display_name": row.DestinationDisplayName,
			"created_at":               pgTime(row.CreatedAt),
			"updated_at":               pgTime(row.UpdatedAt),
		}
	}
	JSON(w, http.StatusOK, out)
}

func jsonOrEmpty(b []byte) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return json.RawMessage("[]")
	}
	return json.RawMessage(b)
}

// CreateRoute POST /notifications/routes
func (h *NotificationsHandler) CreateRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DestinationID string                 `json:"destination_id"`
		EventTypes    []string               `json:"event_types"`
		MinSeverity   string                 `json:"min_severity"`
		HostGroupIDs  []string               `json:"host_group_ids"`
		HostIDs       []string               `json:"host_ids"`
		MatchRules    map[string]interface{} `json:"match_rules"`
		Enabled       *bool                  `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || req.DestinationID == "" {
		Error(w, http.StatusBadRequest, "destination_id required")
		return
	}
	if len(req.EventTypes) == 0 {
		req.EventTypes = []string{"*"}
	}
	ms := req.MinSeverity
	if ms == "" {
		ms = "informational"
	}
	eventTypes, _ := json.Marshal(req.EventTypes)
	hostGroupIDs, _ := json.Marshal(req.HostGroupIDs)
	if hostGroupIDs == nil {
		hostGroupIDs = []byte("[]")
	}
	hostIDs, _ := json.Marshal(req.HostIDs)
	if hostIDs == nil {
		hostIDs = []byte("[]")
	}
	var rules []byte
	if req.MatchRules != nil {
		rules, _ = json.Marshal(req.MatchRules)
	}
	en := true
	if req.Enabled != nil {
		en = *req.Enabled
	}
	row, err := h.q(r.Context()).CreateNotificationRoute(r.Context(), db.CreateNotificationRouteParams{
		ID:            uuid.New().String(),
		DestinationID: req.DestinationID,
		EventTypes:    eventTypes,
		MinSeverity:   ms,
		HostGroupIds:  hostGroupIDs,
		HostIds:       hostIDs,
		MatchRules:    rules,
		Enabled:       en,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to create route")
		return
	}
	JSON(w, http.StatusCreated, map[string]interface{}{"id": row.ID})
}

// UpdateRoute PUT /notifications/routes/{id}
func (h *NotificationsHandler) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		DestinationID string                 `json:"destination_id"`
		EventTypes    []string               `json:"event_types"`
		MinSeverity   string                 `json:"min_severity"`
		HostGroupIDs  []string               `json:"host_group_ids"`
		HostIDs       []string               `json:"host_ids"`
		MatchRules    map[string]interface{} `json:"match_rules"`
		Enabled       *bool                  `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	existing, err := h.q(r.Context()).GetNotificationRouteByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "Not found")
		return
	}
	did := existing.DestinationID
	if req.DestinationID != "" {
		did = req.DestinationID
	}
	ms := existing.MinSeverity
	if req.MinSeverity != "" {
		ms = req.MinSeverity
	}
	eventTypes := existing.EventTypes
	if req.EventTypes != nil {
		eventTypes, _ = json.Marshal(req.EventTypes)
	}
	hostGroupIDs := existing.HostGroupIds
	if req.HostGroupIDs != nil {
		hostGroupIDs, _ = json.Marshal(req.HostGroupIDs)
	}
	hostIDs := existing.HostIds
	if req.HostIDs != nil {
		hostIDs, _ = json.Marshal(req.HostIDs)
	}
	rules := existing.MatchRules
	if req.MatchRules != nil {
		rules, _ = json.Marshal(req.MatchRules)
	}
	en := existing.Enabled
	if req.Enabled != nil {
		en = *req.Enabled
	}
	row, err := h.q(r.Context()).UpdateNotificationRoute(r.Context(), db.UpdateNotificationRouteParams{
		ID:            id,
		DestinationID: did,
		EventTypes:    eventTypes,
		MinSeverity:   ms,
		HostGroupIds:  hostGroupIDs,
		HostIds:       hostIDs,
		MatchRules:    rules,
		Enabled:       en,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update route")
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"id": row.ID})
}

// DeleteRoute DELETE /notifications/routes/{id}
func (h *NotificationsHandler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.q(r.Context()).DeleteNotificationRoute(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to delete")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// ListDeliveryLog GET /notifications/delivery-log
func (h *NotificationsHandler) ListDeliveryLog(w http.ResponseWriter, r *http.Request) {
	limit := int32(50)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil && n > 0 && n <= 500 {
			limit = int32(n)
		}
	}
	offset := int32(0)
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil && n >= 0 {
			offset = int32(n)
		}
	}
	rows, err := h.q(r.Context()).ListNotificationDeliveryLog(r.Context(), db.ListNotificationDeliveryLogParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to list log")
		return
	}
	out := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		out[i] = map[string]interface{}{
			"id":                  row.ID,
			"event_fingerprint":   row.EventFingerprint,
			"reference_type":      row.ReferenceType,
			"reference_id":        row.ReferenceID,
			"destination_id":      row.DestinationID,
			"event_type":          row.EventType,
			"status":              row.Status,
			"error_message":       row.ErrorMessage,
			"attempt_count":       row.AttemptCount,
			"provider_message_id": row.ProviderMessageID,
			"created_at":          pgTime(row.CreatedAt),
			"updated_at":          pgTime(row.UpdatedAt),
		}
	}
	JSON(w, http.StatusOK, out)
}

// TestDestination POST /notifications/test
func (h *NotificationsHandler) TestDestination(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DestinationID string `json:"destination_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.DestinationID == "" {
		Error(w, http.StatusBadRequest, "destination_id required")
		return
	}
	d := h.db.DB(r.Context())
	if d == nil {
		Error(w, http.StatusInternalServerError, "No database available")
		return
	}
	if h.emit == nil {
		Error(w, http.StatusServiceUnavailable, "Notifications not configured")
		return
	}
	th := hostctx.TenantHostKey(r.Context())
	err := h.emit.EnqueueToDestination(r.Context(), d, th, req.DestinationID, notifications.Event{
		Type:          "test",
		Severity:      "informational",
		Title:         branding.ProductNameShort + " test notification",
		Message:       "This is a test message from the " + branding.ProductNameShort + " notification settings.",
		ReferenceType: "test",
		ReferenceID:   uuid.New().String(),
		Metadata:      map[string]interface{}{"source": "manual_test"},
	})
	if err != nil {
		switch {
		case errors.Is(err, notifications.ErrDestinationNotFound):
			Error(w, http.StatusNotFound, "Destination not found")
		case errors.Is(err, notifications.ErrDestinationDisabled):
			Error(w, http.StatusBadRequest, "Destination is disabled")
		case errors.Is(err, notifications.ErrRateLimited):
			Error(w, http.StatusTooManyRequests, "Too many notifications; try again shortly")
		case errors.Is(err, notifications.ErrNotificationsDisabled):
			Error(w, http.StatusServiceUnavailable, "Notifications not configured")
		default:
			Error(w, http.StatusInternalServerError, "Failed to enqueue test")
		}
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "enqueued"})
}

func (h *NotificationsHandler) scheduledReportToMap(row db.ScheduledReport) map[string]interface{} {
	var def interface{}
	if len(row.Definition) > 0 {
		_ = json.Unmarshal(row.Definition, &def)
	}
	if def == nil {
		def = map[string]interface{}{}
	}
	var destIDs interface{}
	if len(row.DestinationIds) > 0 {
		_ = json.Unmarshal(row.DestinationIds, &destIDs)
	}
	if destIDs == nil {
		destIDs = []interface{}{}
	}
	return map[string]interface{}{
		"id":              row.ID,
		"name":            row.Name,
		"cron_expr":       row.CronExpr,
		"enabled":         row.Enabled,
		"definition":      def,
		"destination_ids": destIDs,
		"timezone":        row.Timezone,
		"next_run_at":     pgTime(row.NextRunAt),
		"last_run_at":     pgTime(row.LastRunAt),
		"created_at":      pgTime(row.CreatedAt),
		"updated_at":      pgTime(row.UpdatedAt),
	}
}

// validatedDefinition parses a submitted report definition strictly, checks
// that every host group exists, and returns the normalised JSON (version 2).
func (h *NotificationsHandler) validatedDefinition(ctx context.Context, raw map[string]interface{}) ([]byte, error) {
	in, _ := json.Marshal(raw)
	if raw == nil {
		in = []byte("{}")
	}
	def, err := reports.ParseDefinition(in)
	if err != nil {
		return nil, err
	}
	if _, err := reports.ValidateGroupIDs(ctx, h.q(ctx), def.HostGroupIDs); err != nil {
		return nil, err
	}
	out, err := json.Marshal(def)
	if err != nil {
		return nil, fmt.Errorf("definition: %w", err)
	}
	return out, nil
}

// definitionErrorStatus maps validation errors to 400 and everything else
// (database failures while checking groups) to 500.
func definitionErrorStatus(err error) int {
	if reports.IsConfigError(err) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func definitionErrorText(err error) string {
	if reports.IsConfigError(err) {
		return err.Error()
	}
	return "Failed to validate report definition"
}

// ListScheduledReports GET /notifications/scheduled-reports
func (h *NotificationsHandler) ListScheduledReports(w http.ResponseWriter, r *http.Request) {
	rows, err := h.q(r.Context()).ListScheduledReports(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to list")
		return
	}
	out := make([]map[string]interface{}, len(rows))
	for i := range rows {
		out[i] = h.scheduledReportToMap(rows[i])
	}
	JSON(w, http.StatusOK, out)
}

// CreateScheduledReport POST /notifications/scheduled-reports
func (h *NotificationsHandler) CreateScheduledReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string                 `json:"name"`
		CronExpr       string                 `json:"cron_expr"`
		Enabled        *bool                  `json:"enabled"`
		Definition     map[string]interface{} `json:"definition"`
		DestinationIDs []string               `json:"destination_ids"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	cron := req.CronExpr
	if cron == "" {
		cron = "0 8 * * *"
	}
	tz := h.timezoneForRequest(r.Context())
	def, err := h.validatedDefinition(r.Context(), req.Definition)
	if err != nil {
		Error(w, definitionErrorStatus(err), definitionErrorText(err))
		return
	}
	dest, _ := json.Marshal(req.DestinationIDs)
	en := true
	if req.Enabled != nil {
		en = *req.Enabled
	}
	next, err := notifications.NextCronRun(cron, tz, time.Now())
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid cron_expr")
		return
	}
	id := uuid.New().String()
	row, err := h.q(r.Context()).CreateScheduledReport(r.Context(), db.CreateScheduledReportParams{
		ID:             id,
		Name:           req.Name,
		CronExpr:       cron,
		Enabled:        en,
		Definition:     def,
		DestinationIds: dest,
		Timezone:       tz,
		NextRunAt:      pgtime.From(next),
		LastRunAt:      pgtype.Timestamp{Valid: false},
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to create")
		return
	}
	// Enqueue the first run at the computed next_run_at (event-driven).
	if en {
		_ = queue.EnqueueScheduledReportAt(h.qc, id, hostFromRequest(r), next)
	}
	JSON(w, http.StatusCreated, h.scheduledReportToMap(row))
}

// UpdateScheduledReport PUT /notifications/scheduled-reports/{id}
func (h *NotificationsHandler) UpdateScheduledReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name           string                 `json:"name"`
		CronExpr       string                 `json:"cron_expr"`
		Enabled        *bool                  `json:"enabled"`
		Definition     map[string]interface{} `json:"definition"`
		DestinationIDs []string               `json:"destination_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	ex, err := h.q(r.Context()).GetScheduledReportByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "Not found")
		return
	}
	name := ex.Name
	if req.Name != "" {
		name = req.Name
	}
	cron := ex.CronExpr
	if req.CronExpr != "" {
		cron = req.CronExpr
	}
	// Preserve the stored timezone; refresh from current tenant settings
	// only when the cron expression changes.
	tz := ex.Timezone
	if tz == "" {
		tz = h.timezoneForRequest(r.Context())
	}
	en := ex.Enabled
	if req.Enabled != nil {
		en = *req.Enabled
	}
	def := ex.Definition
	if req.Definition != nil {
		var derr error
		def, derr = h.validatedDefinition(r.Context(), req.Definition)
		if derr != nil {
			Error(w, definitionErrorStatus(derr), definitionErrorText(derr))
			return
		}
	}
	dest := ex.DestinationIds
	if req.DestinationIDs != nil {
		dest, _ = json.Marshal(req.DestinationIDs)
	}
	nextAt := ex.NextRunAt
	if req.CronExpr != "" {
		if n, err := notifications.NextCronRun(cron, tz, time.Now()); err == nil {
			nextAt = pgtime.From(n)
		}
	}
	row, err := h.q(r.Context()).UpdateScheduledReport(r.Context(), db.UpdateScheduledReportParams{
		ID:             id,
		Name:           name,
		CronExpr:       cron,
		Enabled:        en,
		Definition:     def,
		DestinationIds: dest,
		Timezone:       tz,
		NextRunAt:      nextAt,
		LastRunAt:      ex.LastRunAt,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update")
		return
	}
	// Re-enqueue if enabled and schedule changed (dedup handles existing tasks).
	if en && nextAt.Valid {
		_ = queue.EnqueueScheduledReportAt(h.qc, id, hostFromRequest(r), nextAt.Time)
	}
	JSON(w, http.StatusOK, h.scheduledReportToMap(row))
}

// RunScheduledReportNow POST /notifications/scheduled-reports/{id}/run-now
func (h *NotificationsHandler) RunScheduledReportNow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		Error(w, http.StatusBadRequest, "id required")
		return
	}
	ex, err := h.q(r.Context()).GetScheduledReportByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "Not found")
		return
	}
	if !ex.Enabled {
		Error(w, http.StatusBadRequest, "Report is disabled")
		return
	}
	// Enqueue immediately for instant execution.
	runID, err := queue.EnqueueScheduledReportManual(h.qc, id, hostFromRequest(r))
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to schedule report")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "scheduled", "run_id": runID})
}

var (
	previewGate     = reports.DefaultGate
	previewWait     = reports.PreviewWait
	previewDeadline = reports.RenderDeadline
	previewBuild    = reports.Build
)

func previewErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return http.StatusServiceUnavailable, "Rendering took too long"
	case reports.IsConfigError(err):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, reports.ErrPDFTooLarge):
		return http.StatusInternalServerError, "Report exceeds the 10 MB PDF limit"
	}
	return http.StatusInternalServerError, "Failed to render report"
}

// PreviewScheduledReport POST /notifications/scheduled-reports/{id}/preview
// Renders the report as PDF for download. Nothing is sent, stored or
// rescheduled. One renderer at a time; a busy renderer answers 503. The
// render budget (reports.RenderDeadline) starts with the request, so gate
// wait plus rendering always ends before the 30 s API timeout.
func (h *NotificationsHandler) PreviewScheduledReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		Error(w, http.StatusBadRequest, "id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), previewDeadline)
	defer cancel()
	release, err := previewGate.Acquire(ctx, previewWait)
	if err != nil {
		Error(w, http.StatusServiceUnavailable, "Renderer busy, try again in a few seconds")
		return
	}
	defer release()
	d := h.db.DB(ctx)
	rep, err := d.Queries.GetScheduledReportByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Not found")
			return
		}
		slog.Error("report preview: load report failed", "report_id", id, "error", err)
		Error(w, http.StatusInternalServerError, "Failed to load report")
		return
	}
	in := reports.BuildInput{ReportName: rep.Name, Definition: rep.Definition, Timezone: rep.Timezone, Now: time.Now(), PDF: true}
	if s, sErr := d.Queries.GetFirstSettings(ctx); sErr == nil {
		in.Branding, in.StaleAfter = reports.BrandingFromSettings(s)
	}
	out, err := previewBuild(ctx, d, in)
	if err != nil {
		code, msg := previewErrorStatus(err)
		if !reports.IsConfigError(err) {
			slog.Error("report preview failed", "report_id", id, "error", err)
		}
		Error(w, code, msg)
		return
	}
	// a render that finishes just as the budget lapses must not answer 200:
	// the router timeout may already have written its own response
	if ctx.Err() != nil {
		slog.Error("report preview failed", "report_id", id, "error", ctx.Err())
		Error(w, http.StatusServiceUnavailable, "Rendering took too long")
		return
	}
	generated := out.Model.GeneratedAt.UTC()
	if out.Model.Location != nil {
		generated = out.Model.GeneratedAt.In(out.Model.Location)
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+reports.PDFFileName(rep.Name, generated)+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Report-Logo", out.LogoSource)
	w.Header().Set("Content-Length", strconv.Itoa(len(out.PDF)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out.PDF)
}

// DeleteScheduledReport DELETE /notifications/scheduled-reports/{id}
func (h *NotificationsHandler) DeleteScheduledReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.q(r.Context()).DeleteScheduledReport(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to delete")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func pgTime(t pgtype.Timestamp) interface{} {
	if !t.Valid {
		return nil
	}
	return t.Time.UTC().Format(time.RFC3339)
}
