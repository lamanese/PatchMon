package config

import (
	"os"
	"testing"
)

func TestLoad_ValidEnv(t *testing.T) {
	t.Setenv("ENV_FILE", "/nonexistent")
	t.Setenv("DATABASE_URL", "postgresql://localhost/test")
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("PORT", "3001")
	t.Setenv("ENABLE_LOGGING", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DatabaseURL != "postgresql://localhost/test" {
		t.Errorf("DatabaseURL = %q, want postgresql://localhost/test", cfg.DatabaseURL)
	}
	if cfg.Port != 3001 {
		t.Errorf("Port = %d, want 3001", cfg.Port)
	}
	if cfg.Version != DefaultVersion {
		t.Errorf("Version = %q, want %s", cfg.Version, DefaultVersion)
	}
	if !cfg.EnableLogging {
		t.Error("EnableLogging = false, want true")
	}
}

func TestLoad_EnableLoggingDefault(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "unset defaults to on", env: "", want: true},
		{name: "explicit false is honoured", env: "false", want: false},
		{name: "explicit true", env: "true", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENV_FILE", "/nonexistent")
			t.Setenv("DATABASE_URL", "postgresql://localhost/test")
			t.Setenv("JWT_SECRET", "test-secret")
			t.Setenv("ENABLE_LOGGING", tt.env)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.EnableLogging != tt.want {
				t.Errorf("EnableLogging = %v, want %v", cfg.EnableLogging, tt.want)
			}
		})
	}
}

// TestLoad_IgnoreDefinitionUpdatesDefault mirrors TestLoad_EnableLoggingDefault:
// PM_IGNORE_DEFINITION_UPDATES follows the same "empty env" == "" comparison
// style as PM_HIDE_COMMUNITY_LINKS / PM_DISABLE_SIGNUP, so it must default to
// false and only turn on for the literal string "true".
func TestLoad_IgnoreDefinitionUpdatesDefault(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		unset bool // when true, the variable is genuinely absent (os.Unsetenv), not just set to ""
		want  bool
	}{
		{name: "genuinely unset defaults to off", unset: true, want: false},
		{name: "explicit empty string defaults to off", env: "", want: false},
		{name: "explicit true", env: "true", want: true},
		{name: "explicit false stays off", env: "false", want: false},
		{name: "garbage value stays off (fail closed, not fail open)", env: "TRUE", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENV_FILE", "/nonexistent")
			t.Setenv("DATABASE_URL", "postgresql://localhost/test")
			t.Setenv("JWT_SECRET", "test-secret")
			if tt.unset {
				// t.Setenv can only set a value, never remove the variable, so a
				// genuinely-absent PM_IGNORE_DEFINITION_UPDATES needs a manual
				// unset with restore, distinct from the "set to empty string" case.
				original, wasSet := os.LookupEnv("PM_IGNORE_DEFINITION_UPDATES")
				if err := os.Unsetenv("PM_IGNORE_DEFINITION_UPDATES"); err != nil {
					t.Fatalf("Unsetenv: %v", err)
				}
				t.Cleanup(func() {
					if wasSet {
						_ = os.Setenv("PM_IGNORE_DEFINITION_UPDATES", original)
					}
				})
			} else {
				t.Setenv("PM_IGNORE_DEFINITION_UPDATES", tt.env)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.IgnoreDefinitionUpdates != tt.want {
				t.Errorf("IgnoreDefinitionUpdates = %v, want %v", cfg.IgnoreDefinitionUpdates, tt.want)
			}
		})
	}
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Setenv("ENV_FILE", "/nonexistent")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "test-secret")

	_, err := Load()
	if err == nil {
		t.Error("Load() expected error, got nil")
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	cfg := &Config{DatabaseURL: "postgres://x", Port: 0, LogLevel: "info"}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() expected error for port 0")
	}
}
