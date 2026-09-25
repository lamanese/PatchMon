package queue

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/branding"
	hostctx "github.com/PatchMon/PatchMon/server-source-code/internal/context"
	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/PatchMon/PatchMon/server-source-code/internal/reports"
	"github.com/PatchMon/PatchMon/server-source-code/internal/util"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
)

// ScheduledReportsDispatchHandler enqueues run jobs for due scheduled reports.
type ScheduledReportsDispatchHandler struct {
	defaultDB *database.DB
	poolCache *hostctx.PoolCache
	qc        *asynq.Client
	log       *slog.Logger
}

// NewScheduledReportsDispatchHandler creates the handler.
func NewScheduledReportsDispatchHandler(defaultDB *database.DB, poolCache *hostctx.PoolCache, qc *asynq.Client, log *slog.Logger) *ScheduledReportsDispatchHandler {
	return &ScheduledReportsDispatchHandler{defaultDB: defaultDB, poolCache: poolCache, qc: qc, log: log}
}

// resolveDB fails closed: a payload that names a tenant host must resolve
// to that tenant's database, never to the default one.
func (h *ScheduledReportsDispatchHandler) resolveDB(ctx context.Context, payload []byte) (*database.DB, error) {
	if len(payload) == 0 {
		return h.defaultDB, nil
	}
	var p AutomationPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("scheduled_reports_dispatch: invalid payload: %w", err)
	}
	return resolveTenantDB(ctx, h.defaultDB, h.poolCache, p.Host)
}

// resolveTenantDB returns the default DB for an empty host and the tenant DB
// otherwise. Missing pool cache or a failed lookup is an error (fail-closed).
func resolveTenantDB(ctx context.Context, defaultDB *database.DB, poolCache *hostctx.PoolCache, host string) (*database.DB, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return defaultDB, nil
	}
	if poolCache == nil {
		return nil, fmt.Errorf("tenant %q: no pool cache configured", host)
	}
	resolved, err := poolCache.GetOrCreate(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("tenant %q: %w", host, err)
	}
	if resolved == nil {
		return nil, fmt.Errorf("tenant %q: no database", host)
	}
	return resolved, nil
}

func (h *ScheduledReportsDispatchHandler) processDB(ctx context.Context, d *database.DB, tenantHost string) {
	now := time.Now()
	rows, err := d.Queries.ListScheduledReportsDue(ctx, pgtime.From(now))
	if err != nil {
		if h.log != nil {
			h.log.Error("scheduled_reports_dispatch: list due", "error", err)
		}
		return
	}
	for _, r := range rows {
		// Enqueue at the report's own stored slot (not "now") so the TaskID
		// matches the event-driven chain and duplicate runs are prevented.
		enqueueReportAtStoredSlot(ctx, d, h.qc, r, tenantHost, now, h.log)
	}
}

// ProcessTask implements asynq.Handler.
func (h *ScheduledReportsDispatchHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	payload := t.Payload()
	if len(payload) > 0 {
		d, err := h.resolveDB(ctx, payload)
		if err != nil {
			if h.log != nil {
				h.log.Error("scheduled_reports_dispatch: tenant resolution failed", "error", err)
			}
			return err
		}
		h.processDB(ctx, d, tenantHostFromPayload(payload))
		return nil
	}
	if h.poolCache == nil {
		h.processDB(ctx, h.defaultDB, "")
		return nil
	}
	h.processDB(ctx, h.defaultDB, "")
	hosts := h.poolCache.ListHosts()
	for _, host := range hosts {
		d, err := h.poolCache.GetOrCreate(ctx, host)
		if err != nil || d == nil {
			continue
		}
		h.processDB(ctx, d, host)
	}
	return nil
}

// ScheduledReportRunHandler builds and sends a scheduled report.
type ScheduledReportRunHandler struct {
	defaultDB *database.DB
	poolCache *hostctx.PoolCache
	qc        *asynq.Client
	enc       *util.Encryption
	log       *slog.Logger
}

// NewScheduledReportRunHandler creates the handler.
func NewScheduledReportRunHandler(defaultDB *database.DB, poolCache *hostctx.PoolCache, qc *asynq.Client, enc *util.Encryption, log *slog.Logger) *ScheduledReportRunHandler {
	return &ScheduledReportRunHandler{defaultDB: defaultDB, poolCache: poolCache, qc: qc, enc: enc, log: log}
}

// resolveDB fails closed (see resolveTenantDB).
func (h *ScheduledReportRunHandler) resolveDB(ctx context.Context, payload []byte) (*database.DB, error) {
	var p ScheduledReportRunPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("scheduled_report: invalid payload: %w", err)
	}
	return resolveTenantDB(ctx, h.defaultDB, h.poolCache, p.Host)
}

// Report archive and delivery limits (spec §7).
const (
	// ReportArchiveKeep is the number of archive rows kept per report.
	ReportArchiveKeep = 24
	// ReportMailDeadline bounds one mail (dial, auth, data) end to end.
	ReportMailDeadline = 60 * time.Second
)

// Seams for tests; production code never reassigns them.
var (
	sendReportEmail   = sendReportEmailSMTP
	sendReportWebhook = sendScheduledWebhook
	sendReportNtfy    = sendScheduledNtfy
	reportRetryState  = asynqRetryState
)

// errNoDestinations: the report has nothing it could deliver to. Deterministic
// like a config error; the run fails with destination_invalid.
var errNoDestinations = errors.New(reports.CodeDestinationInvalid)

// reportMarkTimeout bounds the status writes after a send. They run on a
// context detached from the task's, so a confirmed send is recorded even when
// the task context ends right after the SMTP server accepted the mail.
const reportMarkTimeout = 10 * time.Second

// asynqRetryState returns the task's retry count and limit. Outside an asynq
// context there are no retries left, so the run finalizes.
func asynqRetryState(ctx context.Context) (int, int) {
	n, ok1 := asynq.GetRetryCount(ctx)
	m, ok2 := asynq.GetMaxRetry(ctx)
	if !ok1 || !ok2 {
		return 0, 0
	}
	return n, m
}

func (h *ScheduledReportRunHandler) logInfo(msg string, args ...any) {
	if h.log != nil {
		h.log.Info(msg, args...)
	}
}

func (h *ScheduledReportRunHandler) logWarn(msg string, args ...any) {
	if h.log != nil {
		h.log.Warn(msg, args...)
	}
}

func (h *ScheduledReportRunHandler) logError(msg string, args ...any) {
	if h.log != nil {
		h.log.Error(msg, args...)
	}
}

// ProcessTask implements asynq.Handler. One task is one run (RunKey): claim
// the slot (scheduled runs), open the archive row, render and snapshot once,
// then deliver. A retry resumes the pending archive row and only re-sends
// deliveries that are not yet sent.
func (h *ScheduledReportRunHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p ScheduledReportRunPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}
	if p.Trigger == "" {
		h.logInfo("scheduled_report: legacy task discarded", "report_id", p.ReportID)
		return nil
	}
	runKey, err := p.RunKey()
	if err != nil {
		h.logWarn("scheduled_report: malformed task discarded", "report_id", p.ReportID, "error", err)
		return nil
	}
	d, err := h.resolveDB(ctx, t.Payload())
	if err != nil {
		h.logError("scheduled_report: tenant resolution failed", "report_id", p.ReportID, "error", err)
		return err
	}
	rep, err := d.Queries.GetScheduledReportByID(ctx, p.ReportID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !rep.Enabled) {
		return nil
	}
	if err != nil {
		return err
	}

	arch, err := d.Queries.ForkGetReportArchiveByRunKey(ctx, runKey)
	switch {
	case err == nil && arch.Status != "pending":
		return nil // already finished
	case err == nil:
		// A retry of this run: resume the pending archive row.
	case errors.Is(err, pgx.ErrNoRows):
		var claimed bool
		arch, claimed, err = h.openArchive(ctx, d, rep, p, runKey)
		if err != nil {
			return err
		}
		if !claimed {
			h.logInfo("scheduled_report: slot not claimable, task discarded", "report_id", rep.ID, "slot", p.SlotAt)
			return nil
		}
	default:
		return err
	}

	if !arch.HasPdf {
		if err := h.renderAndSnapshot(ctx, d, rep, arch.ID); err != nil {
			return h.handleRenderError(ctx, d, rep, arch.ID, err)
		}
	}
	return h.deliverArchive(ctx, d, rep, arch.ID)
}

// openArchive claims the slot (scheduled runs) and inserts the pending archive
// row in one transaction, so a claimed slot always has its archive row and a
// retry finds it. claimed is false when another task already took the slot.
// The next slot is enqueued after the commit, independent of delivery.
func (h *ScheduledReportRunHandler) openArchive(ctx context.Context, d *database.DB, rep db.ScheduledReport, p ScheduledReportRunPayload, runKey string) (db.ForkGetReportArchiveByRunKeyRow, bool, error) {
	var none db.ForkGetReportArchiveByRunKeyRow
	lang := reports.DefaultLanguage
	var groupIDs []string
	if def, derr := reports.ParseDefinition(rep.Definition); derr == nil {
		lang, groupIDs = def.Language, def.HostGroupIDs
	}
	tx, err := d.Begin(ctx)
	if err != nil {
		return none, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := d.Queries.WithTx(tx)

	var next time.Time
	if p.Trigger == ReportTriggerScheduled {
		now := time.Now()
		next = nextReportSlot(rep.CronExpr, rep.Timezone, now)
		rows, err := q.ForkClaimScheduledReportSlot(ctx, db.ForkClaimScheduledReportSlotParams{
			ID: rep.ID, Slot: pgtime.From(*p.SlotAt), Now: pgtime.From(now), Next: pgtime.From(next),
		})
		if err != nil {
			return none, false, err
		}
		if rows == 0 {
			return none, false, nil
		}
	}
	if err := q.ForkInsertReportArchive(ctx, db.ForkInsertReportArchiveParams{
		ID:                uuid.NewString(),
		ScheduledReportID: rep.ID,
		RunKey:            runKey,
		TriggerKind:       p.Trigger,
		SlotAt:            p.SlotAt,
		ReportName:        rep.Name,
		Language:          lang,
		CustomerMode:      rep.ForkEmailRecipients != nil,
		GroupIds:          groupIDs,
		Recipients:        rep.ForkEmailRecipients,
	}); err != nil {
		return none, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return none, false, err
	}
	if p.Trigger == ReportTriggerScheduled {
		if err := EnqueueScheduledReportAt(h.qc, rep.ID, p.Host, next); err != nil {
			h.logError("scheduled_report: failed to enqueue next run", "report_id", rep.ID, "next", next, "error", err)
		}
	}
	arch, err := d.Queries.ForkGetReportArchiveByRunKey(ctx, runKey)
	if err != nil {
		return none, false, err
	}
	return arch, true, nil
}

// runErrorCode classifies a render/plan failure. Recipient and destination
// problems are delivery configuration, not rendering.
func runErrorCode(err error) string {
	if errors.Is(err, errNoDestinations) || errors.Is(err, reports.ErrRecipients) {
		return reports.CodeDestinationInvalid
	}
	return reports.BuildErrorCode(err)
}

// handleRenderError finalizes deterministic failures (configuration, PDF too
// large, nothing to deliver to) at once and returns nil. A transient failure
// is returned for an asynq retry; on the last attempt the run is finalized as
// failed instead of waiting for the 24 h abandon rule.
func (h *ScheduledReportRunHandler) handleRenderError(ctx context.Context, d *database.DB, rep db.ScheduledReport, archiveID string, err error) error {
	deterministic := reports.IsConfigError(err) || errors.Is(err, reports.ErrPDFTooLarge) || errors.Is(err, errNoDestinations)
	if !deterministic {
		retried, maxRetry := reportRetryState(ctx)
		if retried < maxRetry || ctx.Err() != nil {
			h.logWarn("scheduled_report: render failed, retrying", "report_id", rep.ID, "archive_id", archiveID, "error", reports.RedactError(err))
			return err
		}
	}
	code, msg := runErrorCode(err), reports.RedactError(err)
	h.logWarn("scheduled_report: run failed", "report_id", rep.ID, "archive_id", archiveID, "code", code)
	h.finishArchive(ctx, d, archiveID, "failed", code, msg)
	h.insertRun(ctx, d, rep.ID, "failed", msg, "")
	applyReportRetention(ctx, d, rep.ID, h.log)
	return nil
}

// plannedDelivery is one row of fork_report_deliveries before it exists.
type plannedDelivery struct {
	destID, destName, channel, recipient string
}

// deliveryPlan is who receives this run, frozen into the snapshot.
type deliveryPlan struct {
	deliveries []plannedDelivery
	smtpDestID *string
	mailFrom   *string
}

// planDeliveries resolves the report's destinations. Customer mode sends one
// mail per recipient over exactly one enabled e-mail destination (its own To
// is ignored); internal mode keeps one delivery per destination. Missing,
// disabled and "internal" destinations are skipped.
func (h *ScheduledReportRunHandler) planDeliveries(ctx context.Context, d *database.DB, rep db.ScheduledReport) (deliveryPlan, error) {
	var plan deliveryPlan
	customer := rep.ForkEmailRecipients != nil
	var recipients []string
	if customer {
		var err error
		if recipients, err = reports.ParseRecipients(rep.ForkEmailRecipients); err != nil {
			return plan, err
		}
	}
	var ids []string
	if len(rep.DestinationIds) > 0 {
		_ = json.Unmarshal(rep.DestinationIds, &ids)
	}
	seen := map[string]bool{}
	var emailDests []db.NotificationDestination
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		dest, err := d.Queries.GetNotificationDestinationByID(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			h.logDebug("scheduled_report: destination missing, skipped", "report_id", rep.ID, "destination_id", id)
			continue
		}
		if err != nil {
			return plan, err
		}
		channel := strings.ToLower(dest.ChannelType)
		if !dest.Enabled || channel == "internal" {
			h.logDebug("scheduled_report: destination disabled or internal, skipped", "report_id", rep.ID, "destination_id", id)
			continue
		}
		switch {
		case channel == "email":
			emailDests = append(emailDests, dest)
		case customer:
			h.logDebug("scheduled_report: non-mail destination ignored in customer mode", "report_id", rep.ID, "destination_id", id)
		default:
			plan.deliveries = append(plan.deliveries, plannedDelivery{destID: dest.ID, destName: dest.DisplayName, channel: channel})
		}
	}

	if customer {
		if len(emailDests) != 1 {
			return plan, fmt.Errorf("%w: customer report needs one enabled e-mail destination", reports.ErrRecipients)
		}
		dest := emailDests[0]
		cfg, err := h.emailConfig(dest)
		if err != nil {
			return plan, fmt.Errorf("%w: e-mail destination config unreadable", errNoDestinations)
		}
		plan.smtpDestID, plan.mailFrom = &dest.ID, &cfg.From
		for _, addr := range recipients {
			plan.deliveries = append(plan.deliveries, plannedDelivery{destID: dest.ID, destName: dest.DisplayName, channel: "email", recipient: addr})
		}
	} else {
		for _, dest := range emailDests {
			// An unreadable config still gets a delivery row: it fails as
			// destination_invalid and stays visible in the archive.
			to := ""
			if cfg, err := h.emailConfig(dest); err == nil {
				to = strings.TrimSpace(cfg.To)
			}
			plan.deliveries = append(plan.deliveries, plannedDelivery{destID: dest.ID, destName: dest.DisplayName, channel: "email", recipient: to})
		}
	}
	if len(plan.deliveries) == 0 {
		return plan, fmt.Errorf("%w: no enabled destinations", errNoDestinations)
	}
	return plan, nil
}

func (h *ScheduledReportRunHandler) logDebug(msg string, args ...any) {
	if h.log != nil {
		h.log.Debug(msg, args...)
	}
}

// emailConfig decrypts and decodes an e-mail destination's config.
func (h *ScheduledReportRunHandler) emailConfig(dest db.NotificationDestination) (scheduledEmailConfig, error) {
	var cfg scheduledEmailConfig
	plain, err := decryptNotifConfig(h.enc, dest.ConfigEncrypted)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal([]byte(plain), &cfg)
	return cfg, err
}

// renderAndSnapshot renders the report once (HTML, CSV, PDF) and stores the
// output, the frozen scope and the planned deliveries in one transaction.
func (h *ScheduledReportRunHandler) renderAndSnapshot(ctx context.Context, d *database.DB, rep db.ScheduledReport, archiveID string) error {
	release, err := reports.DefaultGate.Acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer release()

	// Branding and the stale threshold come from the tenant's settings.
	branding := reports.Branding{}
	var staleAfter time.Duration
	if settings, sErr := d.Queries.GetFirstSettings(ctx); sErr == nil {
		branding, staleAfter = reports.BrandingFromSettings(settings)
	}
	out, err := reports.Build(ctx, d, reports.BuildInput{
		ReportName:   rep.Name,
		Definition:   rep.Definition,
		Timezone:     rep.Timezone,
		Now:          time.Now(),
		StaleAfter:   staleAfter,
		Branding:     branding,
		CustomerMode: rep.ForkEmailRecipients != nil,
		PDF:          true,
	})
	if err != nil {
		return err
	}
	plan, err := h.planDeliveries(ctx, d, rep)
	if err != nil {
		return err
	}

	m := out.Model
	groupIDs := make([]string, 0, len(m.Groups))
	groupNames := make([]string, 0, len(m.Groups))
	for _, g := range m.Groups {
		groupIDs = append(groupIDs, g.ID)
		groupNames = append(groupNames, g.Name)
	}
	sum := sha256.Sum256(out.PDF)
	sumHex := hex.EncodeToString(sum[:])
	periodFrom, periodTo := m.PeriodFrom, m.PeriodTo

	tx, err := d.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := d.Queries.WithTx(tx)
	if err := q.ForkSnapshotReportArchive(ctx, db.ForkSnapshotReportArchiveParams{
		ID:                archiveID,
		PeriodFrom:        &periodFrom,
		PeriodTo:          &periodTo,
		GroupIds:          groupIDs,
		GroupNames:        groupNames,
		HostCount:         int32(m.HostCount), //nolint:gosec // bounded by the scope limit
		SmtpDestinationID: plan.smtpDestID,
		MailFrom:          plan.mailFrom,
		Subject:           out.Subject,
		Html:              &out.HTML,
		Csv:               &out.CSV,
		Pdf:               out.PDF,
		PdfSize:           int32(len(out.PDF)), //nolint:gosec // bounded by the 10 MB PDF limit
		PdfSha256:         &sumHex,
	}); err != nil {
		return err
	}
	for _, pd := range plan.deliveries {
		if err := q.ForkInsertReportDelivery(ctx, db.ForkInsertReportDeliveryParams{
			ID:              uuid.NewString(),
			ArchiveID:       archiveID,
			DestinationID:   pd.destID,
			DestinationName: pd.destName,
			Channel:         pd.channel,
			Recipient:       pd.recipient,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// deliverArchive sends every delivery that is not yet sent, records each
// result immediately and finalizes the run. It returns an error only when a
// retryable delivery failed and asynq has retries left (archive stays pending).
func (h *ScheduledReportRunHandler) deliverArchive(ctx context.Context, d *database.DB, rep db.ScheduledReport, archiveID string) error {
	content, err := d.Queries.ForkGetReportArchiveContent(ctx, archiveID)
	if err != nil {
		return err
	}
	dels, err := d.Queries.ForkListReportDeliveries(ctx, archiveID)
	if err != nil {
		return err
	}
	loc, _ := reports.ResolveLocation(rep.Timezone)
	var sent, failed int
	anyRetryable := false
	firstCode := ""
	for _, del := range dels {
		if del.Status == "sent" {
			sent++
			continue
		}
		sendErr := h.sendDelivery(ctx, d, del, content, loc)
		params := db.ForkMarkReportDeliveryParams{ID: del.ID, Status: "sent"}
		if sendErr != nil {
			code, msg := classifyDeliveryError(del.Channel, sendErr), reports.RedactError(sendErr)
			params.Status, params.ErrorCode, params.ErrorMessage = "failed", &code, &msg
			failed++
			if firstCode == "" {
				firstCode = code
			}
			if reports.RetryableCode(code) {
				anyRetryable = true
			}
			h.logWarn("scheduled_report: delivery failed", "archive_id", archiveID, "delivery_id", del.ID, "channel", del.Channel, "code", code)
		} else {
			sent++
		}
		markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reportMarkTimeout)
		err := d.Queries.ForkMarkReportDelivery(markCtx, params)
		cancel()
		if err != nil {
			return err
		}
	}

	if retried, maxRetry := reportRetryState(ctx); anyRetryable && retried < maxRetry {
		return fmt.Errorf("scheduled_report: %d deliveries failed, retrying", failed)
	}

	status, code, msg := "completed", "", ""
	switch {
	case failed == 0:
	case sent == 0:
		status, code, msg = "failed", firstCode, "no delivery succeeded"
	default:
		status, msg = "partial", fmt.Sprintf("%d of %d deliveries failed", failed, sent+failed)
	}
	h.logInfo("scheduled_report: run finished", "report_id", rep.ID, "archive_id", archiveID, "status", status, "sent", sent, "failed", failed)
	h.finishArchive(ctx, d, archiveID, status, code, msg)
	pdfSum := ""
	if content.PdfSha256 != nil {
		pdfSum = *content.PdfSha256
	}
	h.insertRun(ctx, d, rep.ID, status, msg, pdfSum)
	applyReportRetention(ctx, d, rep.ID, h.log)
	return nil
}

// finishArchive sets the archive row's final status.
func (h *ScheduledReportRunHandler) finishArchive(ctx context.Context, d *database.DB, archiveID, status, code, msg string) {
	var cp, mp *string
	if code != "" {
		cp = &code
	}
	if msg != "" {
		mp = &msg
	}
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reportMarkTimeout)
	defer cancel()
	if err := d.Queries.ForkFinishReportArchive(markCtx, db.ForkFinishReportArchiveParams{ID: archiveID, Status: status, ErrorCode: cp, ErrorMessage: mp}); err != nil {
		h.logError("scheduled_report: finish archive failed", "archive_id", archiveID, "error", err)
	}
}

// applyReportRetention marks pending rows older than 24 h as abandoned and
// keeps the ReportArchiveKeep newest rows of the report.
func applyReportRetention(ctx context.Context, d *database.DB, reportID string, log *slog.Logger) {
	abandoned, err := d.Queries.ForkAbandonStaleReportArchive(ctx, reportID)
	if err != nil && log != nil {
		log.Error("scheduled_report: abandon stale archive rows failed", "report_id", reportID, "error", err)
	}
	pruned, err := d.Queries.ForkPruneReportArchive(ctx, db.ForkPruneReportArchiveParams{ID: reportID, Keep: ReportArchiveKeep})
	if err != nil && log != nil {
		log.Error("scheduled_report: prune archive failed", "report_id", reportID, "error", err)
	}
	if log != nil {
		log.Debug("scheduled_report: archive retention", "report_id", reportID, "abandoned", abandoned, "pruned", pruned)
	}
}

func (h *ScheduledReportRunHandler) insertRun(ctx context.Context, d *database.DB, reportID, status, errMsg, hash string) {
	var em *string
	if errMsg != "" {
		em = &errMsg
	}
	var sh *string
	if hash != "" {
		sh = &hash
	}
	_, err := d.Queries.InsertScheduledReportRun(ctx, db.InsertScheduledReportRunParams{
		ID:                uuid.New().String(),
		ScheduledReportID: reportID,
		Status:            status,
		ErrorMessage:      em,
		SummaryHash:       sh,
	})
	if err != nil && h.log != nil {
		h.log.Debug("scheduled_report: insert run failed", "error", err)
	}
}

func decryptNotifConfig(enc *util.Encryption, s string) (string, error) {
	if s == "" {
		return "{}", nil
	}
	if enc != nil && util.IsEncrypted(s) {
		return enc.Decrypt(s)
	}
	return s, nil
}

type scheduledWebhookConfig struct {
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	SigningSecret string            `json:"signing_secret"`
}

func sendScheduledWebhook(ctx context.Context, plain, subject, html, csv string) error {
	var cfg scheduledWebhookConfig
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return err
	}
	if cfg.URL == "" {
		return fmt.Errorf("webhook url missing")
	}
	var b []byte
	var err error
	switch {
	case isDiscordWebhookURL(cfg.URL):
		b, err = discordScheduledReportWebhookBody(subject, html, csv)
	case isSlackIncomingWebhookURL(cfg.URL):
		b, err = slackScheduledReportWebhookBody(subject, html, csv)
	default:
		body := map[string]interface{}{
			"kind":    "scheduled_report",
			"subject": subject,
			"html":    html,
			"csv":     csv,
		}
		b, err = json.Marshal(body)
	}
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		if strings.TrimSpace(k) != "" {
			req.Header.Set(k, v)
		}
	}
	if cfg.SigningSecret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.SigningSecret))
		mac.Write(b)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-PatchMon-Signature", "sha256="+sig)
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}

type scheduledEmailConfig struct {
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"`
	UseTLS   bool   `json:"use_tls"`
}

func sendScheduledNtfy(ctx context.Context, plain, subject, html, csv string) error {
	var cfg ntfyConfig
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return err
	}
	if cfg.Topic == "" {
		return fmt.Errorf("ntfy topic is required")
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = "https://ntfy.sh"
	}
	serverURL := strings.TrimRight(cfg.ServerURL, "/")

	title := strings.TrimSpace(subject)
	if title == "" {
		title = branding.ProductNameShort + " scheduled report"
	}

	// Build a plain-text excerpt from the HTML for ntfy
	message := stripScheduledReportHTML(html)
	if message == "" {
		message = "Scheduled report delivered"
	}
	message = truncateUTF8(message, 4000)

	body := map[string]interface{}{
		"topic":    cfg.Topic,
		"title":    truncateUTF8(title, 256),
		"message":  message,
		"priority": 3,
		"tags":     []string{"bar_chart", "clipboard"},
	}

	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	} else if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy status %d", resp.StatusCode)
	}
	return nil
}
