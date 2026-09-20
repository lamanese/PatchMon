package packages

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsWinGetNotFound(t *testing.T) {
	assert.True(t, isWinGetNotFound("WINGET_NOT_FOUND"))
	assert.True(t, isWinGetNotFound("  WINGET_NOT_FOUND\r\n"))
	assert.False(t, isWinGetNotFound(""))
	assert.False(t, isWinGetNotFound("some other output"))
}

func TestInterpretWinGetUpgradeAllResult_NotFound(t *testing.T) {
	// Windows Server has no WinGet component; a patch-ALL run must treat this
	// as a normal skip, not an error, and must not surface "ERROR:" text.
	psErr := errors.New("exit status 1")
	out, err := interpretWinGetUpgradeAllResult("WINGET_NOT_FOUND", psErr)
	require.NoError(t, err)
	assert.Equal(t, "[WinGet] winget.exe not found - skipping application upgrades", out)
	assert.NotContains(t, out, "ERROR")
}

func TestInterpretWinGetUpgradeAllResult_Success(t *testing.T) {
	out, err := interpretWinGetUpgradeAllResult("Nothing to upgrade found.", nil)
	require.NoError(t, err)
	assert.Equal(t, "Nothing to upgrade found.", out)
}

func TestInterpretWinGetUpgradeAllResult_RealFailure(t *testing.T) {
	psErr := errors.New("exit status 1")
	out, err := interpretWinGetUpgradeAllResult("some winget error output", psErr)
	require.Error(t, err)
	assert.Equal(t, "some winget error output", out)
}

func TestInterpretWinGetUpgradePackageResult_NotFound(t *testing.T) {
	// The user explicitly asked to upgrade a specific package here, so a
	// missing winget.exe stays a real error.
	psErr := errors.New("exit status 1")
	out, err := interpretWinGetUpgradePackageResult("Some.Package", "WINGET_NOT_FOUND", psErr)
	require.Error(t, err)
	assert.Equal(t, "ERROR:winget.exe not found", out)
}

func TestInterpretWinGetUpgradePackageResult_Success(t *testing.T) {
	out, err := interpretWinGetUpgradePackageResult("Some.Package", "Successfully installed", nil)
	require.NoError(t, err)
	assert.Equal(t, "Successfully installed", out)
}

func TestInterpretWinGetUpgradePackageResult_RealFailure(t *testing.T) {
	psErr := errors.New("exit status 1")
	out, err := interpretWinGetUpgradePackageResult("Some.Package", "some winget error output", psErr)
	require.Error(t, err)
	assert.Equal(t, "some winget error output", out)
}
