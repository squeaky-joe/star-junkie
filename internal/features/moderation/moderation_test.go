package moderation

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	ok := map[string]time.Duration{
		"10m": 10 * time.Minute, "2h": 2 * time.Hour, "1d": 24 * time.Hour,
		"1w": 7 * 24 * time.Hour, " 30 M ": 30 * time.Minute, "28d": 28 * 24 * time.Hour,
	}
	for in, want := range ok {
		if got, err := parseDuration(in); err != nil || got != want {
			t.Errorf("%q: got %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "10", "m", "0m", "29d", "5w", "1.5h", "-5m", "10 minutes"} {
		if _, err := parseDuration(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestFormatModlog(t *testing.T) {
	got := formatModlog([]modEntry{
		{Action: "warn", Reason: "spamming", ModID: "9", At: 100},
		{Action: "promote", ModID: "9", At: 50},
		{Action: "ban", Reason: strings.Repeat("x", 300), ModID: "8", At: 10},
	})
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines: %q", got)
	}
	if lines[0] != "<t:100:d> **warn** by <@9>: spamming" {
		t.Errorf("line 0: %q", lines[0])
	}
	if lines[1] != "<t:50:d> **promote** by <@9>" {
		t.Errorf("line 1: %q", lines[1])
	}
	if !strings.HasSuffix(lines[2], "…") || len([]rune(lines[2])) > 200 {
		t.Errorf("long reason not shortened: %q", lines[2])
	}
}

func TestRoleDiff(t *testing.T) {
	a, r := roleDiff([]string{"1", "2"}, []string{"2", "3"})
	if !reflect.DeepEqual(a, []string{"3"}) || !reflect.DeepEqual(r, []string{"1"}) {
		t.Errorf("added %v removed %v", a, r)
	}
	if a, r := roleDiff([]string{"1"}, []string{"1"}); a != nil || r != nil {
		t.Errorf("no change: %v %v", a, r)
	}
}

func TestMarks(t *testing.T) {
	m := &marks{m: map[string]time.Time{}}
	if m.has("ban", "g", "u", time.Minute) {
		t.Error("nothing marked yet")
	}
	m.set("ban", "g", "u")
	if !m.has("ban", "g", "u", time.Minute) || m.has("kick", "g", "u", time.Minute) || m.has("ban", "g", "other", time.Minute) {
		t.Error("marks should match kind+guild+id exactly")
	}
	m.m["ban|g|old"] = time.Now().Add(-time.Hour)
	if m.has("ban", "g", "old", time.Minute) {
		t.Error("stale mark should not count")
	}
}
