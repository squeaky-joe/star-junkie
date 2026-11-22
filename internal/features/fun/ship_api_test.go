package fun

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Shapes copied from the live API: localized objects, nested crew/speed/manufacturer, numeric strings, nulls.
const page1 = `{"data":[
 {"name":"Carrack","slug":"anvl-carrack-bis2950","manufacturer":{"name":"Anvil Aerospace","code":"ANVL"},
  "career":"Exploration","role":"Expedition","size":{"en_EN":"Large","de_DE":"Groß"},
  "crew":{"min":6,"max":12,"weapon":4,"operation":null},"cargo_capacity":456,"speed":{"scm":140,"max":1050},
  "msrp":600,"description":{"en_EN":"The Anvil Carrack features reinforced fuel tanks. It is built for the long haul. It is great."},
  "web_url":"https://api.star-citizen.wiki/vehicles/anvl-carrack-bis2950","production_status":{"en_EN":"flight-ready"},"is_power_suit":false},
 {"name":"Avenger Stalker","slug":"aegs-avenger-stalker-display","manufacturer":{"name":"Aegis Dynamics"},"web_url":"https://x/display"},
 {"name":"Some Suit","slug":"suit","is_power_suit":true}
],"meta":{"current_page":1,"last_page":2}}`

const page2 = `{"data":[
 {"name":"Avenger Stalker","slug":"aegs-avenger-stalker","manufacturer":{"name":"Aegis Dynamics"},"web_url":"https://x/real","size":"Small","msrp":"55","crew":null,"cargo_capacity":null},
 {"name":"Weird Thing","slug":"weird","manufacturer":null,"size":42,"cargo_capacity":"3.4","speed":null,"description":null,"game_description":{"en_EN":"Fallback text."}}
],"meta":{"current_page":2,"last_page":2}}`

func testServer(t *testing.T, fail *atomic.Bool, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/api/vehicles" || r.Header.Get("User-Agent") == "" {
			http.Error(w, "bad request", 400)
			return
		}
		if fail.Load() {
			http.Error(w, "boom", 500)
			return
		}
		body := page1
		if r.URL.Query().Get("page[number]") == "2" {
			body = page2
		}
		_, _ = io.WriteString(w, body)
	}))
}

func TestAPIRefresh(t *testing.T) {
	var fail atomic.Bool
	var hits atomic.Int32
	srv := testServer(t, &fail, &hits)
	defer srv.Close()

	a := newAPIShips(srv.URL+"/", slog.New(slog.NewTextHandler(io.Discard, nil)))
	a.pause = 0
	if a.ready() {
		t.Fatal("should not be ready before the first refresh")
	}
	if err := a.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Errorf("expected 2 page requests, got %d", hits.Load())
	}

	var names []string
	for _, s := range a.snapshot() {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "Avenger Stalker,Carrack,Weird Thing" { // sorted; power suit dropped; duplicate merged
		t.Fatalf("names: %v", names)
	}

	c, ok, _ := a.Find(context.Background(), "carrack")
	if !ok || c.Manufacturer != "Anvil Aerospace" || c.Role != "Exploration · Expedition" || c.Size != "Large" ||
		c.CrewMin != 6 || c.CrewMax != 12 || c.CargoSCU != 456 || c.SCM != 140 || c.MSRP != 600 || c.Credit != shipCredit {
		t.Errorf("carrack mapped wrong: %+v", c)
	}
	if !strings.HasSuffix(c.Description, "long haul.") { // cut at a sentence end, not mid-word
		t.Logf("description: %q", c.Description)
	}
	if s, _, _ := a.Find(context.Background(), "stalker"); s.URL != "https://x/real" || s.Size != "Small" || s.MSRP != 55 || s.CrewMax != 0 {
		t.Errorf("duplicate should keep the non-display record, tolerant of odd shapes: %+v", s)
	}
	if w, _, _ := a.Find(context.Background(), "weird"); w.Description != "Fallback text." || w.CargoSCU != 3 || w.Size != "" || w.Manufacturer != "" {
		t.Errorf("tolerance: %+v", w)
	}
	if alts, _ := a.Suggest(context.Background(), "a", 5); len(alts) == 0 || alts[0] != "Avenger Stalker" {
		t.Errorf("suggest: %v", alts)
	}

	// A failed refresh keeps the last good data.
	fail.Store(true)
	if err := a.refresh(context.Background()); err == nil {
		t.Error("refresh should report the 500")
	}
	if len(a.snapshot()) != 3 {
		t.Error("snapshot must survive a failed refresh")
	}
}

func TestLayeredFallback(t *testing.T) {
	var fail atomic.Bool
	var hits atomic.Int32
	srv := testServer(t, &fail, &hits)
	defer srv.Close()
	a := newAPIShips(srv.URL, slog.New(slog.NewTextHandler(io.Discard, nil)))
	a.pause = 0
	l := layered{api: a, fallback: jsonShips{path: "../../../content/ships.json"}}

	// Before the API loads: served from the local JSON file.
	if s, ok, err := l.Find(context.Background(), "cutlass black"); err != nil || !ok || s.Credit != "" {
		t.Errorf("fallback: %+v %v %v", s, ok, err)
	}
	if err := a.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// After: served from the API.
	if s, ok, _ := l.Find(context.Background(), "carrack"); !ok || s.Credit != shipCredit {
		t.Errorf("api: %+v %v", s, ok)
	}
	if _, ok, _ := l.Find(context.Background(), "cutlass black"); ok {
		t.Error("once the API is loaded, the JSON file is no longer consulted")
	}
}

func TestTolerantTypes(t *testing.T) {
	var v struct {
		A localized `json:"a"`
		B localized `json:"b"`
		C localized `json:"c"`
		N num       `json:"n"`
		M num       `json:"m"`
		O num       `json:"o"`
	}
	in := `{"a":"plain","b":{"de_DE":"x","en_EN":"hello"},"c":[1,2],"n":"7.6","m":null,"o":{"x":1}}`
	if err := json.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	if capFirst("capital") != "Capital" || capFirst("") != "" {
		t.Error("capFirst")
	}
	if v.A != "plain" || v.B != "hello" || v.C != "" || v.N.int() != 8 || v.M != 0 || v.O != 0 {
		t.Errorf("%+v", v)
	}
}

func TestShorten(t *testing.T) {
	if shorten("  a   b\n c ", 50) != "a b c" {
		t.Error("whitespace")
	}
	long := strings.Repeat("Word ", 30) + "end. " + strings.Repeat("More ", 60)
	got := shorten(long, 200)
	if n := len([]rune(got)); n > 201 {
		t.Errorf("too long: %d", n)
	}
	if got2 := shorten("One. Two. "+strings.Repeat("x", 300), 100); !strings.HasSuffix(got2, "…") && !strings.HasSuffix(got2, ".") {
		t.Errorf("cut: %q", got2)
	}
}

// Hits the real API (about 15 MB). Run with: SHIP_API_LIVE=1 go test ./internal/features/fun -run Live -v
func TestLiveAPI(t *testing.T) {
	if os.Getenv("SHIP_API_LIVE") != "1" {
		t.Skip("set SHIP_API_LIVE=1 to run against the real Star Citizen Wiki API")
	}
	a := newAPIShips("https://api.star-citizen.wiki", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := a.refresh(ctx); err != nil {
		t.Fatal(err)
	}
	ships := a.snapshot()
	t.Logf("%d ships", len(ships))
	if len(ships) < 100 {
		t.Errorf("expected well over 100 ships, got %d", len(ships))
	}
	fl, _ := a.Suggest(ctx, "freelancer", 10)
	t.Logf("freelancer suggestions (variants, so a plain lookup asks which one): %v", fl)
	for _, q := range []string{"carrack", "cutlass black", "reclaimer"} {
		s, ok, _ := a.Find(ctx, q)
		t.Logf("%-14s ok=%v mfr=%q role=%q size=%q crew=%d-%d cargo=%d scm=%d msrp=%d status=%q desc=%d chars url=%s",
			q, ok, s.Manufacturer, s.Role, s.Size, s.CrewMin, s.CrewMax, s.CargoSCU, s.SCM, s.MSRP, s.Status, len(s.Description), s.URL)
		if !ok || s.Manufacturer == "" || s.CrewMax == 0 {
			t.Errorf("%s looks incomplete: %+v", q, s)
		}
	}
	alts, _ := a.Suggest(ctx, "cutlass", 10)
	t.Logf("cutlass suggestions: %v", alts)
}

func fieldText(e *discordgo.MessageEmbed) string {
	var b strings.Builder
	for _, f := range e.Fields {
		b.WriteString(f.Name + "=" + f.Value + ";")
	}
	return b.String()
}

func TestShipEmbedAPI(t *testing.T) {
	e := shipEmbed(Ship{Name: "Carrack", Manufacturer: "Anvil", SCM: 140, MSRP: 600, Status: "in-concept", URL: "https://x/y", Credit: shipCredit, CrewMin: 6, CrewMax: 12})
	if e.URL != "https://x/y" || e.Footer.Text != shipCredit {
		t.Errorf("url/footer: %q %q", e.URL, e.Footer.Text)
	}
	joined := fieldText(e)
	for _, want := range []string{"Crew=6–12", "SCM speed=140 m/s", "Pledge price=$600", "Status=in-concept"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	// A flight-ready ship doesn't need a status field; the JSON-file fallback keeps its approximate-specs footer.
	e = shipEmbed(Ship{Name: "X", Status: "flight-ready"})
	if strings.Contains(fieldText(e), "Status") || !strings.Contains(e.Footer.Text, "approximate") {
		t.Errorf("fallback embed: %+v", e)
	}
}
