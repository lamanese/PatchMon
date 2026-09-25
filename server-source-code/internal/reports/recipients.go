package reports

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// MaxRecipients bounds a customer report's recipient list (spec §7).
const MaxRecipients = 10

// ErrRecipients marks every recipient validation error (a configuration error).
var ErrRecipients = errors.New("recipients")

// ParseRecipients normalises a customer report's recipients: trimmed, one
// mailbox each, lowercase, duplicates dropped, 1..MaxRecipients. A nil input
// means "internal report" and stays nil; an empty non-nil list is an error.
func ParseRecipients(in []string) ([]string, error) {
	if in == nil {
		return nil, nil
	}
	if len(in) == 0 {
		return nil, fmt.Errorf("%w: at least one recipient is required", ErrRecipients)
	}
	if len(in) > MaxRecipients {
		return nil, fmt.Errorf("%w: at most %d recipients", ErrRecipients, MaxRecipients)
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for i, raw := range in {
		addr, err := ParseMailbox(raw)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i+1, err)
		}
		if seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out, nil
}

// ParseMailbox accepts exactly one e-mail address (optionally with a display
// name) and returns the bare lowercase address. Lists, empty input and
// control characters are rejected so nothing can reach an SMTP header line.
func ParseMailbox(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%w: empty address", ErrRecipients)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: control character in address", ErrRecipients)
		}
	}
	if strings.ContainsAny(s, ",;") {
		return "", fmt.Errorf("%w: one address per entry", ErrRecipients)
	}
	a, err := mail.ParseAddress(s)
	if err != nil {
		return "", fmt.Errorf("%w: invalid address", ErrRecipients)
	}
	addr := strings.ToLower(a.Address)
	if !strings.Contains(addr, "@") {
		return "", fmt.Errorf("%w: invalid address", ErrRecipients)
	}
	return addr, nil
}
