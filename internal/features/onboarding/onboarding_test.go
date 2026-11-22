package onboarding

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/features/setup"
)

func TestPanelRows(t *testing.T) {
	rows := panelRows()
	total, seen := 0, map[string]bool{}
	for _, r := range rows {
		btns := r.(discordgo.ActionsRow).Components
		if len(btns) > 5 {
			t.Fatalf("row has %d buttons, max 5", len(btns))
		}
		for _, b := range btns {
			id := b.(discordgo.Button).CustomID
			if !strings.HasPrefix(id, "role:") || seen[id] {
				t.Errorf("bad or duplicate custom id %q", id)
			}
			seen[id] = true
			total++
		}
	}
	if total != len(setup.PingRoles) {
		t.Errorf("got %d buttons, want %d", total, len(setup.PingRoles))
	}
	if setup.PingKey("Game Night") != "game-night" {
		t.Error("PingKey")
	}
}
