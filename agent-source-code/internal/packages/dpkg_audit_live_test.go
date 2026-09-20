//go:build linux

package packages

import (
	"os"
	"strings"
	"testing"
)

// TestDpkgPackageStateLive runs against the real dpkg of the machine it runs
// on. It is opt-in (PM_TEST_DPKG_LIVE names the package that was deliberately
// left half-configured), because it needs a throwaway container:
//
//	dpkg -i broken.deb   # postinst exits 1 -> package stays half-configured
//	PM_TEST_DPKG_LIVE=demo-broken go test ./internal/packages/ -run DpkgPackageStateLive
func TestDpkgPackageStateLive(t *testing.T) {
	want := os.Getenv("PM_TEST_DPKG_LIVE")
	if want == "" {
		t.Skip("PM_TEST_DPKG_LIVE not set; skipping live dpkg test")
	}
	broken, detail, known := DpkgPackageState()
	if !known {
		t.Fatal("state must be known on a dpkg host without a held lock")
	}
	if !broken || !strings.Contains(detail, want) {
		t.Fatalf("got broken=%v detail=%q, want broken with %q", broken, detail, want)
	}
}
