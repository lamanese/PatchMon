package reports

import (
	"errors"
	"regexp"
	"strings"
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
	redactSecret = regexp.MustCompile(`(?i)(password|passwd|pass|secret|token|authorization)(\s*[=:]\s*)\S+`)
)

// RedactError flattens an error to one line of at most MaxErrorMessage
// characters and masks anything that looks like a credential. It never
// receives MIME bodies or config JSON by contract; this is defence in depth.
func RedactError(err error) string {
	if err == nil {
		return ""
	}
	s := redactSpaces.ReplaceAllString(err.Error(), " ")
	s = redactSecret.ReplaceAllString(s, "$1$2***")
	s = strings.TrimSpace(s)
	if len(s) <= MaxErrorMessage {
		return s
	}
	// Cut on a rune boundary and leave room for the ellipsis so the final
	// byte length never exceeds MaxErrorMessage, even for multi-byte runes.
	const ellipsis = "…"
	budget := MaxErrorMessage - len(ellipsis)
	runes := []rune(s)
	cut := len(runes)
	for cut > 0 && len(string(runes[:cut])) > budget {
		cut--
	}
	return string(runes[:cut]) + ellipsis
}
