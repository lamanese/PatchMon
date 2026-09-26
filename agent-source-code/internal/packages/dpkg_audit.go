package packages

import (
	"sort"
	"strings"
)

// maxDpkgAuditPackages caps how many package names are reported; the hint is
// for a human, who will run "dpkg --audit" on the host anyway.
const maxDpkgAuditPackages = 20

// parseDpkgAudit extracts the affected package names from "dpkg --audit"
// output. dpkg prints one explanatory paragraph per problem class followed by
// the packages, each on a line starting with a single space:
//
//	The following packages have been unpacked but not yet configured.
//	They must be configured using dpkg --configure or the configure
//	menu option in dselect for them to work:
//	 fwupd                Firmware update daemon
func parseDpkgAudit(output string) []string {
	seen := map[string]bool{}
	var names []string
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, " ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Multi-arch packages are listed as name:arch.
		name := strings.SplitN(fields[0], ":", 2)[0]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// formatDpkgAuditDetail renders the package list for the report.
func formatDpkgAuditDetail(names []string) string {
	if len(names) > maxDpkgAuditPackages {
		rest := len(names) - maxDpkgAuditPackages
		return strings.Join(names[:maxDpkgAuditPackages], ", ") + " (+" + itoa(rest) + " more)"
	}
	return strings.Join(names, ", ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
