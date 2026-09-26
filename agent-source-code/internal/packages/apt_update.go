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

// AptKeepLocalConfigArgs returns the dpkg options for an unattended real run.
//
// When an upgrade ships a new version of a configuration file the admin has
// edited, dpkg asks which one to keep. A patch run has no terminal, so dpkg
// reads end-of-file, fails the package and leaves it half-configured; every
// later apt call then stops with "dpkg was interrupted". With these options
// dpkg never asks: confdef takes the package default where one exists, and
// confold keeps the locally modified file otherwise. A configuration file is
// never overwritten; the new version is left next to it as .dpkg-dist.
func AptKeepLocalConfigArgs() []string {
	return []string{
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
	}
}

// AptDpkgInterruptedHint explains a run that failed because an earlier dpkg
// run on the host was interrupted. The agent deliberately does not repair this
// itself: "dpkg --configure -a" can stop for decisions only an administrator
// at a terminal should take. Returns "" when the output holds no such message.
func AptDpkgInterruptedHint(output string) string {
	if !strings.Contains(output, "dpkg was interrupted") {
		return ""
	}
	return "\n[PatchMon] An earlier package installation on this host was interrupted, so apt refuses to continue.\n" +
		"[PatchMon] PatchMon does not repair this automatically. An administrator has to finish it in a terminal on the host:\n" +
		"  sudo dpkg --configure -a\n" +
		"  sudo apt-get -f install\n" +
		"[PatchMon] If dpkg asks about a modified configuration file, keeping the local version leaves the current settings untouched.\n" +
		"[PatchMon] Start the patch run again afterwards.\n"
}
