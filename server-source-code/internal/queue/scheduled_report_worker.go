package queue

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	hostctx "github.com/PatchMon/PatchMon/server-source-code/internal/context"
	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/notifications"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/PatchMon/PatchMon/server-source-code/internal/reports"
	"github.com/PatchMon/PatchMon/server-source-code/internal/util"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
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
		// Use the same enqueue path as the event-driven chain so TaskIDs
		// are consistent and duplicate runs are prevented.
		if err := EnqueueScheduledReportAt(h.qc, r.ID, tenantHost, now); err != nil {
			if h.log != nil {
				h.log.Debug("scheduled_reports_dispatch: enqueue skipped", "report_id", r.ID, "error", err)
			}
		}
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

// ScheduledReportRunPayload is the payload for scheduled_report_run.
type ScheduledReportRunPayload struct {
	ReportID string `json:"report_id"`
	Host     string `json:"host,omitempty"`
}

// NewScheduledReportRunTask enqueues report generation and delivery.
// The TaskID includes a minute-bucket of the target run time so that each
// scheduled execution is unique, self-enqueue doesn't collide with the
// currently-active task, and "Run Now" can always enqueue.
func NewScheduledReportRunTask(p ScheduledReportRunPayload, runAt time.Time) (*asynq.Task, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	taskID := fmt.Sprintf("scheduled-report-run-%s-%d", p.ReportID, runAt.Unix()/60)
	return asynq.NewTask(TypeScheduledReportRun, b,
		asynq.Queue(QueueScheduledReports),
		asynq.MaxRetry(3),
		asynq.TaskID(taskID),
	), nil
}

// EnqueueScheduledReportAt enqueues a scheduled report run to fire at a specific time.
// Duplicate tasks for the same report+time bucket are silently ignored.
func EnqueueScheduledReportAt(qc *asynq.Client, reportID, host string, runAt time.Time) error {
	if qc == nil {
		return nil
	}
	task, err := NewScheduledReportRunTask(ScheduledReportRunPayload{ReportID: reportID, Host: host}, runAt)
	if err != nil {
		return err
	}
	_, err = qc.Enqueue(task, asynq.ProcessAt(runAt))
	if err == asynq.ErrDuplicateTask || err == asynq.ErrTaskIDConflict {
		return nil
	}
	return err
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

// ProcessTask implements asynq.Handler.
func (h *ScheduledReportRunHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p ScheduledReportRunPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}
	d, err := h.resolveDB(ctx, t.Payload())
	if err != nil {
		if h.log != nil {
			h.log.Error("scheduled_report: tenant resolution failed", "report_id", p.ReportID, "error", err)
		}
		return err
	}
	rep, err := d.Queries.GetScheduledReportByID(ctx, p.ReportID)
	if err != nil {
		return nil
	}
	if !rep.Enabled {
		return nil
	}

	// Branding and the stale threshold come from the tenant's settings.
	branding := reports.Branding{}
	var staleAfter time.Duration
	if settings, sErr := d.Queries.GetFirstSettings(ctx); sErr == nil {
		baseURL := strings.TrimRight(settings.ServerUrl, "/")
		branding.ServerURL = baseURL
		if settings.LogoLight != nil && *settings.LogoLight != "" {
			branding.LogoURL = baseURL + *settings.LogoLight
		}
		if settings.UpdateInterval > 0 {
			staleAfter = 2 * time.Duration(settings.UpdateInterval) * time.Minute
		}
	}

	out, err := reports.Build(ctx, d, reports.BuildInput{
		ReportName: rep.Name,
		Definition: rep.Definition,
		Timezone:   rep.Timezone,
		Now:        time.Now(),
		StaleAfter: staleAfter,
		Branding:   branding,
		// CustomerMode arrives with increment D (recipients column).
	})
	if err != nil {
		h.insertRun(ctx, d, p.ReportID, "failed", err.Error(), "")
		if reports.IsConfigError(err) {
			// Deterministic: the same definition fails on every retry and the
			// hourly fallback would re-fire it forever. Record the failure once
			// per slot and move on to the next slot.
			if h.log != nil {
				h.log.Warn("scheduled_report: configuration error, skipping slot", "report_id", p.ReportID, "error", err)
			}
			h.advanceSchedule(ctx, d, rep, p.Host, time.Now())
			return nil
		}
		return err
	}
	subject, htmlBody, csvBody := out.Subject, out.HTML, out.CSV
	sum := sha256.Sum256([]byte(htmlBody + csvBody))
	sumHex := hex.EncodeToString(sum[:])

	var destIDs []string
	if len(rep.DestinationIds) > 0 {
		_ = json.Unmarshal(rep.DestinationIds, &destIDs)
	}
	if len(destIDs) == 0 {
		// Deterministic like a config error: fail this slot once and move on
		// instead of retrying and re-firing every hour.
		h.insertRun(ctx, d, p.ReportID, "failed", "no destinations configured", sumHex)
		if h.log != nil {
			h.log.Warn("scheduled_report: no destinations configured, skipping slot", "report_id", p.ReportID)
		}
		h.advanceSchedule(ctx, d, rep, p.Host, time.Now())
		return nil
	}

	for _, did := range destIDs {
		if strings.TrimSpace(did) == "" {
			continue
		}
		dest, err := d.Queries.GetNotificationDestinationByID(ctx, did)
		if err != nil || !dest.Enabled {
			continue
		}
		plain, err := decryptNotifConfig(h.enc, dest.ConfigEncrypted)
		if err != nil {
			continue
		}
		switch strings.ToLower(dest.ChannelType) {
		case "webhook":
			err = sendScheduledWebhook(ctx, plain, subject, htmlBody, csvBody)
		case "email":
			err = sendScheduledEmail(plain, subject, htmlBody, csvBody)
		case "ntfy":
			err = sendScheduledNtfy(ctx, plain, subject, htmlBody, csvBody)
		default:
			err = fmt.Errorf("unknown channel %q", dest.ChannelType)
		}
		if err != nil && h.log != nil {
			h.log.Error("scheduled_report: send failed", "destination_id", did, "error", err)
		}
	}

	h.insertRun(ctx, d, p.ReportID, "completed", "", sumHex)
	h.advanceSchedule(ctx, d, rep, p.Host, time.Now())
	return nil
}

// advanceSchedule stamps last_run_at, computes the next slot from the cron
// expression and self-enqueues it (event-driven chain).
func (h *ScheduledReportRunHandler) advanceSchedule(ctx context.Context, d *database.DB, rep db.ScheduledReport, host string, now time.Time) {
	tz := rep.Timezone
	if tz == "" {
		tz = "UTC"
	}
	next, nerr := notifications.NextCronRun(rep.CronExpr, tz, now)
	if nerr != nil {
		next = now.Add(24 * time.Hour)
	}
	_ = d.Queries.UpdateScheduledReportRunTimes(ctx, db.UpdateScheduledReportRunTimesParams{
		ID:        rep.ID,
		LastRunAt: pgtime.From(now),
		NextRunAt: pgtime.From(next),
	})
	if err := EnqueueScheduledReportAt(h.qc, rep.ID, host, next); err != nil && h.log != nil {
		h.log.Error("scheduled_report: failed to enqueue next run", "report_id", rep.ID, "next", next, "error", err)
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

func sendScheduledEmail(plain, subject, html, csv string) error {
	var cfg scheduledEmailConfig
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return err
	}
	if cfg.SMTPHost == "" || cfg.From == "" || cfg.To == "" {
		return fmt.Errorf("email smtp_host, from, to required")
	}
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 587
	}
	// Sanitize subject to prevent SMTP header injection.
	subject = strings.NewReplacer("\r", "", "\n", "").Replace(subject)
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s",
		cfg.From, cfg.To, subject, html))
	addr := cfg.SMTPHost + ":" + strconv.Itoa(cfg.SMTPPort)
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)
	}
	tlsCfg := &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12}

	// use_tls controls STARTTLS: when false, do not upgrade even if the server advertises it
	// (matches notification email delivery; see sendEmail in notification_worker.go).
	c, conn, err := func() (*smtp.Client, net.Conn, error) {
		plainConn, dialErr := net.DialTimeout("tcp", addr, 30*time.Second)
		if dialErr != nil {
			return nil, nil, dialErr
		}
		client, clientErr := smtp.NewClient(plainConn, cfg.SMTPHost)
		if clientErr != nil {
			_ = plainConn.Close()
			return nil, nil, clientErr
		}
		startTLS, _ := client.Extension("STARTTLS")
		if startTLS && cfg.UseTLS {
			if tlsErr := client.StartTLS(tlsCfg); tlsErr != nil {
				_ = client.Close()
				return nil, nil, tlsErr
			}
			return client, plainConn, nil
		}
		if cfg.UseTLS && !startTLS {
			_ = client.Close()
			tlsConn, tlsErr := tls.DialWithDialer(&net.Dialer{Timeout: 30 * time.Second}, "tcp", addr, tlsCfg)
			if tlsErr != nil {
				return nil, nil, tlsErr
			}
			client, clientErr = smtp.NewClient(tlsConn, cfg.SMTPHost)
			if clientErr != nil {
				_ = tlsConn.Close()
				return nil, nil, clientErr
			}
			return client, tlsConn, nil
		}
		return client, plainConn, nil
	}()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	defer func() { _ = c.Close() }()

	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(cfg.From); err != nil {
		return err
	}
	if err := c.Rcpt(cfg.To); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(msg)
	if err != nil {
		return err
	}
	return w.Close()
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
		title = "PatchMon scheduled report"
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
