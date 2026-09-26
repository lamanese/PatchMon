package reports

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildErrorCodeMapsKnownErrors(t *testing.T) {
	cases := map[string]error{
		CodeDefinitionInvalid: fmt.Errorf("%w: bad", ErrDefinition),
		CodeScopeInvalid:      fmt.Errorf("%w: group", ErrScopeInvalid),
		CodeNoHosts:           ErrNoHosts,
		CodeTooManyHosts:      ErrTooManyHosts,
		CodePDFTooLarge:       fmt.Errorf("render pdf: %w", ErrPDFTooLarge),
		CodeRenderFailed:      errors.New("connection refused"),
	}
	for want, err := range cases {
		if got := BuildErrorCode(err); got != want {
			t.Errorf("%v: want %s got %s", err, want, got)
		}
	}
}

func TestRetryableCodes(t *testing.T) {
	for _, c := range []string{CodeSMTPConnect, CodeSMTPTimeout, CodeSMTPTemporary, CodeDeliveryFailed} {
		if !RetryableCode(c) {
			t.Errorf("%s must be retryable", c)
		}
	}
	for _, c := range []string{CodeSMTPAuth, CodeSMTPRejected, CodeDestinationInvalid, CodeScopeInvalid, CodeAbandoned} {
		if RetryableCode(c) {
			t.Errorf("%s must not be retryable", c)
		}
	}
}

func TestRedactErrorTruncatesFlattensAndMasks(t *testing.T) {
	long := strings.Repeat("x", 400)
	got := RedactError(errors.New("line1\r\nline2 password=secret123 " + long))
	if len(got) > MaxErrorMessage {
		t.Fatalf("len %d", len(got))
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Fatal("newlines must be flattened")
	}
	if strings.Contains(got, "secret123") {
		t.Fatal("password value must be masked")
	}
	if RedactError(nil) != "" {
		t.Fatal("nil error is empty")
	}
}

func TestRedactErrorMasksCredentialForms(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		secret string
	}{
		{"password= form", "password=secret123", "secret123"},
		{"password: form", "password: mysecretvalue", "mysecretvalue"},
		{"Password= mixed case", "Password=TopSecret1", "TopSecret1"},
		{"authorization bearer", "Authorization: Bearer abc123token", "abc123token"},
		{"authorization basic", "Authorization: Basic QWxhZGRpbjpvcGVuc2VzYW1l", "QWxhZGRpbjpvcGVuc2VzYW1l"},
		{"json quoted password", `"password":"topsecretvalue"`, "topsecretvalue"},
		{"quoted value with embedded space", `password = "a topsecret b"`, "a topsecret b"},
		{"url credentials", "smtp://smtpuser:hunter2pw@smtp.example.com:465", "hunter2pw"},
		{"api_key", "api_key=abcdef123456", "abcdef123456"},
		{"apikey no underscore", "apikey=abcdef123456", "abcdef123456"},
		{"pwd short key", "pwd=hunter2pw", "hunter2pw"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RedactError(errors.New(c.in))
			if strings.Contains(got, c.secret) {
				t.Fatalf("secret %q leaked in %q", c.secret, got)
			}
		})
	}
}

func TestRedactErrorTruncatesLargeMultiByteInputEfficiently(t *testing.T) {
	// A >=1 MB error with a multi-byte rune sitting exactly on the byte
	// budget boundary: this must not split the rune, must not scan the whole
	// message to find the cut (the old backward-shrinking loop was
	// quadratic), and must still respect MaxErrorMessage.
	const budget = MaxErrorMessage - len("…")
	var b strings.Builder
	b.WriteString(strings.Repeat("x", budget-1))
	b.WriteString("€") // 3-byte rune straddling the cut boundary
	b.WriteString(strings.Repeat("y", 1<<20))
	got := RedactError(errors.New(b.String()))
	if len(got) > MaxErrorMessage {
		t.Fatalf("len %d exceeds %d", len(got), MaxErrorMessage)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatal("result must be valid UTF-8: a rune must not be split")
	}
}

func TestRedactErrorDropsURLPathsAndAddresses(t *testing.T) {
	got := RedactError(errors.New(`Post "https://hooks.slack.com/services/T1/B2/SECRET": dial tcp`))
	if strings.Contains(got, "SECRET") || strings.Contains(got, "T1/B2") {
		t.Fatalf("webhook path leaked: %q", got)
	}
	if !strings.Contains(got, "https://hooks.slack.com") {
		t.Fatalf("scheme and host must stay: %q", got)
	}
	got = RedactError(errors.New("550 5.1.1 <kunde@example.com>: Recipient address rejected"))
	if strings.Contains(got, "kunde") || strings.Contains(got, "example.com") {
		t.Fatalf("address leaked: %q", got)
	}
	if !strings.Contains(got, "550 5.1.1") || !strings.Contains(got, "***@***") {
		t.Fatalf("reply code must stay, address masked: %q", got)
	}
}
