package migrate

import (
	"io/fs"
	"regexp"
	"slices"
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
	"fork_is_definition_update",
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

// createdObject finds every object name a fork migration creates: CREATE
// TABLE, ADD COLUMN and CREATE [UNIQUE] INDEX, each with an optional
// IF NOT EXISTS. The captured name strips an optional schema qualifier
// (public.foo -> foo), since forkOwnedIdentifiers lists bare names. A
// multi-column "ALTER TABLE t ADD COLUMN a ..., ADD COLUMN b ..." matches
// once per ADD COLUMN occurrence: FindAllStringSubmatch scans the whole body
// for every match of the alternation, not just the first one found.
var createdObject = regexp.MustCompile(`(?i)(?:` +
	`create\s+table\s+(?:if\s+not\s+exists\s+)?` +
	`|add\s+column\s+(?:if\s+not\s+exists\s+)?` +
	`|create\s+(?:unique\s+)?index\s+(?:if\s+not\s+exists\s+)?` +
	`)(?:[a-z_][a-z0-9_]*\.)?([a-z_][a-z0-9_]*)`)

// coveredByGuardList reports whether name is guarded by forkOwnedIdentifiers.
// An index name is accepted when it merely CONTAINS a listed identifier
// (e.g. idx_reboot_schedules_enabled contains reboot_schedules): index names
// are derived from the table/column they index, not independently
// fork-owned, so the substring rule covers them without listing every
// generated index name individually.
func coveredByGuardList(name string, listed map[string]bool) bool {
	if listed[name] {
		return true
	}
	for id := range listed {
		if strings.Contains(name, id) {
			return true
		}
	}
	return false
}

// Every table, column or index a fork migration creates must be on the guard
// list (directly, or as a substring for index names), otherwise the guard
// above quietly stops covering it.
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
			if name := strings.ToLower(m[1]); !coveredByGuardList(name, listed) {
				t.Errorf("%s creates %q, which is missing from forkOwnedIdentifiers", e.Name(), name)
			}
		}
	}
}

// Pins createdObject's matching rules against a literal SQL string, so a
// future edit to the regex cannot quietly stop catching a form the real
// migrations happen not to use yet: two ADD COLUMN clauses in one ALTER
// TABLE statement, a schema-qualified CREATE TABLE, and CREATE UNIQUE INDEX
// IF NOT EXISTS.
func TestCreatedObjectRegexMatchesEachForm(t *testing.T) {
	const sql = `
CREATE TABLE IF NOT EXISTS public.reboot_schedules (id serial);
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS allow_reboot boolean, ADD COLUMN IF NOT EXISTS another_col text;
CREATE UNIQUE INDEX IF NOT EXISTS idx_hosts_allow_reboot ON hosts(allow_reboot);
`
	var got []string
	for _, m := range createdObject.FindAllStringSubmatch(sql, -1) {
		got = append(got, strings.ToLower(m[1]))
	}
	want := []string{"reboot_schedules", "allow_reboot", "another_col", "idx_hosts_allow_reboot"}
	if !slices.Equal(got, want) {
		t.Errorf("createdObject matches = %v, want %v", got, want)
	}
}
