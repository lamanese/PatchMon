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
		{"unparsable input", "postgres://user:pass@host:5432/db?%zz"},
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

			// A URL forkURL can parse must round-trip its credentials: the
			// fallback path for unparsable input is exempt, since it never
			// touches the userinfo at all.
			if u, err := url.Parse(tc.input); err == nil {
				if got2, err2 := url.Parse(got); err2 != nil {
					t.Errorf("forkURL(%q) produced an unparsable URL %q: %v", tc.input, got, err2)
				} else if u.User != nil {
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
}
