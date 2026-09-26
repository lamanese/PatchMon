package packages

import (
	"strings"
	"testing"
)

func TestAptUpdateArgs(t *testing.T) {
	args := strings.Join(AptUpdateArgs(), " ")
	for _, want := range []string{"update", "-qq", "Acquire::AllowReleaseInfoChange::Label=true", "Acquire::AllowReleaseInfoChange::Suite=true"} {
		if !strings.Contains(args, want) {
			t.Errorf("AptUpdateArgs() = %q, missing %q", args, want)
		}
	}
	// Origin and Codename must never be accepted automatically, and neither
	// may the blanket switch that would accept them.
	for _, forbidden := range []string{"Origin", "Codename", "--allow-releaseinfo-change", "AllowReleaseInfoChange=true"} {
		if strings.Contains(args, forbidden) {
			t.Errorf("AptUpdateArgs() = %q, must not contain %q", args, forbidden)
		}
	}
}

func TestAptReleaseInfoChangeHint(t *testing.T) {
	out := "E: Repository 'http://ppa.launchpad.net/ondrej/php/ubuntu focal InRelease' changed its 'Origin' value from 'LP-PPA-ondrej-php' to 'Someone else'\n" +
		"E: Repository 'http://ppa.launchpad.net/ondrej/php/ubuntu focal InRelease' changed its 'Origin' value from 'LP-PPA-ondrej-php' to 'Someone else'\n"
	hint := AptReleaseInfoChangeHint(out)
	for _, want := range []string{"ondrej/php", "Origin changed from 'LP-PPA-ondrej-php' to 'Someone else'", "apt-get update --allow-releaseinfo-change"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint missing %q:\n%s", want, hint)
		}
	}
	if n := strings.Count(hint, "Origin changed"); n != 1 {
		t.Errorf("duplicate apt lines must be reported once, got %d", n)
	}
	if got := AptReleaseInfoChangeHint("E: Could not get lock /var/lib/apt/lists/lock"); got != "" {
		t.Errorf("unrelated failure must not produce a hint, got %q", got)
	}
}

func TestAptKeepLocalConfigArgs(t *testing.T) {
	args := strings.Join(AptKeepLocalConfigArgs(), " ")
	for _, want := range []string{"Dpkg::Options::=--force-confdef", "Dpkg::Options::=--force-confold"} {
		if !strings.Contains(args, want) {
			t.Errorf("AptKeepLocalConfigArgs() = %q, missing %q", args, want)
		}
	}
	// confnew would overwrite configuration files the admin has edited.
	if strings.Contains(args, "confnew") {
		t.Errorf("AptKeepLocalConfigArgs() = %q must never replace local configuration files", args)
	}
}

func TestAptDpkgInterruptedHint(t *testing.T) {
	out := "E: dpkg was interrupted, you must manually run 'dpkg --configure -a' to correct the problem. \n"
	hint := AptDpkgInterruptedHint(out)
	for _, want := range []string{"sudo dpkg --configure -a", "sudo apt-get -f install", "does not repair this automatically"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint missing %q:\n%s", want, hint)
		}
	}
	if got := AptDpkgInterruptedHint("E: Unable to locate package foo"); got != "" {
		t.Errorf("unrelated failure must not produce a hint, got %q", got)
	}
}
