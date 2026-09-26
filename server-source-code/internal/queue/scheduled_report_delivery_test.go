package queue

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/reports"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestClassifyDeliveryError(t *testing.T) {
	cases := []struct {
		name, channel string
		err           error
		want          string
	}{
		{"destination gone", "email", fmt.Errorf("%w: x", errDestinationInvalid), reports.CodeDestinationInvalid},
		{"webhook destination gone", "webhook", errDestinationInvalid, reports.CodeDestinationInvalid},
		{"webhook status", "webhook", errors.New("webhook status 500"), reports.CodeDeliveryFailed},
		{"auth", "email", &textproto.Error{Code: 535, Msg: "bad credentials"}, reports.CodeSMTPAuth},
		{"rejected", "email", &textproto.Error{Code: 550, Msg: "no such user"}, reports.CodeSMTPRejected},
		{"421", "email", &textproto.Error{Code: 421, Msg: "try later"}, reports.CodeSMTPTemporary},
		{"450", "email", &textproto.Error{Code: 450, Msg: "mailbox busy"}, reports.CodeSMTPTemporary},
		{"451", "email", &textproto.Error{Code: 451, Msg: "local error"}, reports.CodeSMTPTemporary},
		{"452", "email", &textproto.Error{Code: 452, Msg: "insufficient storage"}, reports.CodeSMTPTemporary},
		{"deadline", "email", context.DeadlineExceeded, reports.CodeSMTPTimeout},
		{"net timeout", "email", &net.OpError{Op: "read", Err: timeoutErr{}}, reports.CodeSMTPTimeout},
		{"refused", "email", errors.New("dial tcp: connection refused"), reports.CodeSMTPConnect},
	}
	for _, c := range cases {
		if got := classifyDeliveryError(c.channel, c.err); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestRunErrorCodeMapsDeliveryConfigToDestinationInvalid(t *testing.T) {
	if got := runErrorCode(fmt.Errorf("%w: customer report needs one enabled e-mail destination", reports.ErrRecipients)); got != reports.CodeDestinationInvalid {
		t.Errorf("recipients: %s", got)
	}
	if got := runErrorCode(fmt.Errorf("%w: no enabled destinations", errNoDestinations)); got != reports.CodeDestinationInvalid {
		t.Errorf("no destinations: %s", got)
	}
	if got := runErrorCode(fmt.Errorf("x: %w", reports.ErrNoHosts)); got != reports.CodeNoHosts {
		t.Errorf("no hosts: %s", got)
	}
}

func TestStripURLErrorDropsWebhookPath(t *testing.T) {
	err := stripURLError(&url.Error{Op: "Post", URL: "https://hooks.slack.com/services/T1/B2/SECRET", Err: errors.New("dial tcp: connection refused")})
	stored := reports.RedactError(err)
	if strings.Contains(stored, "SECRET") || strings.Contains(stored, "/services/") {
		t.Fatalf("webhook path in stored text: %q", stored)
	}
	if classifyDeliveryError("webhook", err) != reports.CodeDeliveryFailed {
		t.Fatal("webhook errors stay delivery_failed")
	}
}

// A server that accepts the connection but never sends its greeting must not
// hold the mail past the deadline; the error is a timeout.
func TestSendReportEmailSMTPSilentServerTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() }) // hold open, never greet
		}
	}()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	cfg := scheduledEmailConfig{SMTPHost: host, SMTPPort: port, UseTLS: true, From: "reports@example.com"}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = sendReportEmailSMTP(ctx, cfg, "a@example.com", []byte("x"))
	if err == nil {
		t.Fatal("a silent server must fail")
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("greeting wait ignored the deadline: %v", el)
	}
	if code := classifyDeliveryError("email", err); code != reports.CodeSMTPTimeout {
		t.Fatalf("code %q (err %v)", code, err)
	}
}

func TestRedactDeliveryErrorMasksOwnRecipient(t *testing.T) {
	err := errors.New("550 5.1.1 <user@localhost>: unknown; USER@LOCALHOST rejected, other@example.com")
	got := redactDeliveryError(err, "user@localhost")
	if strings.Contains(strings.ToLower(got), "user@localhost") || strings.Contains(got, "other@example.com") {
		t.Fatalf("recipient leaked: %q", got)
	}
	if !strings.Contains(got, "***@***") {
		t.Fatalf("expected masked address: %q", got)
	}
	if redactDeliveryError(nil, "x@y") != "" {
		t.Fatal("nil error is empty")
	}
}

func TestRecipientStillWanted(t *testing.T) {
	var rep db.ScheduledReport
	rep.ForkEmailRecipients = []string{"A@example.com"}
	if !recipientStillWanted(true, rep, scheduledEmailConfig{}, "a@example.com") {
		t.Fatal("listed recipient is wanted")
	}
	if recipientStillWanted(true, rep, scheduledEmailConfig{}, "b@example.com") {
		t.Fatal("removed recipient is not wanted")
	}
	rep.ForkEmailRecipients = nil
	if recipientStillWanted(true, rep, scheduledEmailConfig{}, "a@example.com") {
		t.Fatal("a report switched to internal mails no customer")
	}
	if !recipientStillWanted(false, rep, scheduledEmailConfig{To: "Intern@example.com"}, "intern@example.com") ||
		recipientStillWanted(false, rep, scheduledEmailConfig{To: "neu@example.com"}, "intern@example.com") {
		t.Fatal("internal runs follow the destination's current To")
	}
}

func TestCheckCustomerSMTP(t *testing.T) {
	cases := []struct {
		cfg  scheduledEmailConfig
		want error
	}{
		{scheduledEmailConfig{SMTPPort: 587, UseTLS: true, From: "r@example.com"}, nil},
		{scheduledEmailConfig{SMTPPort: 465, UseTLS: true, From: "r@example.com"}, nil},
		{scheduledEmailConfig{SMTPPort: 465, From: "r@example.com"}, ErrCustomerSMTPNoTLS},
		{scheduledEmailConfig{SMTPPort: 587, From: "r@example.com"}, ErrCustomerSMTPNoTLS},
		{scheduledEmailConfig{SMTPPort: 587, UseTLS: true, From: ""}, ErrCustomerSMTPNoSender},
	}
	for _, c := range cases {
		if err := checkCustomerSMTP(c.cfg); !errors.Is(err, c.want) && err != c.want {
			t.Errorf("%+v: want %v got %v", c.cfg, c.want, err)
		}
	}
	if err := CheckCustomerSMTPConfig(nil, "{not json"); !errors.Is(err, ErrCustomerSMTPUnreadable) {
		t.Fatalf("unreadable: %v", err)
	}
}
