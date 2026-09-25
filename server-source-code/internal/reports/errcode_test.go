package reports

import (
	"errors"
	"fmt"
	"strings"
	"testing"
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
	for _, c := range []string{CodeSMTPConnect, CodeSMTPTimeout, CodeDeliveryFailed} {
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
