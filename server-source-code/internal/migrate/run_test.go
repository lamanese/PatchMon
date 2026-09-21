package migrate

import "testing"

func TestRun_FreshDatabaseAppliesBothSets(t *testing.T) {
	dbURL := newTestDB(t)

	if err := Run(dbURL, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	up, fork, forkTable := dbVersions(t, dbURL)
	if want := highestVersion(t, migrationsFS, "migrations"); up != want {
		t.Errorf("upstream version = %d, want %d", up, want)
	}
	if !forkTable {
		t.Fatal("schema_migrations_fork was not created")
	}
	if want := highestVersion(t, forkMigrationsFS, "migrations_fork"); fork != want {
		t.Errorf("fork version = %d, want %d", fork, want)
	}
}

func TestRun_SecondRunChangesNothing(t *testing.T) {
	dbURL := newTestDB(t)
	if err := Run(dbURL, discardLogger()); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	up1, fork1, _ := dbVersions(t, dbURL)

	if err := Run(dbURL, discardLogger()); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	up2, fork2, _ := dbVersions(t, dbURL)
	if up1 != up2 || fork1 != fork2 {
		t.Errorf("versions changed on second run: %d/%d -> %d/%d", up1, fork1, up2, fork2)
	}
}

// A database that only ever saw upstream PatchMon has no fork objects. It must
// not be bridged; the fork set simply runs from 1.
func TestRun_PureUpstreamDatabaseGetsForkSet(t *testing.T) {
	dbURL := newTestDB(t)
	m, err := OpenSet(dbURL, SetUpstream)
	if err != nil {
		t.Fatalf("open upstream set: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("upstream up: %v", err)
	}
	_, _ = m.Close()
	upBefore, _, forkTable := dbVersions(t, dbURL)
	if forkTable {
		t.Fatal("precondition: fork table must not exist yet")
	}

	if err := Run(dbURL, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	up, fork, _ := dbVersions(t, dbURL)
	if up != upBefore {
		t.Errorf("upstream version changed: %d -> %d", upBefore, up)
	}
	if want := highestVersion(t, forkMigrationsFS, "migrations_fork"); fork != want {
		t.Errorf("fork version = %d, want %d", fork, want)
	}
}
