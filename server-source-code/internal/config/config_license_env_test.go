package config

import "testing"

// A mistyped PM_LICENSE_MAX_HOSTS used to parse as 0, which means "not
// env-managed": the lock vanished silently and the licence became editable in
// the UI again. Licence env vars must fail startup instead of being ignored.
func TestLoad_LicenseEnvValidation(t *testing.T) {
	cases := []struct {
		name     string
		maxHosts string
		enforce  string
		wantErr  bool
	}{
		{"all unset", "", "", false},
		{"valid max only", "50", "", false},
		{"valid max with enforce", "50", "true", false},
		{"valid max, enforce false", "50", "false", false},
		{"surrounding whitespace is tolerated", " 50 ", "", false},
		{"typo in number", "5O", "", true},
		{"non-numeric", "abc", "", true},
		{"zero", "0", "", true},
		{"negative", "-5", "", true},
		{"enforce without max", "", "true", true},
		{"enforce is not a boolean", "50", "yes", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENV_FILE", "/nonexistent")
			t.Setenv("DATABASE_URL", "postgresql://localhost/test")
			t.Setenv("JWT_SECRET", "test-secret")
			t.Setenv("PM_LICENSE_MAX_HOSTS", tc.maxHosts)
			t.Setenv("PM_LICENSE_ENFORCE", tc.enforce)

			cfg, err := Load()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Load() accepted PM_LICENSE_MAX_HOSTS=%q PM_LICENSE_ENFORCE=%q, want error", tc.maxHosts, tc.enforce)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if tc.maxHosts != "" && cfg.LicenseMaxHosts != 50 {
				t.Errorf("LicenseMaxHosts = %d, want 50", cfg.LicenseMaxHosts)
			}
		})
	}
}
