package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/PatchMon/PatchMon/server-source-code/internal/config"
	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/license"
	"github.com/PatchMon/PatchMon/server-source-code/internal/middleware"
	"github.com/PatchMon/PatchMon/server-source-code/internal/store"
	"github.com/google/uuid"
)

// LicenseHandler handles the host licence settings (fork feature: amanit
// sells packages by VM count; the platform shows the licence and can
// optionally block new host registrations beyond max + tolerance).
type LicenseHandler struct {
	settings *store.SettingsStore
	hosts    *store.HostsStore
	cfg      *config.Config
	db       database.DBProvider
}

// NewLicenseHandler creates a new licence handler.
func NewLicenseHandler(settings *store.SettingsStore, hosts *store.HostsStore, cfg *config.Config, dbp database.DBProvider) *LicenseHandler {
	return &LicenseHandler{settings: settings, hosts: hosts, cfg: cfg, db: dbp}
}

// Get handles GET /api/v1/license - effective licence plus current usage.
// Read-only, visible to anyone with can_manage_settings.
func (h *LicenseHandler) Get(w http.ResponseWriter, r *http.Request) {
	s, err := h.settings.GetFirst(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to load settings")
		return
	}
	active, pending, err := h.hosts.CountByStatus(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to count hosts")
		return
	}

	eff := license.Resolve(h.cfg, s)
	used := active + pending

	var maxHosts, hardLimit interface{}
	if eff.MaxHosts != nil {
		maxHosts = *eff.MaxHosts
		hardLimit = license.HardLimit(*eff.MaxHosts)
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"max_hosts":     maxHosts,
		"package":       eff.Package,
		"enforce":       eff.Enforce,
		"locked":        eff.Locked,
		"tolerance_pct": license.TolerancePct,
		"hard_limit":    hardLimit,
		"active_count":  active,
		"pending_count": pending,
		"used_slots":    used,
		"status":        eff.Status(used),
	})
}

// Update handles PUT /api/v1/license - superadmin only. Refused with 403
// when the licence is env-managed (PM_LICENSE_MAX_HOSTS set).
func (h *LicenseHandler) Update(w http.ResponseWriter, r *http.Request) {
	callerRole, _ := r.Context().Value(middleware.UserRoleKey).(string)
	if callerRole != "superadmin" {
		Error(w, http.StatusForbidden, "Only superadmins can change the licence")
		return
	}
	if h.cfg != nil && h.cfg.LicenseMaxHosts > 0 {
		Error(w, http.StatusForbidden, "Licence is managed via environment (PM_LICENSE_MAX_HOSTS) and cannot be changed here")
		return
	}

	var req struct {
		MaxHosts *int    `json:"max_hosts"`
		Enforce  *bool   `json:"enforce"`
		Package  *string `json:"package"`
	}
	if err := decodeJSON(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.MaxHosts != nil && (*req.MaxHosts < 1 || *req.MaxHosts > license.MaxLicensableHosts) {
		Error(w, http.StatusBadRequest, fmt.Sprintf("max_hosts must be between 1 and %d (or null for unlimited)", license.MaxLicensableHosts))
		return
	}
	if req.Enforce == nil {
		Error(w, http.StatusBadRequest, "enforce must be a boolean")
		return
	}
	if req.Package != nil {
		trimmed := strings.TrimSpace(*req.Package)
		if trimmed == "" {
			req.Package = nil
		} else if len(trimmed) > 200 {
			Error(w, http.StatusBadRequest, "package must be at most 200 characters")
			return
		} else {
			req.Package = &trimmed
		}
	}

	s, err := h.settings.GetFirst(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to load settings")
		return
	}

	// Audit before applying (fail-closed: no change without a trail).
	detail := map[string]interface{}{
		"old_max_hosts": s.LicenseMaxHosts,
		"old_enforce":   s.LicenseEnforce,
		"old_package":   s.LicensePackage,
		"new_max_hosts": req.MaxHosts,
		"new_enforce":   *req.Enforce,
		"new_package":   req.Package,
	}
	if err := h.writeAuditLog(r, "license_settings_updated", true, detail); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to write audit log")
		return
	}

	if err := h.settings.UpdateLicense(r.Context(), s.ID, req.MaxHosts, *req.Enforce, req.Package); err != nil {
		_ = h.writeAuditLog(r, "license_settings_update_failed", false, detail)
		Error(w, http.StatusInternalServerError, "Failed to update licence settings")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"message":   "Licence settings updated successfully",
		"max_hosts": req.MaxHosts,
		"enforce":   *req.Enforce,
		"package":   req.Package,
	})
}

// licenseLimitMessage is returned by both host create paths when the gate refuses.
const licenseLimitMessage = "Licensed host limit reached (max plus 10% tolerance). Upgrade the licence to register more hosts."

// licenseBlocksHostCreate reports whether the licence gate refuses another
// host registration. The gate counts active+pending hosts (a slot is
// reserved at creation time — otherwise pending hosts would bypass the
// limit and connect later). Errors fail open, matching the SaaS MaxHosts
// check: the licence is transparency + contract, not copy protection.
// Note: count-then-create is TOCTOU-prone; parallel enrollments can
// overshoot the limit by a few hosts, which is acceptable here.
func licenseBlocksHostCreate(ctx context.Context, cfg *config.Config, settingsStore *store.SettingsStore, hostsStore *store.HostsStore) bool {
	s, err := settingsStore.GetFirst(ctx)
	if err != nil {
		s = nil // an env-managed licence still applies
	}
	eff := license.Resolve(cfg, s)
	if !eff.Enforce || eff.MaxHosts == nil {
		return false
	}
	active, pending, err := hostsStore.CountByStatus(ctx)
	if err != nil {
		return false
	}
	return eff.Blocks(active + pending)
}

// writeAuditLog inserts an audit_logs entry attributed to the acting user.
func (h *LicenseHandler) writeAuditLog(r *http.Request, event string, success bool, detail map[string]interface{}) error {
	if h.db == nil {
		return fmt.Errorf("no database available for audit log")
	}
	ctx := r.Context()
	d := h.db.DB(ctx)
	if d == nil {
		return fmt.Errorf("no database available for audit log")
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
	if b, err := json.Marshal(detail); err == nil {
		s := string(b)
		details = &s
	}

	return d.Queries.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ID:        uuid.New().String(),
		Event:     event,
		UserID:    userID,
		IpAddress: &ip,
		UserAgent: &ua,
		RequestID: requestID,
		Details:   details,
		Success:   success,
	})
}
