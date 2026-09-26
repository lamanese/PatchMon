// Package main runs database migrations using golang-migrate with embedded SQL files.
// Usage:
//
//	migrate up                       - bridge a legacy fork DB, then run upstream and fork migrations (same as server start)
//	migrate [-set upstream|fork] down            - roll back the last migration of one set
//	migrate [-set upstream|fork] force VERSION   - set the version of one set
//	migrate version                  - show both versions
//
// Requires DATABASE_URL environment variable.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	ourmigrate "github.com/PatchMon/PatchMon/server-source-code/internal/migrate"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
)

func main() {
	setName := flag.String("set", string(ourmigrate.SetUpstream), "migration set for down/force: upstream or fork")
	flag.Parse()
	args := flag.Args()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL environment variable is required")
		os.Exit(1)
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: migrate [-set upstream|fork] [up|down|force VERSION|version]")
		os.Exit(1)
	}

	switch args[0] {
	case "up":
		if err := ourmigrate.Run(dbURL, slog.New(slog.NewTextHandler(os.Stderr, nil))); err != nil {
			fmt.Fprintf(os.Stderr, "Migration up failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Migrations completed successfully")
		return
	case "version":
		printVersion(dbURL, ourmigrate.SetUpstream)
		needsBridge, err := ourmigrate.NeedsBridge(context.Background(), dbURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fork: failed to check bridge state: %v\n", err)
			os.Exit(1)
		}
		if needsBridge {
			fmt.Println(`fork: legacy database, not bridged yet (run "migrate up")`)
		} else {
			printVersion(dbURL, ourmigrate.SetFork)
		}
		return
	}

	if ourmigrate.Set(*setName) == ourmigrate.SetFork {
		needsBridge, err := ourmigrate.NeedsBridge(context.Background(), dbURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to check bridge state: %v\n", err)
			os.Exit(1)
		}
		if needsBridge {
			fmt.Fprintln(os.Stderr, `legacy database, run "migrate up" first`)
			os.Exit(1)
		}
	}

	m, err := ourmigrate.OpenSet(dbURL, ourmigrate.Set(*setName))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create migrate instance: %v\n", err)
		os.Exit(1)
	}
	defer func() { _, _ = m.Close() }()

	switch args[0] {
	case "down":
		downErr := m.Steps(-1)
		if downErr != nil && downErr != migrate.ErrNoChange {
			fmt.Fprintf(os.Stderr, "Migration down failed: %v\n", downErr)
			os.Exit(1)
		}
		if downErr == migrate.ErrNoChange {
			fmt.Println("No migrations to roll back")
		} else {
			fmt.Println("Rollback completed successfully")
		}
	case "force":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: migrate force VERSION")
			os.Exit(1)
		}
		var version int
		if _, err := fmt.Sscanf(args[1], "%d", &version); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid version: %s\n", args[1])
			os.Exit(1)
		}
		if err := m.Force(version); err != nil {
			fmt.Fprintf(os.Stderr, "Force failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Forced version to %d\n", version)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Usage: migrate [-set upstream|fork] [up|down|force VERSION|version]")
		os.Exit(1)
	}
}

// printVersion opens one migration set to report its version. Opening the
// fork set on a database that was never bridged creates an empty
// schema_migrations_fork table (golang-migrate does that on open), which
// would make the bridge think the database is already converted. Callers
// must check NeedsBridge before calling this for the fork set.
func printVersion(dbURL string, set ourmigrate.Set) {
	m, err := ourmigrate.OpenSet(dbURL, set)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: failed to open: %v\n", set, err)
		os.Exit(1)
	}
	defer func() { _, _ = m.Close() }()
	version, dirty, err := m.Version()
	switch {
	case err == migrate.ErrNilVersion:
		fmt.Printf("%s: no migrations applied yet\n", set)
	case err != nil:
		fmt.Fprintf(os.Stderr, "%s: version check failed: %v\n", set, err)
		os.Exit(1)
	default:
		fmt.Printf("%s: version %d (dirty: %v)\n", set, version, dirty)
	}
}
