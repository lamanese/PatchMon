package packages

import (
	"regexp"
	"sort"
	"strings"
)

// aptFailureHint is one known apt failure with the commands that resolve it.
// The agent only prints them: every fix is run by an administrator in a
// terminal on the host, never by PatchMon.
type aptFailureHint struct {
	// match reports whether the run output shows this problem and may return
	// details taken from the output (package names, repository URLs).
	match    func(output string) (bool, []string)
	problem  string
	commands []string
	note     string
}

var (
	aptErrorsProcessingRe = regexp.MustCompile(`(?m)^Errors were encountered while processing:\n((?:[ \t]+\S+\n?)+)`)
	aptLockRe             = regexp.MustCompile(`Could not get lock (/var/lib/(?:dpkg|apt)/\S+)|Unable to acquire the dpkg frontend lock`)
	aptKeyRe              = regexp.MustCompile(`(?:NO_PUBKEY|EXPKEYSIG|KEYEXPIRED) ?([0-9A-F]{8,})?`)
	aptKeyRepoRe          = regexp.MustCompile(`(?m)^(?:W|E): (?:GPG error|An error occurred during the signature verification[^:]*): (\S+ \S+)`)
	aptNoReleaseRe        = regexp.MustCompile(`The repository '([^']+)' (?:does not have a Release file|no longer has a Release file|is not signed)`)
	aptResolveRe          = regexp.MustCompile(`Temporary failure resolving '([^']+)'|Could not resolve '([^']+)'`)
)

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func contains(sub string) func(string) (bool, []string) {
	return func(output string) (bool, []string) { return strings.Contains(output, sub), nil }
}

// aptFailureHints is ordered: the first entries are root causes, later ones are
// often only their consequence.
var aptFailureHints = []aptFailureHint{
	{
		match:    contains("No space left on device"),
		problem:  "The disk is full.",
		commands: []string{"df -h / /boot /var", "sudo apt-get clean", "sudo apt autoremove   # review the list first: removes old kernels and unused packages"},
		note:     "A full /boot is the usual cause on hosts that never removed old kernels. If dpkg was interrupted by this, run 'sudo dpkg --configure -a' after freeing space.",
	},
	{
		match: func(output string) (bool, []string) {
			if !aptLockRe.MatchString(output) {
				return false, nil
			}
			return true, nil
		},
		problem:  "Another package manager process holds the apt/dpkg lock (often unattended-upgrades or an open apt session).",
		commands: []string{"ps -eo pid,etime,cmd | grep -E '[a]pt|[d]pkg|[u]nattended'", "sudo fuser -v /var/lib/dpkg/lock-frontend"},
		note:     "Wait until that process has finished, then start the patch run again. Do not delete the lock files.",
	},
	{
		match: func(output string) (bool, []string) {
			if !strings.Contains(output, "end of file on stdin at conffile prompt") {
				return false, nil
			}
			return true, nil
		},
		problem:  "dpkg asked which version of a modified configuration file to keep and nobody could answer.",
		commands: []string{"sudo dpkg --configure -a", "sudo apt-get -f install"},
		note:     "When dpkg asks, keeping the local version (N, the default) leaves the current settings untouched. Agents from 2.0.14 on answer this automatically by keeping the local file.",
	},
	{
		match: func(output string) (bool, []string) {
			m := aptErrorsProcessingRe.FindStringSubmatch(output)
			if m == nil {
				return false, nil
			}
			return true, uniqueSorted(strings.Split(m[1], "\n"))
		},
		problem:  "These packages could not be configured and are only half installed:",
		commands: []string{"sudo dpkg --configure -a", "sudo apt-get -f install", "sudo dpkg --audit   # must print nothing afterwards"},
		note: "The real error is further up in this output, at the 'Setting up <package>' line of the first package listed. " +
			"If 'dpkg --configure -a' stops with the same error again, the installation script of that package itself is failing: " +
			"the line above 'Errors were encountered' says why, and the fix is specific to that package (its documentation or vendor). " +
			"Do not remove a package to get rid of the error unless you know nothing depends on it.",
	},
	{
		match:    contains("Unmet dependencies"),
		problem:  "Packages have unmet dependencies.",
		commands: []string{"sudo apt-get -f install"},
		note:     "Read the proposed changes before confirming: apt may want to remove packages to resolve this.",
	},
	{
		match:    contains("you have held broken packages"),
		problem:  "Held packages block the upgrade.",
		commands: []string{"apt-mark showhold", "sudo apt-mark unhold <package>   # only if the hold is no longer wanted"},
	},
	{
		match: func(output string) (bool, []string) {
			if !aptKeyRe.MatchString(output) {
				return false, nil
			}
			var repos []string
			for _, m := range aptKeyRepoRe.FindAllStringSubmatch(output, -1) {
				repos = append(repos, m[1])
			}
			return true, uniqueSorted(repos)
		},
		problem:  "The signing key of a repository is missing or has expired:",
		commands: []string{"sudo apt-get update   # shows the affected repository and key id"},
		note:     "Install the current key from the repository vendor's installation instructions. Do not disable signature checks.",
	},
	{
		match: func(output string) (bool, []string) {
			var repos []string
			for _, m := range aptNoReleaseRe.FindAllStringSubmatch(output, -1) {
				repos = append(repos, m[1])
			}
			return len(repos) > 0, uniqueSorted(repos)
		},
		problem:  "A repository no longer provides packages for this release (common for PPAs after the distribution release went end of life):",
		commands: []string{"grep -rn '<repository-host>' /etc/apt/sources.list /etc/apt/sources.list.d/"},
		note:     "Remove or replace that repository entry, then run 'sudo apt-get update'.",
	},
	{
		match: func(output string) (bool, []string) {
			var hosts []string
			for _, m := range aptResolveRe.FindAllStringSubmatch(output, -1) {
				hosts = append(hosts, m[1]+m[2])
			}
			return len(hosts) > 0, uniqueSorted(hosts)
		},
		problem:  "The host could not resolve these repository servers (DNS or network problem):",
		commands: []string{"resolvectl status | head -20", "getent hosts archive.ubuntu.com"},
	},
}

// AptFailureHints turns the output of a failed apt run into copyable help: for
// every known problem it names the cause and the commands an administrator has
// to run in a terminal on the host. PatchMon itself runs none of them. Returns
// "" when nothing is recognised.
func AptFailureHints(output string) string {
	// dpkg runs under a pseudo-terminal during a patch run, so parts of the
	// output arrive with CRLF line endings.
	output = strings.ReplaceAll(output, "\r\n", "\n")
	var b strings.Builder
	b.WriteString(AptReleaseInfoChangeHint(output))
	b.WriteString(AptDpkgInterruptedHint(output))
	for _, h := range aptFailureHints {
		ok, details := h.match(output)
		if !ok {
			continue
		}
		b.WriteString("\n[PatchMon] Known problem: " + h.problem + "\n")
		for _, d := range details {
			b.WriteString("  - " + d + "\n")
		}
		if len(h.commands) > 0 {
			b.WriteString("[PatchMon] Run in a terminal on the host:\n")
			for _, c := range h.commands {
				b.WriteString("  " + c + "\n")
			}
		}
		if h.note != "" {
			b.WriteString("[PatchMon] " + h.note + "\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String() + "[PatchMon] PatchMon does not run any of these commands itself. Start the patch run again once the host is repaired.\n"
}
