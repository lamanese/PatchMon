package reports

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRecipientsNormalisesAndDedupes(t *testing.T) {
	got, err := ParseRecipients([]string{" Kunde <Kunde@Example.com> ", "kunde@example.com", "ops@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "kunde@example.com,ops@example.org" {
		t.Fatalf("got %v", got)
	}
}

func TestParseRecipientsNilMeansInternal(t *testing.T) {
	got, err := ParseRecipients(nil)
	if err != nil || got != nil {
		t.Fatalf("nil must stay nil: %v %v", got, err)
	}
}

func TestParseRecipientsRejectsEmptyTooManyAndInvalid(t *testing.T) {
	cases := [][]string{
		{},
		make([]string, MaxRecipients+1),
		{"not-an-address"},
		{"a@b.example, c@d.example"},
	}
	for i := range cases[1] {
		cases[1][i] = "u" + string(rune('a'+i)) + "@example.com"
	}
	for _, c := range cases {
		if _, err := ParseRecipients(c); !errors.Is(err, ErrRecipients) {
			t.Errorf("%v: want ErrRecipients, got %v", c, err)
		}
	}
	// The archive/log error text must never echo the recipient address
	// (global constraint: no recipient addresses in stored error texts).
	if _, err := ParseRecipients([]string{"not-an-address"}); err == nil || strings.Contains(err.Error(), "not-an-address") {
		t.Fatalf("error text must not contain the recipient address: %v", err)
	}
}

func TestParseRecipientsRejectsInjectionAndLists(t *testing.T) {
	for _, s := range []string{
		"\"Kunde\" <kunde@example.com>\r\nBcc: x@y.example",
		"kunde@example.com\nBcc: x@y.example",
		"a@b.example; c@d.example",
		"kunde@exam\x00ple.com",
	} {
		if _, err := ParseMailbox(s); err == nil {
			t.Errorf("%q must be rejected", s)
		}
	}
}

func TestRecipientsErrorIsConfigError(t *testing.T) {
	_, err := ParseRecipients([]string{})
	if !IsConfigError(err) {
		t.Fatal("recipient errors are configuration errors (400, no retry)")
	}
}
