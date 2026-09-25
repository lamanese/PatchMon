package queue

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"testing"

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
