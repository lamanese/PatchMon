package packages

import (
	"fmt"
	"regexp"
	"strings"
)

// AptUpdateArgs returns the arguments for a non-interactive "apt-get update".
//
// apt refuses to refresh a repository whose Release file changed one of its
// identifying fields until a human confirms it, which an agent cannot do: the
// run ends with exit status 100. Label is cosmetic (a PPA owner renaming the
// repository) and Suite changes on every Debian release (stable -> oldstable)
// on every host at once, so both are accepted. Origin and Codename stay
// blocked: Origin drives pinning and unattended-upgrades patterns, and a new
// Codename means the repository now serves a different release.
// apt older than 1.8.1 does not know these options and ignores them.
func AptUpdateArgs() []string {
	return []string{
		"update", "-qq",
		"-o", "Acquire::AllowReleaseInfoChange::Label=true",
		"-o", "Acquire::AllowReleaseInfoChange::Suite=true",
	}
}

var aptReleaseInfoChangeRe = regexp.MustCompile(`Repository '([^']+)' changed its '([A-Za-z]+)' value from '([^']*)' to '([^']*)'`)

// AptReleaseInfoChangeHint explains an "apt-get update" failure caused by a
// release info change that AptUpdateArgs deliberately does not accept. Returns
// "" when the output holds no such message.
func AptReleaseInfoChangeHint(output string) string {
	matches := aptReleaseInfoChangeRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n[PatchMon] apt refused to refresh a repository because its release information changed and needs a manual confirmation:\n")
	seen := map[string]bool{}
	for _, m := range matches {
		line := fmt.Sprintf("  - %s: %s changed from '%s' to '%s'\n", m[1], m[2], m[3], m[4])
		if seen[line] {
			continue
		}
		seen[line] = true
		b.WriteString(line)
	}
	b.WriteString("[PatchMon] Check that the change is expected, then accept it once on the host with:\n")
	b.WriteString("  sudo apt-get update --allow-releaseinfo-change\n")
	b.WriteString("[PatchMon] Label and Suite changes are accepted automatically; Origin and Codename changes are not.\n")
	return b.String()
}
