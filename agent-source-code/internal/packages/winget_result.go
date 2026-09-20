package packages

import (
	"fmt"
	"strings"
)

// wingetNotFoundSentinel is the marker line the PowerShell winget-resolve
// block (wingetResolveBlock in windows_patch.go) writes when winget.exe
// cannot be located, following the same WINGET_NOT_FOUND convention already
// used by getPackagesFromWinget in windows.go.
const wingetNotFoundSentinel = "WINGET_NOT_FOUND"

// isWinGetNotFound reports whether output is the sentinel line the
// PowerShell winget-resolve block writes when winget.exe cannot be located.
func isWinGetNotFound(output string) bool {
	return strings.TrimSpace(output) == wingetNotFoundSentinel
}

// interpretWinGetUpgradeAllResult turns the raw output/error from a winget
// upgrade --all invocation into the (output, error) pair callers should see.
// Windows Server has no WinGet component, so a missing winget.exe is an
// expected condition during a patch-ALL run: it must read as a normal skip,
// not an error, and must not affect the run's success/failure status.
func interpretWinGetUpgradeAllResult(output string, err error) (string, error) {
	if isWinGetNotFound(output) {
		return "[WinGet] winget.exe not found - skipping application upgrades", nil
	}
	if err != nil {
		return output, fmt.Errorf("winget upgrade --all failed: %w", err)
	}
	return output, nil
}

// interpretWinGetUpgradePackageResult mirrors interpretWinGetUpgradeAllResult
// for the per-package upgrade path. There, a missing winget.exe stays an
// error exactly as before: the user explicitly asked to upgrade this
// specific WinGet package, so it cannot be silently skipped. The output text
// reproduces the pre-sentinel "ERROR:" message so operators see the same
// thing they always have for this path.
func interpretWinGetUpgradePackageResult(packageID, output string, err error) (string, error) {
	if isWinGetNotFound(output) {
		return "ERROR:winget.exe not found", fmt.Errorf("winget upgrade --id %s failed: winget.exe not found", packageID)
	}
	if err != nil {
		return output, fmt.Errorf("winget upgrade --id %s failed: %w", packageID, err)
	}
	return output, nil
}
