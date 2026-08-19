package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	var (
		command       string
		steps         int
		migrationsDir string
		databaseURL   string
	)

	flag.StringVar(&command, "command", "", "Migration command: up, down, force, version, drop")
	flag.IntVar(&steps, "steps", 0, "Number of steps for up/down (0 = all)")
	flag.StringVar(&migrationsDir, "path", "", "Path to migrations directory (default: auto-detected)")
	flag.StringVar(&databaseURL, "database", "", "Database connection URL (default: from DATABASE_URL env)")
	flag.Parse()

	// Resolve database URL
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		log.Fatal("Database URL is required. Set DATABASE_URL env or use -database flag.")
	}

	// Resolve migrations directory
	if migrationsDir == "" {
		migrationsDir = os.Getenv("MIGRATIONS_PATH")
	}
	if migrationsDir == "" {
		// Auto-detect: look relative to the binary or working directory
		candidates := []string{
			"migrations",
			"backend/migrations",
			filepath.Join("..", "..", "migrations"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				migrationsDir = candidate
				break
			}
		}
		if migrationsDir == "" {
			migrationsDir = "migrations"
		}
	}

	// Convert to absolute path for file source
	absPath, err := filepath.Abs(migrationsDir)
	if err != nil {
		log.Fatalf("Failed to resolve migrations path: %v", err)
	}

	sourceURL := fmt.Sprintf("file://%s", absPath)

	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		log.Fatalf("Failed to create migrate instance: %v", err)
	}
	defer m.Close()

	switch command {
	case "up":
		if steps > 0 {
			err = m.Steps(steps)
		} else {
			err = m.Up()
		}
	case "down":
		if steps > 0 {
			err = m.Steps(-steps)
		} else {
			err = m.Down()
		}
	case "force":
		if steps == 0 {
			log.Fatal("Force requires -steps to specify the version to force")
		}
		err = m.Force(steps)
	case "version":
		version, dirty, verErr := m.Version()
		if verErr != nil {
			log.Fatalf("Failed to get version: %v", verErr)
		}
		fmt.Printf("Version: %d, Dirty: %v\n", version, dirty)
		return
	case "drop":
		err = m.Drop()
	default:
		fmt.Println("DomainRadar Migration Tool")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  migrate -command <command> [options]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  up       Apply all (or N) pending migrations")
		fmt.Println("  down     Roll back all (or N) applied migrations")
		fmt.Println("  force    Force set version (use with -steps for version number)")
		fmt.Println("  version  Print current migration version")
		fmt.Println("  drop     Drop all tables")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  -steps N      Number of migrations to apply/rollback (0 = all)")
		fmt.Println("  -path DIR     Path to migrations directory")
		fmt.Println("  -database URL PostgreSQL connection URL")
		fmt.Println()
		fmt.Println("Environment Variables:")
		fmt.Println("  DATABASE_URL    PostgreSQL connection URL")
		fmt.Println("  MIGRATIONS_PATH Path to migrations directory")
		os.Exit(1)
	}

	if err != nil {
		if err == migrate.ErrNoChange {
			fmt.Println("No migrations to apply.")
		} else {
			log.Fatalf("Migration failed: %v", err)
		}
	} else {
		fmt.Printf("Migration '%s' completed successfully.\n", command)
	}
}
