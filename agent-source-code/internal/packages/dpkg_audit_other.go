//go:build !linux

package packages

// DpkgPackageState is only meaningful on dpkg-based Linux hosts.
func DpkgPackageState() (broken bool, detail string, known bool) {
	return false, "", false
}
