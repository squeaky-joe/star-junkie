package events

import (
	"strings"
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		date, clock string
		want        time.Time // UTC
	}{
		{"2026-12-05", "20:00", time.Date(2026, 12, 6, 1, 0, 0, 0, time.UTC)}, // EST = UTC-5
		{"2026-07-05", "8pm", time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)},    // EDT = UTC-4
		{"2026-12-05", "8:30 PM", time.Date(2026, 12, 6, 1, 30, 0, 0, time.UTC)},
		{"2026-12-05", "9:05", time.Date(2026, 12, 5, 14, 5, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		got, err := parseWhen(c.date, c.clock, ny, time.Time{})
		if err != nil || !got.UTC().Equal(c.want) {
			t.Errorf("%s %s: got %v err %v, want %v", c.date, c.clock, got.UTC(), err, c.want)
		}
	}
	for _, bad := range [][2]string{{"12/05/2026", "20:00"}, {"2026-12-05", "late"}, {"2026-13-40", "20:00"}, {"2026-12-05", "25:00"}} {
		if _, err := parseWhen(bad[0], bad[1], ny, time.Time{}); err == nil {
			t.Errorf("%v should fail", bad)
		}
	}

	// Edit: blank date keeps the base date, blank clock keeps the base clock.
	base := time.Date(2026, 12, 5, 20, 0, 0, 0, ny)
	got, _ := parseWhen("", "9pm", ny, base)
	if !got.Equal(time.Date(2026, 12, 5, 21, 0, 0, 0, ny)) {
		t.Errorf("clock-only edit: %v", got)
	}
	got, _ = parseWhen("2026-12-12", "", ny, base)
	if !got.Equal(time.Date(2026, 12, 12, 20, 0, 0, 0, ny)) {
		t.Errorf("date-only edit: %v", got)
	}
}

func TestTimeTags(t *testing.T) {
	got := timeTags(time.Unix(1764972000, 0))
	if got != "<t:1764972000:F> (<t:1764972000:R>)" {
		t.Errorf("got %q", got)
	}
}

func TestReminderDue(t *testing.T) {
	start := time.Date(2026, 12, 5, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		now    time.Time
		s1, sS bool
		want   string
	}{
		{"far away", start.Add(-3 * time.Hour), false, false, ""},
		{"inside the hour", start.Add(-59 * time.Minute), false, false, "1h"},
		{"hour already sent", start.Add(-30 * time.Minute), true, false, ""},
		{"at start", start, true, false, "start"},
		{"just after start", start.Add(5 * time.Minute), false, false, "start"},
		{"missed by a lot", start.Add(2 * time.Hour), false, false, "skip"},
		{"all sent", start.Add(time.Minute), true, true, ""},
	}
	for _, c := range cases {
		if got := reminderDue(start, c.now, c.s1, c.sS); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestChunkAndMentions(t *testing.T) {
	ids := []string{"1", "2", "3", "4", "5"}
	g := chunk(ids, 2)
	if len(g) != 3 || len(g[2]) != 1 {
		t.Errorf("chunk: %v", g)
	}
	if len(chunk(nil, 2)) != 0 {
		t.Error("empty chunk")
	}
	if got := mentionList(ids, 2); got != "<@1> <@2> +3 more" {
		t.Errorf("mentionList = %q", got)
	}
	if mentionList(nil, 5) != "—" {
		t.Error("empty list")
	}
}

func TestRenderEmbed(t *testing.T) {
	ev := event{ID: 3, Title: "Game Night", Desc: "Bring snacks", Start: time.Unix(1764972000, 0), Status: "active"}
	e := renderEmbed(ev, map[string][]string{"going": {"7"}})
	if !strings.Contains(e.Description, "<t:1764972000:F>") || !strings.Contains(e.Description, "Bring snacks") {
		t.Errorf("description: %q", e.Description)
	}
	if len(e.Fields) != 3 || !strings.Contains(e.Fields[0].Name, "(1)") || e.Fields[0].Value != "<@7>" {
		t.Errorf("fields: %+v", e.Fields)
	}
	ev.Status = "cancelled"
	if c := renderEmbed(ev, nil); !strings.HasPrefix(c.Title, "❌ Cancelled") || len(c.Fields) != 0 {
		t.Errorf("cancelled: %+v", c)
	}
}
