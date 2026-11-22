package db

import (
	"context"
	"path/filepath"
	"testing"
)

// Opens a fresh DB (running every migration), checks the tables exist, and that re-opening is a no-op.
func TestOpenMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "bot.db")
	for range 2 {
		d, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, tbl := range []string{"guild_config", "mod_actions", "applications", "events", "event_rsvps"} {
			var n int
			if err := d.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
				t.Errorf("table %s: %v", tbl, err)
			}
		}
		ctx := context.Background()
		if err := SetConfig(ctx, d, "g", "k", "v1"); err != nil {
			t.Fatal(err)
		}
		if err := SetConfig(ctx, d, "g", "k", "v2"); err != nil {
			t.Fatal(err)
		}
		if v, _ := GetConfig(ctx, d, "g", "k"); v != "v2" {
			t.Errorf("GetConfig = %q", v)
		}
		if v, _ := GetConfig(ctx, d, "g", "missing"); v != "" {
			t.Errorf("missing key = %q", v)
		}
		d.Close()
	}
}
