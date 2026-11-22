// Package db opens the SQLite database and applies embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite" // pure-Go driver, no CGO
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1) // ponytail: single connection, no lock contention; raise if reads ever bottleneck
	if err := migrate(d); err != nil {
		d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// migrate applies migrations/NNN_name.sql files in filename order, once each.
func migrate(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, f := range files {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, f.Name()).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + f.Name())
		if err != nil {
			return err
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", f.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, f.Name()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SetConfig stores a per-guild key/value (channel IDs, role IDs, settings).
func SetConfig(ctx context.Context, d *sql.DB, guildID, key, value string) error {
	_, err := d.ExecContext(ctx, `INSERT INTO guild_config (guild_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT(guild_id, key) DO UPDATE SET value = excluded.value`, guildID, key, value)
	return err
}

// GetConfig returns "" when the key is unset.
func GetConfig(ctx context.Context, d *sql.DB, guildID, key string) (string, error) {
	var v string
	err := d.QueryRowContext(ctx, `SELECT value FROM guild_config WHERE guild_id = ? AND key = ?`, guildID, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}
