package migrate

import (
	"net/url"
	"strings"
	"testing"
)

// forkURL must always point golang-migrate at ForkMigrationsTable, never at
// whatever x-migrations-table (if any) the caller's URL already carries: two
// migrate.Migrate instances sharing one version table would silently skip
// each other's pending migrations. These are pure string/URL tests, no DB.
func TestForkURL(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"plain url", "postgres://user:pass@host:5432/db"},
		{"existing query", "postgres://user:pass@host:5432/db?connect_timeout=5"},
		{"already has sslmode", "postgres://user:pass@host:5432/db?sslmode=require"},
		{"already has x-migrations-table", "postgres://user:pass@host:5432/db?x-migrations-table=foo"},
		{"password with special characters", "postgres://user:p%40ss%2Fw0rd@host:5432/db"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := forkURL(tc.input)

			if !strings.Contains(got, "x-migrations-table="+ForkMigrationsTable) {
				t.Errorf("forkURL(%q) = %q, want it to set x-migrations-table=%s",
					tc.input, got, ForkMigrationsTable)
			}
			if strings.Count(got, "x-migrations-table=") != 1 {
				t.Errorf("forkURL(%q) = %q, want exactly one x-migrations-table param", tc.input, got)
			}

			// Every case in this table is parsable by net/url (verified by
			// the precondition below), so forkURL takes the net/url path and
			// must round-trip credentials exactly. The unparsable-DSN
			// fallback is covered separately, below.
			u, err := url.Parse(tc.input)
			if err != nil {
				t.Fatalf("precondition failed: url.Parse(%q) = %v, want this case to be parsable", tc.input, err)
			}
			got2, err2 := url.Parse(got)
			if err2 != nil {
				t.Fatalf("forkURL(%q) produced an unparsable URL %q: %v", tc.input, got, err2)
			}
			if u.User != nil {
				if got2.User == nil {
					t.Fatalf("forkURL(%q) = %q, lost userinfo", tc.input, got)
				}
				wantPass, _ := u.User.Password()
				gotPass, _ := got2.User.Password()
				if got2.User.Username() != u.User.Username() || gotPass != wantPass {
					t.Errorf("forkURL(%q) = %q, userinfo changed (want user=%q pass=%q, got user=%q pass=%q)",
						tc.input, got, u.User.Username(), wantPass, got2.User.Username(), gotPass)
				}
			}
		})
	}

	t.Run("replaces an existing x-migrations-table instead of leaving it", func(t *testing.T) {
		got := forkURL("postgres://user:pass@host:5432/db?x-migrations-table=foo")
		q, err := url.ParseQuery(strings.SplitN(got, "?", 2)[1])
		if err != nil {
			t.Fatalf("parse query of %q: %v", got, err)
		}
		if v := q.Get("x-migrations-table"); v != ForkMigrationsTable {
			t.Errorf("x-migrations-table = %q, want %q (must replace, not keep, an existing value)", v, ForkMigrationsTable)
		}
	})

	t.Run("adds sslmode=disable only when absent", func(t *testing.T) {
		got := forkURL("postgres://user:pass@host:5432/db")
		if !strings.Contains(got, "sslmode=disable") {
			t.Errorf("forkURL(%q) = %q, want sslmode=disable added", "postgres://user:pass@host:5432/db", got)
		}

		got2 := forkURL("postgres://user:pass@host:5432/db?sslmode=require")
		if strings.Contains(got2, "sslmode=disable") || !strings.Contains(got2, "sslmode=require") {
			t.Errorf("forkURL with existing sslmode=require = %q, want sslmode=require kept, not overridden", got2)
		}
	})

	// The fallback below only ever runs for a DSN net/url.Parse rejects.
	// %zz is NOT such a DSN: url.Parse does not validate RawQuery eagerly,
	// so "...?%zz" parses successfully and never touches the fallback at
	// all. These three are verified (by the precondition loop) to fail
	// url.Parse for a real structural reason: a non-numeric port, an
	// unterminated IPv6 host literal, and a raw control character.
	unparsable := []string{
		"postgres://u:p@db:notaport/db",
		"postgres://u:p@[::1/db",
		"postgres://u:p@host/db\n",
	}
	for _, in := range unparsable {
		if _, err := url.Parse(in); err == nil {
			t.Fatalf("precondition failed: url.Parse(%q) succeeded, want an error so this case exercises forkURL's fallback", in)
		}
	}

	// golang-migrate's own postgres driver parses its DSN with the same
	// net/url.Parse (Open in database/postgres/postgres.go), so any of these
	// would fail there too and Run never reaches a working migrate.Migrate.
	// The fallback therefore only needs to be harmless: still exactly one
	// x-migrations-table param, and still pointed at ForkMigrationsTable.
	t.Run("fallback appends x-migrations-table when absent", func(t *testing.T) {
		for _, in := range unparsable {
			got := forkURL(in)
			if strings.Count(got, "x-migrations-table=") != 1 {
				t.Errorf("forkURL(%q) = %q, want exactly one x-migrations-table param", in, got)
			}
			if !strings.Contains(got, "x-migrations-table="+ForkMigrationsTable) {
				t.Errorf("forkURL(%q) = %q, want x-migrations-table=%s", in, got, ForkMigrationsTable)
			}
		}
	})

	t.Run("fallback replaces an existing x-migrations-table instead of leaving it", func(t *testing.T) {
		for _, in := range unparsable {
			withParam := in + "?x-migrations-table=foo"
			if _, err := url.Parse(withParam); err == nil {
				t.Fatalf("precondition failed: url.Parse(%q) succeeded, want an error", withParam)
			}

			got := forkURL(withParam)
			if strings.Count(got, "x-migrations-table=") != 1 {
				t.Errorf("forkURL(%q) = %q, want exactly one x-migrations-table param", withParam, got)
			}
			if !strings.Contains(got, "x-migrations-table="+ForkMigrationsTable) {
				t.Errorf("forkURL(%q) = %q, want x-migrations-table=%s", withParam, got, ForkMigrationsTable)
			}
			if strings.Contains(got, "x-migrations-table=foo") {
				t.Errorf("forkURL(%q) = %q, kept the caller's x-migrations-table=foo instead of replacing it", withParam, got)
			}
		}
	})
}
