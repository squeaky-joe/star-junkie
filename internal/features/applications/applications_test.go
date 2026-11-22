package applications

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestParseCustomID(t *testing.T) {
	cases := []struct {
		in     string
		action string
		id     int64
		ok     bool
	}{
		{"app:accept:12", "accept", 12, true},
		{"app:decline:3", "decline", 3, true},
		{"app:submit", "", 0, false},
		{"app:promote:1", "", 0, false},
		{"app:accept:x", "", 0, false},
		{"role:accept:1", "", 0, false},
	}
	for _, c := range cases {
		a, id, ok := parseCustomID(c.in)
		if a != c.action || id != c.id || ok != c.ok {
			t.Errorf("%q: got (%q,%d,%v)", c.in, a, id, ok)
		}
	}
	if got := customID("accept", 7); got != "app:accept:7" {
		t.Errorf("customID = %q", got)
	}
}

func TestModalValues(t *testing.T) {
	rows := []discordgo.MessageComponent{
		&discordgo.ActionsRow{Components: []discordgo.MessageComponent{&discordgo.TextInput{CustomID: "handle", Value: "  Cmdr_Foo \n"}}},
		&discordgo.ActionsRow{Components: []discordgo.MessageComponent{&discordgo.TextInput{CustomID: "found", Value: "a friend"}}},
	}
	v := modalValues(rows)
	if v["handle"] != "Cmdr_Foo" || v["found"] != "a friend" {
		t.Errorf("got %v", v)
	}
}
