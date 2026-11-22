package moderation

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestNormalize(t *testing.T) {
	if normalize("  Hello   WORLD \n") != "hello world" || normalize("   ") != "" {
		t.Error("normalize")
	}
}

func TestTrackerRepeats(t *testing.T) {
	tr := &tracker{m: map[string][]spamEntry{}}
	base := time.Date(2026, 12, 5, 20, 0, 0, 0, time.UTC)
	msg := func(id, text string, sec int) spamEntry {
		return spamEntry{msgID: id, chID: "c", norm: normalize(text), at: base.Add(time.Duration(sec) * time.Second)}
	}
	const win, n = 15 * time.Second, 5

	// Four identical messages is fine, the fifth trips it and returns all five.
	for i := 0; i < 4; i++ {
		if hit := tr.add("u", msg(string(rune('a'+i)), "BUY NOW", i), win, n); hit != nil {
			t.Fatalf("flagged too early at %d", i)
		}
	}
	hit := tr.add("u", msg("e", "buy   now", 4), win, n)
	if len(hit) != 5 {
		t.Fatalf("want 5 flagged, got %d", len(hit))
	}
	// Tracking was reset: the next one starts a new count.
	if tr.add("u", msg("f", "buy now", 5), win, n) != nil {
		t.Error("should start fresh after a flag")
	}

	// Different messages never add up; other users are tracked separately.
	for i := 0; i < 10; i++ {
		if tr.add("v", msg("x", string(rune('a'+i)), i), win, n) != nil {
			t.Error("different content flagged")
		}
	}
	for i := 0; i < 4; i++ {
		tr.add("w", msg("y", "same", i), win, n)
	}
	if tr.add("z", msg("y", "same", 4), win, n) != nil {
		t.Error("users must be tracked separately")
	}

	// Slow repeats outside the window are not spam.
	for i := 0; i < 8; i++ {
		if tr.add("slow", msg("s", "hi", i*20), win, n) != nil {
			t.Error("slow repeats flagged")
		}
	}
}

func TestUniqueMentions(t *testing.T) {
	m := &discordgo.Message{
		Mentions:     []*discordgo.User{{ID: "1"}, {ID: "1"}, {ID: "2"}},
		MentionRoles: []string{"1", "9"}, // role "1" is not user "1"
	}
	if got := uniqueMentions(m); got != 4 {
		t.Errorf("got %d, want 4", got)
	}
}

func TestSummary(t *testing.T) {
	if s := summary(defaultSpam); s == "" || defaultSpam.Repeats < 5 || defaultSpam.Mentions < 10 {
		t.Errorf("defaults should stay conservative: %+v", defaultSpam)
	}
}
