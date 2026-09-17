package util

import "testing"

// Regression: the Windows agent binary contains "VersionEnumPageFilesWv0.0.0"
// long before the real version, and ARM/386 builds contain "1.0.0". Taking the
// first X.Y.Z match reported those as the bundled version, so every host whose
// binary the server cannot execute never received a self-update.
func TestExtractEmbeddedAgentVersion(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"windows binary with misleading earlier match", "VersionEnumPageFilesWv0.0.0\x00go1.26.8\x001.40.0 fp:2.0.9serve\x00PatchMon Agent v2.0.9\nA system patch monitoring agent", "2.0.9"},
		{"arm binary with 1.0.0 first", "1.0.0\x00PatchMon Agent v2.0.8\nfoo", "2.0.8"},
		{"multi digit", "PatchMon Agent v12.34.567\n", "12.34.567"},
		{"no marker must not guess", "VersionEnumPageFilesWv0.0.0 1.40.0 2.0.9", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractEmbeddedAgentVersion([]byte(tt.data)); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
