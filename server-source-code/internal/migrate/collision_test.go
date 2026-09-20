package migrate

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// forkOwnedIdentifiers are schema names the fork created. `IF NOT EXISTS` would
// silently keep OUR table if upstream later ships one with the same name and a
// different shape, so an upstream migration mentioning one of these must stop
// the build at sync time. Add every new fork object here (prefix new ones fork_).
var forkOwnedIdentifiers = []string{
	"can_reboot_hosts",
	"allow_reboot",
	"reboot_schedules",
	"patch_schedules",
	"license_max_hosts",
	"license_enforce",
	"license_package",
}

func TestUpstreamMigrationsDoNotTouchForkObjects(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	for _, e := range entries {
		body, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		lower := strings.ToLower(string(body))
		for _, id := range forkOwnedIdentifiers {
			if strings.Contains(lower, id) {
				t.Errorf("upstream migration %s mentions fork-owned identifier %q", e.Name(), id)
			}
		}
	}
}

var createdObject = regexp.MustCompile(`(?i)(?:create\s+table\s+(?:if\s+not\s+exists\s+)?|add\s+column\s+(?:if\s+not\s+exists\s+)?)([a-z_][a-z0-9_]*)`)

// Every table or column a fork migration creates must be on the guard list,
// otherwise the guard above quietly stops covering it.
func TestForkMigrationsAreOnTheGuardList(t *testing.T) {
	listed := map[string]bool{}
	for _, id := range forkOwnedIdentifiers {
		listed[id] = true
	}
	entries, err := fs.ReadDir(forkMigrationsFS, "migrations_fork")
	if err != nil {
		t.Fatalf("read fork migrations: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		body, err := fs.ReadFile(forkMigrationsFS, "migrations_fork/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, m := range createdObject.FindAllStringSubmatch(string(body), -1) {
			if name := strings.ToLower(m[1]); !listed[name] {
				t.Errorf("%s creates %q, which is missing from forkOwnedIdentifiers", e.Name(), name)
			}
		}
	}
}
