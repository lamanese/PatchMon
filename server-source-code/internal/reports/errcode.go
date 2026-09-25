package reports

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Error codes stored in fork_report_archive.error_code and
// fork_report_deliveries.error_code (spec §7, fixed list).
const (
	CodeDefinitionInvalid  = "definition_invalid"
	CodeScopeInvalid       = "scope_invalid"
	CodeNoHosts            = "no_hosts"
	CodeTooManyHosts       = "too_many_hosts"
	CodeRenderFailed       = "render_failed"
	CodePDFTooLarge        = "pdf_too_large"
	CodeSMTPConnect        = "smtp_connect"
	CodeSMTPAuth           = "smtp_auth"
	CodeSMTPRejected       = "smtp_rejected"
	CodeSMTPTimeout        = "smtp_timeout"
	CodeDestinationInvalid = "destination_invalid"
	CodeDeliveryFailed     = "delivery_failed"
	CodeAbandoned          = "abandoned"
)

// MaxErrorMessage bounds every stored error text.
const MaxErrorMessage = 300

// BuildErrorCode classifies an error returned by Build.
func BuildErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrDefinition):
		return CodeDefinitionInvalid
	case errors.Is(err, ErrScopeInvalid):
		return CodeScopeInvalid
	case errors.Is(err, ErrNoHosts):
		return CodeNoHosts
	case errors.Is(err, ErrTooManyHosts):
		return CodeTooManyHosts
	case errors.Is(err, ErrPDFTooLarge):
		return CodePDFTooLarge
	}
	return CodeRenderFailed
}

// RetryableCode reports whether a delivery with this code may succeed on a
// later attempt (transport problems yes, credentials and rejections no).
func RetryableCode(code string) bool {
	switch code {
	case CodeSMTPConnect, CodeSMTPTimeout, CodeDeliveryFailed:
		return true
	}
	return false
}

var (
	redactSpaces = regexp.MustCompile(`\s+`)

	// redactSecret matches "<key><sep><value>" for a fixed list of
	// credential-shaped keys. The key and an optional wrapping quote may sit
	// on either side of the separator (covers JSON's `"password":"x"`); an
	// optional "Bearer "/"Basic " scheme is absorbed into the value so an
	// Authorization header's token is masked too; the value itself is either
	// a quoted string (so an embedded space, as in `password = "a b"`,
	// doesn't leak the part after the space) or a run of non-space bytes.
	redactSecret = regexp.MustCompile(`(?i)(password|passwd|pass|secret|token|authorization|api_key|apikey|pwd)("?\s*[=:]\s*)(?:(?:bearer|basic)\s+)?(?:"[^"]*"|'[^']*'|\S+)`)

	// redactURLCreds masks a userinfo credential embedded in a URL, e.g.
	// smtp://user:hunter2@host -> smtp://***:***@host.
	redactURLCreds = regexp.MustCompile(`://[^/\s:@]+:[^@\s]+@`)
)

// RedactError flattens an error to one line of at most MaxErrorMessage
// characters and masks anything that looks like a credential. It never
// receives MIME bodies or config JSON by contract; this is defence in depth.
func RedactError(err error) string {
	if err == nil {
		return ""
	}
	s := redactSpaces.ReplaceAllString(err.Error(), " ")
	s = redactURLCreds.ReplaceAllString(s, "://***:***@")
	s = redactSecret.ReplaceAllString(s, "$1$2***")
	s = strings.TrimSpace(s)
	if len(s) <= MaxErrorMessage {
		return s
	}
	// Walk forward once, summing rune byte-lengths, and cut as soon as the
	// budget (MaxErrorMessage minus the ellipsis) would be exceeded. This is
	// O(cut position) rather than the O(n^2) a backward-shrinking loop over
	// string(runes[:cut]) would cost on a large error message, and the cut
	// always lands on a rune boundary because it stops at a range-loop index.
	const ellipsis = "…"
	budget := MaxErrorMessage - len(ellipsis)
	n := 0
	for i, r := range s {
		rl := utf8.RuneLen(r)
		if n+rl > budget {
			return s[:i] + ellipsis
		}
		n += rl
	}
	return s
}
