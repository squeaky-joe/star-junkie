package fun

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestParseRoll(t *testing.T) {
	ok := []struct {
		in           string
		n, sides, md int
	}{
		{"d20", 1, 20, 0}, {"2d6", 2, 6, 0}, {"3d8+2", 3, 8, 2}, {" D100 - 5 ", 1, 100, -5}, {"50d1000+1000", 50, 1000, 1000},
	}
	for _, c := range ok {
		n, sides, md, err := parseRoll(c.in)
		if err != nil || n != c.n || sides != c.sides || md != c.md {
			t.Errorf("%q: got %d %d %d %v", c.in, n, sides, md, err)
		}
	}
	for _, bad := range []string{"", "20", "d", "0d6", "51d6", "d1", "d1001", "2d6+", "2d6*3", "d20 d20", "99999d6", "-d6"} {
		if _, _, _, err := parseRoll(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestRollDice(t *testing.T) {
	lo, tot := rollDice(3, 6, 2, func(int) int { return 0 }) // always rolls 1
	if len(lo) != 3 || tot != 5 {
		t.Errorf("min: %v %d", lo, tot)
	}
	hi, tot := rollDice(2, 6, -1, func(n int) int { return n - 1 }) // always rolls max
	if hi[0] != 6 || tot != 11 {
		t.Errorf("max: %v %d", hi, tot)
	}
}

func TestFormatRoll(t *testing.T) {
	cases := map[string]string{
		formatRoll(1, 20, 0, []int{14}, 14):  "🎲 d20 → **14**",
		formatRoll(2, 6, 3, []int{4, 5}, 12): "🎲 2d6+3 → [4, 5] + 3 = **12**",
		formatRoll(2, 6, -2, []int{4, 5}, 7): "🎲 2d6-2 → [4, 5] - 2 = **7**",
		formatRoll(1, 8, 2, []int{3}, 5):     "🎲 d8+2 → [3] + 2 = **5**",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestFortunes(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fortunes {
		if strings.TrimSpace(f) == "" || seen[f] || len(f) > 200 {
			t.Errorf("bad or duplicate fortune: %q", f)
		}
		seen[f] = true
	}
}

func TestFit(t *testing.T) {
	if fit("  hello \n") != "hello" {
		t.Error("trim")
	}
	long := fit(strings.Repeat("a", 5000))
	if n := len([]rune(long)); n > 4096 || !strings.Contains(long, "Trimmed") {
		t.Errorf("long text: %d runes", n)
	}
}

func TestShipLookup(t *testing.T) {
	ships := []Ship{{Name: "Cutlass Black"}, {Name: "Constellation Andromeda"}, {Name: "Constellation Aquila"}, {Name: "Hornet F7C"}}
	cases := []struct {
		q    string
		want string
	}{
		{"cutlass black", "Cutlass Black"}, // exact, any case
		{"cutlass", "Cutlass Black"},       // only match
		{"hornet", "Hornet F7C"},
		{"andromeda", "Constellation Andromeda"}, // contains, unique
		{"constellation", ""},                    // ambiguous
		{"nope", ""}, {"", ""},
	}
	for _, c := range cases {
		got, ok := matchShip(ships, c.q)
		if (c.want == "") == ok || got.Name != c.want {
			t.Errorf("%q: got %q ok=%v, want %q", c.q, got.Name, ok, c.want)
		}
	}
	// A base ship plus its variants resolves to the base; unrelated look-alikes stay ambiguous.
	fl := []Ship{{Name: "MISC Freelancer MAX"}, {Name: "MISC Freelancer"}, {Name: "MISC Freelancer DUR"}, {Name: "Cutlass Black"}, {Name: "Cutlass Blue"}}
	if s, ok := matchShip(fl, "freelancer"); !ok || s.Name != "MISC Freelancer" {
		t.Errorf("base ship: %v %v", s.Name, ok)
	}
	if _, ok := matchShip(fl, "cutlass"); ok {
		t.Error("cutlass black vs blue must stay ambiguous")
	}
	if got := suggest(ships, "const", 5); len(got) != 2 {
		t.Errorf("suggest: %v", got)
	}
	if got := suggest(ships, "", 2); len(got) != 2 {
		t.Errorf("suggest limit: %v", got)
	}
}

func TestLFC(t *testing.T) {
	p := lfcPost{ID: 4, Host: "9", Activity: "Hauling", Ship: "Freelancer", Slots: 2}
	e := lfcEmbed(p, nil)
	if !strings.Contains(e.Title, "Hauling") || !strings.Contains(e.Fields[len(e.Fields)-1].Name, "(0/2)") || len(e.Fields) != 3 {
		t.Errorf("empty: %+v", e)
	}
	btn := func(filled int) (label string, disabled bool) {
		b := lfcButtons(p, filled)[0].(discordgo.ActionsRow).Components[0].(discordgo.Button)
		return b.Label, b.Disabled
	}
	if l, dis := btn(1); l != "Join" || dis {
		t.Errorf("1/2: %s %v", l, dis)
	}
	if l, dis := btn(2); l != "Full" || !dis {
		t.Errorf("2/2: %s %v", l, dis)
	}
	full := lfcEmbed(p, []string{"1", "2"})
	if !strings.Contains(full.Footer.Text, "full") || !strings.Contains(full.Fields[2].Value, "<@2>") {
		t.Errorf("full: %+v", full)
	}
}
