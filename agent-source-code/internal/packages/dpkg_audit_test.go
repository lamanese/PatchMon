package packages

import (
	"strings"
	"testing"
)

func TestParseDpkgAudit(t *testing.T) {
	out := `The following packages have been unpacked but not yet configured.
They must be configured using dpkg --configure or the configure
menu option in dselect for them to work:
 fwupd                Firmware update daemon
 libfoo1:amd64        Some library

The following packages are only half configured, probably due to problems
configuring them the first time.  The configuration should be retried using
dpkg --configure <package> or the configure menu option in dselect:
 php8.0-fpm           server-side, HTML-embedded scripting language
 fwupd                Firmware update daemon
`
	got := parseDpkgAudit(out)
	want := []string{"fwupd", "libfoo1", "php8.0-fpm"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("parseDpkgAudit = %v, want %v", got, want)
	}
	if names := parseDpkgAudit(""); len(names) != 0 {
		t.Fatalf("clean host must yield no packages, got %v", names)
	}
}

func TestFormatDpkgAuditDetail(t *testing.T) {
	if got := formatDpkgAuditDetail([]string{"a", "b"}); got != "a, b" {
		t.Fatalf("got %q", got)
	}
	var many []string
	for i := 0; i < maxDpkgAuditPackages+3; i++ {
		many = append(many, "pkg")
	}
	if got := formatDpkgAuditDetail(many); !strings.HasSuffix(got, "(+3 more)") {
		t.Fatalf("long lists must be capped, got %q", got)
	}
}
