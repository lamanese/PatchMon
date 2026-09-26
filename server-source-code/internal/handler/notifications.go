package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
		// null = internal report, [...] = customer report (mail per recipient).
		"email_recipients": row.ForkEmailRecipients,
		"customer_mode":    row.ForkEmailRecipients != nil,
		// false = render and archive only, nothing is sent.
		"deliver": row.ForkDeliver,
		// Newest runs kept in the archive.
		"archive_keep": row.ForkArchiveKeep,
	}
}

// archiveKeepOrError validates an optional archive_keep from a request body;
// nil means "use fallback".
func archiveKeepOrError(v *int, fallback int32) (int32, error) {
	if v == nil {
		return fallback, nil
	}
	if err := reports.ValidateArchiveKeep(*v); err != nil {
		return 0, fmt.Errorf("archive_keep must be between %d and %d runs", reports.MinArchiveKeep, reports.MaxArchiveKeep)
	}
	return int32(*v), nil //nolint:gosec // bounded by ValidateArchiveKeep
}

// RunNowCooldown bounds "Run now" to one enqueue per report and window.
const RunNowCooldown = 60 * time.Second

// MinCustomerReportInterval is the shortest schedule a customer report may use.
const MinCustomerReportInterval = time.Hour

// MaxReportNameLength bounds a report name (characters).
const MaxReportNameLength = 200

// customerCronLookahead is how many upcoming runs validateCustomerCron checks.
const customerCronLookahead = 64

// deliveryError is a validation error with an API-safe text (never a
// recipient address). It unwraps to reports.ErrRecipients so
// definitionErrorStatus answers 400.
type deliveryError struct{ msg string }

func (e deliveryError) Error() string { return e.msg }
func (e deliveryError) Unwrap() error { return reports.ErrRecipients }

// reportDeliveryInput is the delivery side of a report as it will be stored.
type reportDeliveryInput struct {
	Recipients *[]string // nil = internal report
	DestIDs    []string
	CronExpr   string
	Timezone   string
	Enabled    bool
	Definition []byte // normalised definition JSON
}

// reportDeliveryPlan is the validated delivery side of a report.
type reportDeliveryPlan struct {
	Recipients     []string // nil = internal report
	CustomerMode   bool
	DestinationIDs []string // the ids to store (never nil)
}

// validateReportDelivery checks recipients and destinations. Recipients are
// always parsed. An enabled customer report is strict: host groups, exactly
// one enabled e-mail destination with TLS and a valid sender, and at most
// one run per hour. Every other report (internal, or disabled) silently
// drops destination ids that no longer exist or are of type internal (the
// worker skips them anyway), so a stale id never blocks a save and a report
// can always be disabled.
func (h *NotificationsHandler) validateReportDelivery(ctx context.Context, in reportDeliveryInput) (reportDeliveryPlan, error) {
	plan := reportDeliveryPlan{DestinationIDs: []string{}}
	if in.Recipients != nil {
		parsed, err := reports.ParseRecipients(*in.Recipients)
		if err != nil {
			// The helper texts carry no address; rename the field for the API.
			return plan, deliveryError{msg: "email_recipients: " + strings.Replace(err.Error(), "recipients: ", "", 1)}
		}
		plan.Recipients = parsed
		plan.CustomerMode = parsed != nil
	}
	strict := plan.CustomerMode && in.Enabled
	q := h.q(ctx)
	dests := make([]db.NotificationDestination, 0, len(in.DestIDs))
	for _, id := range in.DestIDs {
		dst, err := q.GetNotificationDestinationByID(ctx, id)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return plan, fmt.Errorf("load destination: %w", err)
			}
			if strict {
				return plan, deliveryError{msg: fmt.Sprintf("destination %s not found", id)}
			}
			continue
		}
		if dst.ChannelType == "internal" {
			if strict {
				return plan, deliveryError{msg: "destination type internal cannot receive reports"}
			}
			continue
		}
		dests = append(dests, dst)
		plan.DestinationIDs = append(plan.DestinationIDs, id)
	}
	if !strict {
		return plan, nil
	}
	def, err := reports.ParseDefinition(in.Definition)
	if err != nil {
		return plan, err
	}
	if len(def.HostGroupIDs) == 0 {
		return plan, deliveryError{msg: "customer reports need at least one host group"}
	}
	if len(dests) != 1 || dests[0].ChannelType != "email" || !dests[0].Enabled {
		return plan, deliveryError{msg: "customer reports need exactly one enabled e-mail destination"}
	}
	if err := queue.CheckCustomerSMTPConfig(h.enc, dests[0].ConfigEncrypted); err != nil {
		return plan, deliveryError{msg: err.Error()}
	}
	if err := validateCustomerCron(in.CronExpr, in.Timezone, time.Now()); err != nil {
		return plan, err
	}
	return plan, nil
}

// validateCustomerCron rejects schedules where any two consecutive runs among
// the next customerCronLookahead runs are less than MinCustomerReportInterval
// apart (catches irregular lists such as "0,30 9 * * *").
func validateCustomerCron(expr, tz string, now time.Time) error {
	prev, err := notifications.NextCronRun(expr, tz, now)
	if err != nil {
		return deliveryError{msg: "Invalid cron_expr"}
	}
	for i := 1; i < customerCronLookahead; i++ {
		next, err := notifications.NextCronRun(expr, tz, prev)
		if err != nil {
			return deliveryError{msg: "Invalid cron_expr"}
		}
		if next.Sub(prev) < MinCustomerReportInterval {
			return deliveryError{msg: "customer reports can run at most once per hour"}
		}
		prev = next
	}
	return nil
}

// reportNameTooLong reports whether a name exceeds MaxReportNameLength characters.
func reportNameTooLong(name string) bool {
	return utf8.RuneCountInString(name) > MaxReportNameLength
}

// runNowLimiter remembers the last accepted "Run now" per report (in memory,
// per server process).
type runNowLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

var runNowLimit = &runNowLimiter{last: map[string]time.Time{}, now: time.Now}

// allow reserves the run-now slot of reportID; when the cooldown is still
// running it returns the remaining wait and false.
func (l *runNowLimiter) allow(reportID string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if t, ok := l.last[reportID]; ok {
		if wait := RunNowCooldown - now.Sub(t); wait > 0 {
			return wait, false
		}
	}
	if len(l.last) >= 1024 {
		for id, t := range l.last {
			if now.Sub(t) >= RunNowCooldown {
				delete(l.last, id)
			}
		}
	}
	l.last[reportID] = now
	return 0, true
}

// forget releases a reserved slot (enqueue failed, nothing ran).
func (l *runNowLimiter) forget(reportID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.last, reportID)
}

// destinationIDsOf decodes the stored destination_ids JSON array.
func destinationIDsOf(raw []byte) []string {
	var ids []string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &ids)
	}
	return ids
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
		Name            string                 `json:"name"`
		CronExpr        string                 `json:"cron_expr"`
		Enabled         *bool                  `json:"enabled"`
		Definition      map[string]interface{} `json:"definition"`
		DestinationIDs  []string               `json:"destination_ids"`
		EmailRecipients *[]string              `json:"email_recipients"`
		// Absent = true. false renders and archives without sending.
		Deliver *bool `json:"deliver"`
		// Absent = reports.DefaultArchiveKeep; otherwise 1..MaxArchiveKeep.
		ArchiveKeep *int `json:"archive_keep"`
	}
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if req.Name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	deliver := req.Deliver == nil || *req.Deliver
	archiveKeep, err := archiveKeepOrError(req.ArchiveKeep, reports.DefaultArchiveKeep)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if reportNameTooLong(req.Name) {
		Error(w, http.StatusBadRequest, fmt.Sprintf("name too long (max %d)", MaxReportNameLength))
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
	en := true
	if req.Enabled != nil {
		en = *req.Enabled
	}
	next, err := notifications.NextCronRun(cron, tz, time.Now())
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid cron_expr")
		return
	}
	plan, err := h.validateReportDelivery(r.Context(), reportDeliveryInput{
		Recipients: req.EmailRecipients, DestIDs: req.DestinationIDs, CronExpr: cron, Timezone: tz, Enabled: en, Definition: def,
	})
	if err != nil {
		Error(w, definitionErrorStatus(err), definitionErrorText(err))
		return
	}
	dest, _ := json.Marshal(plan.DestinationIDs)
	id := uuid.New().String()
	ctx := r.Context()
	d := h.db.DB(ctx)
	tx, err := d.Begin(ctx)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to create")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := d.Queries.WithTx(tx)
	_, err = q.CreateScheduledReport(ctx, db.CreateScheduledReportParams{
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
	if err == nil {
		err = q.ForkSetScheduledReportForkFields(ctx, db.ForkSetScheduledReportForkFieldsParams{ID: id, Recipients: plan.Recipients, Deliver: deliver, ArchiveKeep: archiveKeep})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to create")
		return
	}
	row, err := d.Queries.GetScheduledReportByID(ctx, id)
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
		// Absent = keep the stored recipients, null = internal report,
		// [...] = customer report ([] is rejected).
		EmailRecipients json.RawMessage `json:"email_recipients"`
		// Absent = keep the stored values.
		Deliver     *bool `json:"deliver"`
		ArchiveKeep *int  `json:"archive_keep"`
	}
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if reportNameTooLong(req.Name) {
		Error(w, http.StatusBadRequest, fmt.Sprintf("name too long (max %d)", MaxReportNameLength))
		return
	}
	ctx := r.Context()
	d := h.db.DB(ctx)
	// Lock the row first: the worker's slot claim writes next_run_at and
	// last_run_at, and this update must never write back stale values.
	tx, err := d.Begin(ctx)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := d.Queries.WithTx(tx)
	ex, err := q.ForkGetScheduledReportForUpdate(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Not found")
			return
		}
		Error(w, http.StatusInternalServerError, "Failed to update")
		return
	}
	name := ex.Name
	if req.Name != "" {
		name = req.Name
	}
	deliver := ex.ForkDeliver
	if req.Deliver != nil {
		deliver = *req.Deliver
	}
	archiveKeep, err := archiveKeepOrError(req.ArchiveKeep, ex.ForkArchiveKeep)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	cron := ex.CronExpr
	if req.CronExpr != "" {
		cron = req.CronExpr
	}
	// Preserve the stored timezone; refresh from current tenant settings
	// only when the cron expression changes.
	tz := ex.Timezone
	if tz == "" {
		tz = h.timezoneForRequest(ctx)
	}
	en := ex.Enabled
	if req.Enabled != nil {
		en = *req.Enabled
	}
	def := ex.Definition
	if req.Definition != nil {
		var derr error
		def, derr = h.validatedDefinition(ctx, req.Definition)
		if derr != nil {
			Error(w, definitionErrorStatus(derr), definitionErrorText(derr))
			return
		}
	}
	destIDs := destinationIDsOf(ex.DestinationIds)
	if req.DestinationIDs != nil {
		destIDs = req.DestinationIDs
	}
	var recipients *[]string
	switch raw := strings.TrimSpace(string(req.EmailRecipients)); raw {
	case "":
		if ex.ForkEmailRecipients != nil {
			stored := ex.ForkEmailRecipients
			recipients = &stored
		}
	case "null":
	default:
		var list []string
		if err := json.Unmarshal(req.EmailRecipients, &list); err != nil {
			Error(w, http.StatusBadRequest, "email_recipients must be a list of addresses or null")
			return
		}
		if list == nil {
			list = []string{}
		}
		recipients = &list
	}
	plan, err := h.validateReportDelivery(ctx, reportDeliveryInput{
		Recipients: recipients, DestIDs: destIDs, CronExpr: cron, Timezone: tz, Enabled: en, Definition: def,
	})
	if err != nil {
		Error(w, definitionErrorStatus(err), definitionErrorText(err))
		return
	}
	dest, _ := json.Marshal(plan.DestinationIDs)
	// next_run_at comes from the locked row; it is recomputed only when the
	// schedule changed or the report is being enabled with a past (or no) slot.
	now := time.Now()
	nextAt := ex.NextRunAt
	if cron != ex.CronExpr {
		if n, err := notifications.NextCronRun(cron, tz, now); err == nil {
			nextAt = pgtime.From(n)
		}
	}
	// Enabling a report whose slot passed (or is missing) must not fire at
	// once. A past slot of a report that stays enabled is kept: the worker
	// claims it late, once.
	if en && !ex.Enabled && (!nextAt.Valid || nextAt.Time.Before(now)) {
		if n, err := notifications.NextCronRun(cron, tz, now); err == nil {
			nextAt = pgtime.From(n)
		}
	}
	_, err = q.UpdateScheduledReport(ctx, db.UpdateScheduledReportParams{
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
	if err == nil {
		err = q.ForkSetScheduledReportForkFields(ctx, db.ForkSetScheduledReportForkFieldsParams{ID: id, Recipients: plan.Recipients, Deliver: deliver, ArchiveKeep: archiveKeep})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update")
		return
	}
	row, err := d.Queries.GetScheduledReportByID(ctx, id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update")
		return
	}
	// Re-enqueue if enabled and schedule changed (dedup handles existing tasks).
	if row.Enabled && row.NextRunAt.Valid {
		_ = queue.EnqueueScheduledReportAt(h.qc, id, hostFromRequest(r), row.NextRunAt.Time)
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
	if wait, ok := runNowLimit.allow(id); !ok {
		Error(w, http.StatusTooManyRequests, fmt.Sprintf("Please wait %d seconds before running this report again", int(wait.Seconds())+1))
		return
	}
	ex, err := h.q(r.Context()).GetScheduledReportByID(r.Context(), id)
	if err != nil {
		runNowLimit.forget(id)
		Error(w, http.StatusNotFound, "Not found")
		return
	}
	if !ex.Enabled {
		runNowLimit.forget(id)
		Error(w, http.StatusBadRequest, "Report is disabled")
		return
	}
	// Enqueue immediately for instant execution.
	runID, err := queue.EnqueueScheduledReportManual(h.qc, id, hostFromRequest(r))
	if err != nil {
		runNowLimit.forget(id)
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

// logPreviewErr logs a preview failure. A cancelled context means the client
// gave up (browser closed the tab, navigated away); that is not a server
// problem and is logged at Warn, everything else at Error.
func logPreviewErr(msg, reportID string, err error) {
	if errors.Is(err, context.Canceled) {
		slog.Warn(msg, "report_id", reportID, "error", err)
		return
	}
	slog.Error(msg, "report_id", reportID, "error", err)
}

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
		code, msg := previewErrorStatus(err)
		logPreviewErr("report preview: load report failed", id, err)
		Error(w, code, msg)
		return
	}
	in := reports.BuildInput{ReportName: rep.Name, Definition: rep.Definition, Timezone: rep.Timezone, Now: time.Now(), PDF: true,
		CustomerMode: rep.ForkEmailRecipients != nil}
	if s, sErr := d.Queries.GetFirstSettings(ctx); sErr == nil {
		in.Branding, in.StaleAfter = reports.BrandingFromSettings(s)
	}
	out, err := previewBuild(ctx, d, in)
	if err != nil {
		code, msg := previewErrorStatus(err)
		if !reports.IsConfigError(err) {
			logPreviewErr("report preview failed", id, err)
		}
		Error(w, code, msg)
		return
	}
	// a render that finishes just as the budget lapses must not answer 200:
	// the router timeout may already have written its own response
	if ctx.Err() != nil {
		logPreviewErr("report preview failed", id, ctx.Err())
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

func timePtrUTC(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func strOrEmpty(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}

// ListReportArchive GET /notifications/scheduled-reports/{id}/archive
// Lists the newest archived runs of a report with their deliveries. PDF
// bytes are never part of the list (download them separately).
func (h *NotificationsHandler) ListReportArchive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	q := h.q(ctx)
	if _, err := q.GetScheduledReportByID(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Not found")
			return
		}
		slog.Error("report archive: load report failed", "report_id", id, "error", err)
		Error(w, http.StatusInternalServerError, "Failed to load archive")
		return
	}
	rows, err := q.ForkListReportArchive(ctx, db.ForkListReportArchiveParams{ScheduledReportID: id, Limit: int32(reports.MaxArchiveKeep)})
	if err != nil {
		slog.Error("report archive: list failed", "report_id", id, "error", err)
		Error(w, http.StatusInternalServerError, "Failed to load archive")
		return
	}
	dels, err := q.ForkListReportDeliveriesForReport(ctx, id)
	if err != nil {
		slog.Error("report archive: list deliveries failed", "report_id", id, "error", err)
		Error(w, http.StatusInternalServerError, "Failed to load archive")
		return
	}
	byArchive := make(map[string][]map[string]interface{}, len(rows))
	for _, dl := range dels {
		byArchive[dl.ArchiveID] = append(byArchive[dl.ArchiveID], map[string]interface{}{
			"id":               dl.ID,
			"destination_id":   dl.DestinationID,
			"destination_name": dl.DestinationName,
			"channel":          dl.Channel,
			"recipient":        dl.Recipient,
			"status":           dl.Status,
			"error_code":       dl.ErrorCode,
			"error_message":    dl.ErrorMessage,
			"attempts":         dl.Attempts,
			"sent_at":          timePtrUTC(dl.SentAt),
		})
	}
	out := make([]map[string]interface{}, len(rows))
	for i, a := range rows {
		deliveries := byArchive[a.ID]
		if deliveries == nil {
			deliveries = []map[string]interface{}{}
		}
		out[i] = map[string]interface{}{
			"id":            a.ID,
			"trigger":       a.TriggerKind,
			"slot_at":       timePtrUTC(a.SlotAt),
			"created_at":    a.CreatedAt.UTC().Format(time.RFC3339),
			"finished_at":   timePtrUTC(a.FinishedAt),
			"status":        a.Status,
			"error_code":    a.ErrorCode,
			"error_message": a.ErrorMessage,
			"report_name":   a.ReportName,
			"language":      a.Language,
			"period_from":   timePtrUTC(a.PeriodFrom),
			"period_to":     timePtrUTC(a.PeriodTo),
			"group_names":   strOrEmpty(a.GroupNames),
			"host_count":    a.HostCount,
			"customer_mode": a.CustomerMode,
			// false = the run was archived without sending (delivery off).
			"delivery_enabled": a.DeliveryEnabled,
			"recipients":       strOrEmpty(a.Recipients),
			"mail_from":        a.MailFrom,
			"pdf_size":         a.PdfSize,
			"has_pdf":          a.HasPdf,
			"deliveries":       deliveries,
		}
	}
	JSON(w, http.StatusOK, out)
}

// DownloadReportArchivePDF GET /notifications/scheduled-reports/archive/{archiveId}/pdf
func (h *NotificationsHandler) DownloadReportArchivePDF(w http.ResponseWriter, r *http.Request) {
	archiveID := chi.URLParam(r, "archiveId")
	ctx := r.Context()
	row, err := h.q(ctx).ForkGetReportArchivePDF(ctx, archiveID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Not found")
			return
		}
		slog.Error("report archive: load pdf failed", "archive_id", archiveID, "error", err)
		Error(w, http.StatusInternalServerError, "Failed to load PDF")
		return
	}
	if len(row.Pdf) == 0 {
		Error(w, http.StatusNotFound, "Not found")
		return
	}
	loc, _ := reports.ResolveLocation(row.Timezone)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+reports.PDFFileName(row.ReportName, row.CreatedAt.In(loc))+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(row.Pdf)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(row.Pdf)
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
