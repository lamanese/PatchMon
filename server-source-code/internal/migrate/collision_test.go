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
	"fork_pkg_broken",
	"fork_boot_time",
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
// TABLE, ADD COLUMN and CREATE [UNIQUE] INDEX [CONCURRENTLY], each with an
// optional IF NOT EXISTS. The captured name strips an optional schema
// qualifier (public.foo -> foo), since forkOwnedIdentifiers lists bare
// names. A multi-column "ALTER TABLE t ADD COLUMN a ..., ADD COLUMN b ..."
// matches once per ADD COLUMN occurrence: FindAllStringSubmatch scans the
// whole body for every match of the alternation, not just the first one
// found.
//
// Go's regexp (RE2) has no lookahead, so an unnamed "CREATE INDEX ON table"
// captures the following "ON" as if it were the index name; matchedObjectNames
// below discards that one specific false positive rather than complicating
// the pattern.
var createdObject = regexp.MustCompile(`(?i)(?:` +
	`create\s+table\s+(?:if\s+not\s+exists\s+)?` +
	`|add\s+column\s+(?:if\s+not\s+exists\s+)?` +
	`|create\s+(?:unique\s+)?index\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?` +
	`)(?:[a-z_][a-z0-9_]*\.)?([a-z_][a-z0-9_]*)`)

// matchedObjectNames runs createdObject over body and returns the lower-cased
// object names, dropping "on": an unnamed "CREATE INDEX ON table(...)" has no
// index name at all, and createdObject (which cannot look ahead) captures the
// following ON keyword as if it were one.
func matchedObjectNames(body string) []string {
	var names []string
	for _, m := range createdObject.FindAllStringSubmatch(body, -1) {
		if name := strings.ToLower(m[1]); name != "on" {
			names = append(names, name)
		}
	}
	return names
}

// coveredByGuardList reports whether name is guarded by forkOwnedIdentifiers.
// Any created name — table or column, not just an index — is accepted when
// it merely CONTAINS a listed identifier, which is sound because
// TestUpstreamMigrationsDoNotTouchForkObjects above does its own
// upstream-mention scan the same way, as a substring match. This mainly
// matters for a derived name like idx_reboot_schedules_enabled, which
// contains reboot_schedules and so is covered without listing every
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
// list (directly, or as a substring match), otherwise the guard above
// quietly stops covering it.
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
		for _, name := range matchedObjectNames(string(body)) {
			if !coveredByGuardList(name, listed) {
				t.Errorf("%s creates %q, which is missing from forkOwnedIdentifiers", e.Name(), name)
			}
		}
	}
}

// Pins createdObject's matching rules against a literal SQL string, so a
// future edit to the regex cannot quietly stop catching a form the real
// migrations happen not to use yet: two ADD COLUMN clauses in one ALTER
// TABLE statement, a schema-qualified CREATE TABLE, CREATE UNIQUE INDEX IF
// NOT EXISTS, an unnamed CREATE INDEX (must contribute nothing, not "on"),
// and CREATE INDEX CONCURRENTLY.
func TestCreatedObjectRegexMatchesEachForm(t *testing.T) {
	const sql = `
CREATE TABLE IF NOT EXISTS public.reboot_schedules (id serial);
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS allow_reboot boolean, ADD COLUMN IF NOT EXISTS another_col text;
CREATE UNIQUE INDEX IF NOT EXISTS idx_hosts_allow_reboot ON hosts(allow_reboot);
CREATE INDEX ON hosts(allow_reboot);
CREATE INDEX CONCURRENTLY idx_concurrent_test ON hosts(allow_reboot);
`
	got := matchedObjectNames(sql)
	want := []string{"reboot_schedules", "allow_reboot", "another_col", "idx_hosts_allow_reboot", "idx_concurrent_test"}
	if !slices.Equal(got, want) {
		t.Errorf("matchedObjectNames = %v, want %v", got, want)
	}
}
