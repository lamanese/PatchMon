package packages

import (
	"strings"
	"testing"
)

func TestAptFailureHints(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name: "half installed package (aacpx11)",
			output: "Setting up fwupd (2.0.20-1ubuntu2~24.04.2) ...\n*** 85-fwupd (Y/I/N/O/D/Z) [default=N] ? dpkg: error processing package fwupd (--configure):\n end of file on stdin at conffile prompt\n" +
				"Errors were encountered while processing:\n fwupd\n php8.0-fpm\nneedrestart is being skipped since dpkg has failed\nE: Sub-process /usr/bin/dpkg returned an error code (1)\n",
			want: []string{"only half installed", "  - fwupd\n", "  - php8.0-fpm\n", "sudo dpkg --configure -a", "sudo dpkg --audit", "keeping the local version"},
		},
		{
			// Byte-exact from a real run on Staging: dpkg output arrives with CRLF.
			name:   "half installed package with CRLF line endings",
			output: "Setting up pm-demo-broken (1) ...\r\ndpkg: error processing package pm-demo-broken (--configure):\r\n installed pm-demo-broken package post-installation script subprocess returned error exit status 1\r\nErrors were encountered while processing:\r\n pm-demo-broken\r\nneedrestart is being skipped since dpkg has failed\nE: Sub-process /usr/bin/dpkg returned an error code (1)\n",
			want:   []string{"only half installed", "  - pm-demo-broken\n", "sudo dpkg --configure -a"},
		},
		{
			name:   "interrupted dpkg (aacdm03)",
			output: "E: dpkg was interrupted, you must manually run 'dpkg --configure -a' to correct the problem. \n",
			want:   []string{"sudo dpkg --configure -a", "sudo apt-get -f install"},
		},
		{
			name:   "lock held",
			output: "E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 2211 (unattended-upgr)\n",
			want:   []string{"holds the apt/dpkg lock", "fuser -v /var/lib/dpkg/lock-frontend", "Do not delete the lock files"},
		},
		{
			name:   "disk full",
			output: "dpkg: error processing archive x.deb (--unpack):\n cannot copy extracted data: failed to write (No space left on device)\n",
			want:   []string{"disk is full", "df -h / /boot /var", "sudo apt-get clean"},
		},
		{
			name:   "expired key",
			output: "W: GPG error: https://packages.example.org/apt stable InRelease: The following signatures were invalid: EXPKEYSIG 4F4EA0AAE5267A6C Some Vendor\n",
			want:   []string{"signing key", "https://packages.example.org/apt stable", "Do not disable signature checks"},
		},
		{
			name:   "repository gone",
			output: "E: The repository 'http://ppa.launchpad.net/ondrej/php/ubuntu focal Release' does not have a Release file.\n",
			want:   []string{"no longer provides packages", "ondrej/php", "sources.list.d"},
		},
		{
			name:   "dns",
			output: "W: Failed to fetch http://archive.ubuntu.com/ubuntu/dists/noble/InRelease  Temporary failure resolving 'archive.ubuntu.com'\n",
			want:   []string{"could not resolve", "  - archive.ubuntu.com\n", "resolvectl status"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AptFailureHints(tc.output)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			if !strings.Contains(got, "does not run any of these commands itself") {
				t.Errorf("hint block must state that PatchMon repairs nothing:\n%s", got)
			}
		})
	}
	if got := AptFailureHints("E: Unable to locate package does-not-exist\n"); got != "" {
		t.Errorf("unknown failure must not produce a hint, got:\n%s", got)
	}
}
