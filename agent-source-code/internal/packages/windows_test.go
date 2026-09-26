package packages

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// TestWingetNeedsUpdate covers the decision of whether a winget-discovered
// package should be flagged as NeedsUpdate. winget's own --upgrade-available
// pass deliberately excludes packages whose installed version it cannot
// determine (e.g. "Unknown"), so an unknown installed version must never be
// treated as outdated purely because it string-differs from the catalogue's
// "Available" column.
func TestWingetNeedsUpdate(t *testing.T) {
	tests := []struct {
		name         string
		installed    string
		available    string
		inUpgradeMap bool
		want         bool
	}{
		{
			name:         "in upgrade map wins regardless of version",
			installed:    "unknown",
			available:    "17.0.1000.7",
			inUpgradeMap: true,
			want:         true,
		},
		{
			name:         "in upgrade map even when versions look equal",
			installed:    "1.2.3",
			available:    "1.2.3",
			inUpgradeMap: true,
			want:         true,
		},
		{
			name:         "unknown lowercase installed, not in map",
			installed:    "unknown",
			available:    "17.0.1000.7",
			inUpgradeMap: false,
			want:         false,
		},
		{
			name:         "Unknown capitalized installed, not in map",
			installed:    "Unknown",
			available:    "17.0.1000.7",
			inUpgradeMap: false,
			want:         false,
		},
		{
			name:         "empty installed, not in map",
			installed:    "",
			available:    "17.0.1000.7",
			inUpgradeMap: false,
			want:         false,
		},
		{
			name:         "localised German placeholder, not in map",
			installed:    "Unbekannt",
			available:    "17.0.1000.7",
			inUpgradeMap: false,
			want:         false,
		},
		{
			name:         "known version differs from available, not in map",
			installed:    "1.0.0",
			available:    "2.0.0",
			inUpgradeMap: false,
			want:         true,
		},
		{
			name:         "known version equals available, not in map",
			installed:    "1.0.0",
			available:    "1.0.0",
			inUpgradeMap: false,
			want:         false,
		},
		{
			name:         "available empty, not in map",
			installed:    "1.0.0",
			available:    "",
			inUpgradeMap: false,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wingetNeedsUpdate(tt.installed, tt.available, tt.inUpgradeMap)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsUnknownVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"", true},
		{"unknown", true},
		{"Unknown", true},
		{"UNKNOWN", true},
		{"  unknown  ", true},
		{"Unbekannt", true},
		{"unbekannt", true},
		{"1.2.3", false},
		{"17.0.1135.8", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			assert.Equal(t, tt.want, isUnknownVersion(tt.version))
		})
	}
}

// TestGetPackagesFromWinget_UnknownInstalledVersionNotInUpgradeMap reproduces
// the real-world sql1 case end-to-end through parseWingetTable + the winget
// packaging pass: an installed version winget cannot determine ("Unknown")
// must not be flagged NeedsUpdate just because it differs from the
// catalogue's Available column, when winget's own --upgrade-available pass
// (here: empty upgradeMap) does not list the package as upgradable.
func TestGetPackagesFromWinget_UnknownInstalledVersionNotInUpgradeMap(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	m := NewWindowsManager(logger)

	table := `Name                                    Id                            Version   Available    Source
---------------------------------------------------------------------------------------------------
Microsoft SQL Server 2025 (64-bit)      Microsoft.SQLServer2025       Unknown   17.0.1000.7  winget
`
	entries := m.parseWingetTable(table)
	assert.Len(t, entries, 1)
	assert.Equal(t, "Unknown", entries[0].Version)
	assert.Equal(t, "17.0.1000.7", entries[0].Available)

	// Simulate: winget list --upgrade-available correctly excludes this
	// package because it cannot determine the installed version.
	upgradeMap := map[string]string{}

	e := entries[0]
	version := stripEllipsis(e.Version)
	avail := stripEllipsis(e.Available)
	id := stripEllipsis(e.ID)
	up, inUpgradeMap := upgradeMap[id]
	if inUpgradeMap && avail == "" {
		avail = up
	}
	needsUpdate := wingetNeedsUpdate(version, avail, inUpgradeMap)

	assert.False(t, needsUpdate, "unknown installed version outside winget's own upgrade-available list must not be flagged as needing an update")
	assert.Equal(t, "17.0.1000.7", avail, "AvailableVersion stays informational")
}
