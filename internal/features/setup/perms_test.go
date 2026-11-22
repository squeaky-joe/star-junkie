package setup

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

var ids = RoleIDs{Everyone: "e", Founder: "f", CoPilot: "c", Bots: "b", Junkie: "j", Guest: "g", BotUser: "u"}

func find(ow []*discordgo.PermissionOverwrite, id string) *discordgo.PermissionOverwrite {
	for _, o := range ow {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func TestOverwrites(t *testing.T) {
	// Staff: everyone can't see, guest/junkie have no overwrite at all.
	staff := Overwrites(ids, Staff, false)
	if find(staff, "e").Deny&discordgo.PermissionViewChannel == 0 {
		t.Error("staff channel must hide from @everyone")
	}
	if find(staff, "g") != nil || find(staff, "j") != nil {
		t.Error("staff channel must not grant guest/junkie")
	}
	if find(staff, "c").Allow&discordgo.PermissionViewChannel == 0 {
		t.Error("co-pilot must see staff channel")
	}

	// Junkies: guest can't see.
	if find(Overwrites(ids, Junkies, false), "g") != nil {
		t.Error("guest must not be granted in junkie-only category")
	}

	// Read-only members channel: guest can read, can't send; co-pilot can send.
	ro := Overwrites(ids, Members, true)
	g := find(ro, "g")
	if g.Allow&discordgo.PermissionViewChannel == 0 || g.Deny&discordgo.PermissionSendMessages == 0 {
		t.Errorf("guest should read but not send: %+v", g)
	}
	if find(ro, "c").Allow&discordgo.PermissionSendMessages == 0 {
		t.Error("co-pilot must be able to post in read-only")
	}

	// Public read-only: @everyone can see but not talk. Public open: can talk.
	if o := find(Overwrites(ids, Public, true), "e"); o.Deny&discordgo.PermissionSendMessages == 0 {
		t.Error("public read-only must deny send")
	}
	if o := find(Overwrites(ids, Public, false), "e"); o.Allow&discordgo.PermissionSendMessages == 0 {
		t.Error("public open must allow send")
	}

	// Bot user gets a member overwrite; allow/deny never overlap anywhere.
	if o := find(staff, "u"); o == nil || o.Type != discordgo.PermissionOverwriteTypeMember {
		t.Error("bot user needs a member overwrite")
	}
	for _, a := range []Access{Public, Members, Junkies, Staff} {
		for _, ro := range []bool{false, true} {
			for _, o := range Overwrites(ids, a, ro) {
				if o.Allow&o.Deny != 0 {
					t.Errorf("access %d ro=%v id=%s: allow and deny overlap", a, ro, o.ID)
				}
			}
		}
	}
}

func TestLayoutSanity(t *testing.T) {
	seen := map[string]bool{}
	afk := 0
	for _, c := range Categories {
		for _, ch := range c.Channels {
			if seen[ch.Name] {
				t.Errorf("duplicate channel %q", ch.Name)
			}
			seen[ch.Name] = true
			if ch.AFK {
				afk++
			}
		}
	}
	if afk != 1 {
		t.Errorf("want exactly one AFK channel, got %d", afk)
	}
}
