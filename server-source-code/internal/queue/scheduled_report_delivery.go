package queue

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/reports"
	"github.com/PatchMon/PatchMon/server-source-code/internal/util"
	"github.com/jackc/pgx/v5"
)

// errDestinationInvalid: the delivery's destination is gone, disabled,
// unreadable or cannot address this recipient. Never retried.
var errDestinationInvalid = errors.New("destination missing or disabled")

// Customer-report SMTP destination problems. The handler answers them as 400
// with these texts; the worker records them as destination_invalid.
var (
	ErrCustomerSMTPUnreadable = errors.New("the SMTP destination config is unreadable")
	ErrCustomerSMTPNoTLS      = errors.New("customer reports require an SMTP destination with 'Use TLS' enabled")
	ErrCustomerSMTPNoSender   = errors.New("the SMTP destination has no valid sender address")
)

// CheckCustomerSMTPConfig validates the e-mail destination a customer report
// sends through: readable config, use_tls switched on (the dialer uses TLS,
// STARTTLS or implicit on 465, only then) and a valid sender address.
func CheckCustomerSMTPConfig(enc *util.Encryption, configEncrypted string) error {
	plain, err := decryptNotifConfig(enc, configEncrypted)
	if err != nil {
		return ErrCustomerSMTPUnreadable
	}
	var cfg scheduledEmailConfig
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return ErrCustomerSMTPUnreadable
	}
	return checkCustomerSMTP(cfg)
}

func checkCustomerSMTP(cfg scheduledEmailConfig) error {
	if !cfg.UseTLS {
		return ErrCustomerSMTPNoTLS
	}
	if _, err := reports.ParseMailbox(cfg.From); err != nil {
		return ErrCustomerSMTPNoSender
	}
	return nil
}

// recipientStillWanted re-checks a snapshotted e-mail recipient against the
// CURRENT report and destination, so a retry never mails an address the
// operator removed after the snapshot. Customer runs: the address must still
// be in the report's recipient list. Internal runs: it must still be the
// destination's To.
func recipientStillWanted(customer bool, rep db.ScheduledReport, cfg scheduledEmailConfig, recipient string) bool {
	want, err := reports.ParseMailbox(recipient)
	if err != nil {
		return false
	}
	if customer {
		if rep.ForkEmailRecipients == nil {
			return false
		}
		current, err := reports.ParseRecipients(rep.ForkEmailRecipients)
		if err != nil {
			return false
		}
		for _, a := range current {
			if a == want {
				return true
			}
		}
		return false
	}
	to, err := reports.ParseMailbox(cfg.To)
	return err == nil && to == want
}

// sendDelivery sends one archived run to one delivery target. Everything it
// sends comes from the archive snapshot (content), never from a re-render;
// only the recipient is re-checked against the current report (rep).
func (h *ScheduledReportRunHandler) sendDelivery(ctx context.Context, d *database.DB, rep db.ScheduledReport, del db.ForkReportDelivery, content db.ForkGetReportArchiveContentRow, loc *time.Location) error {
	dest, err := d.Queries.GetNotificationDestinationByID(ctx, del.DestinationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !dest.Enabled) {
		return errDestinationInvalid
	}
	if err != nil {
		return err
	}
	plain, err := decryptNotifConfig(h.enc, dest.ConfigEncrypted)
	if err != nil {
		return fmt.Errorf("%w: config unreadable", errDestinationInvalid)
	}
	switch del.Channel {
	case "email":
		var cfg scheduledEmailConfig
		if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
			return fmt.Errorf("%w: config unreadable", errDestinationInvalid)
		}
		if !recipientStillWanted(content.CustomerMode, rep, cfg, del.Recipient) {
			return fmt.Errorf("%w: recipient no longer configured", errDestinationInvalid)
		}
		// The sender frozen with the snapshot wins over the live config, so
		// a retry sends exactly what the archive says.
		if content.MailFrom != nil && strings.TrimSpace(*content.MailFrom) != "" {
			cfg.From = *content.MailFrom
		}
		// A customer run never falls back to an unencrypted session, even
		// when the destination was switched to plaintext after the snapshot.
		if content.CustomerMode {
			if err := checkCustomerSMTP(cfg); err != nil {
				return fmt.Errorf("%w: %s", errDestinationInvalid, err.Error())
			}
		}
		msg, err := reports.BuildMailMessage(reports.MailInput{
			From:      cfg.From,
			To:        del.Recipient,
			Subject:   content.Subject,
			HTML:      content.Html,
			PDF:       content.Pdf,
			PDFName:   reports.PDFFileName(content.ReportName, content.CreatedAt.In(loc)),
			Date:      time.Now(),
			MessageID: del.ID,
		})
		if err != nil {
			// A bad From/To or an unusable snapshot is a destination problem,
			// never a recipient-list configuration error of the report.
			return fmt.Errorf("%w: %s", errDestinationInvalid, reports.RedactError(err))
		}
		return sendReportEmail(ctx, cfg, del.Recipient, msg)
	case "webhook":
		return stripURLError(sendReportWebhook(ctx, plain, content.Subject, content.Html, content.Csv))
	case "ntfy":
		return stripURLError(sendReportNtfy(ctx, plain, content.Subject, content.Html, content.Csv))
	}
	return fmt.Errorf("%w: unsupported channel", errDestinationInvalid)
}

// stripURLError drops the URL from an HTTP client error: webhook and ntfy
// URLs carry secrets in their path and the text is stored and shown in the UI.
func stripURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// sendReportEmailSMTP delivers one prepared message to one recipient within
// ReportMailDeadline (dial, greeting, TLS, auth and data together). A
// shorter deadline on ctx wins.
func sendReportEmailSMTP(ctx context.Context, cfg scheduledEmailConfig, to string, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, ReportMailDeadline)
	defer cancel()
	if cfg.SMTPHost == "" {
		return fmt.Errorf("%w: smtp_host missing", errDestinationInvalid)
	}
	from, err := reports.ParseMailbox(cfg.From)
	if err != nil {
		return fmt.Errorf("%w: invalid from address", errDestinationInvalid)
	}
	rcpt, err := reports.ParseMailbox(to)
	if err != nil {
		return fmt.Errorf("%w: invalid recipient address", errDestinationInvalid)
	}
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 587
	}
	addr := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))
	tlsCfg := &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12}

	deadline, _ := ctx.Deadline()
	c, conn, err := dialSMTPUntil(ctx, addr, cfg.SMTPHost, cfg.UseTLS, implicitTLSPort(cfg.SMTPPort), tlsCfg, ReportMailDeadline, deadline)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	defer func() { _ = c.Close() }()
	// Cancelling the task context aborts a hanging session.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	if cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(rcpt); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	_ = c.Quit()
	return nil
}

// classifyDeliveryError maps a send error to a delivery error code.
func classifyDeliveryError(channel string, err error) string {
	var tp *textproto.Error
	var ne net.Error
	switch {
	case errors.Is(err, errDestinationInvalid):
		return reports.CodeDestinationInvalid
	case channel != "email":
		return reports.CodeDeliveryFailed
	case errors.As(err, &tp):
		switch {
		case tp.Code >= 400 && tp.Code < 500:
			return reports.CodeSMTPTemporary
		case tp.Code == 530, tp.Code == 534, tp.Code == 535, tp.Code == 538:
			return reports.CodeSMTPAuth
		}
		return reports.CodeSMTPRejected
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return reports.CodeSMTPTimeout
	}
	return reports.CodeSMTPConnect
}
