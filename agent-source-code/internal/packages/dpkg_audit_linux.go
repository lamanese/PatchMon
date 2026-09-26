//go:build linux

package packages

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// DpkgPackageState reports whether dpkg holds half-installed or unconfigured
// packages ("dpkg --audit"). known is false when the host has no dpkg, when the
// check could not run, or while another process holds the dpkg lock: a package
// operation in progress looks exactly like a broken one, and the next report
// after it will give the real answer.
//
// This is a read-only check. The agent never runs "dpkg --configure -a".
func DpkgPackageState() (broken bool, detail string, known bool) {
	dpkg, err := exec.LookPath("dpkg")
	if err != nil {
		return false, "", false
	}
	if dpkgLockHeld("/var/lib/dpkg/lock-frontend") || dpkgLockHeld("/var/lib/dpkg/lock") {
		return false, "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, dpkg, "--audit")
	cmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	var out bytes.Buffer
	cmd.Stdout = &out
	// dpkg --audit exits non-zero when it found problems, so the exit status is
	// not an error signal by itself; only a run without any usable result is.
	if err := cmd.Run(); err != nil && out.Len() == 0 {
		return false, "", false
	}
	names := parseDpkgAudit(out.String())
	if len(names) == 0 {
		return false, "", true
	}
	return true, formatDpkgAuditDetail(names), true
}

// dpkgLockHeld reports whether another process holds dpkg's fcntl lock on path.
func dpkgLockHeld(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	lk := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &lk); err != nil {
		return false
	}
	return lk.Type != syscall.F_UNLCK
}
