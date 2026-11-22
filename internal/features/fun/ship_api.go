package fun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/starjunkies/bot/internal/commands"
)

// shipCredit is shown on every API-backed embed (the Wiki API asks to be credited).
const shipCredit = "Data: Star Citizen Wiki (star-citizen.wiki)"

// ---- tolerant JSON types: a surprising field shape must never break the whole download ----

// localized reads either "text" or {"en_EN": "text", "de_DE": ...}.
type localized string

func (l *localized) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*l = localized(s)
		return nil
	}
	var m map[string]string
	if json.Unmarshal(b, &m) == nil {
		for _, k := range []string{"en_EN", "en"} {
			if v := m[k]; v != "" {
				*l = localized(v)
				break
			}
		}
	}
	return nil
}

// num reads a number, a numeric string, or null (as 0).
type num float64

func (n *num) UnmarshalJSON(b []byte) error {
	var f float64
	if json.Unmarshal(b, &f) == nil {
		*n = num(f)
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			*n = num(f)
		}
	}
	return nil
}

func (n num) int() int { return int(math.Round(float64(n))) }

// apiVehicle is the slice of /api/vehicles we use (the API returns far more).
type apiVehicle struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Manufacturer struct {
		Name string `json:"name"`
	} `json:"manufacturer"`
	Career string    `json:"career"`
	Role   string    `json:"role"`
	Size   localized `json:"size"`
	Crew   struct {
		Min num `json:"min"`
		Max num `json:"max"`
	} `json:"crew"`
	CargoCapacity num `json:"cargo_capacity"`
	Speed         struct {
		SCM num `json:"scm"`
	} `json:"speed"`
	MSRP            num       `json:"msrp"`
	Description     localized `json:"description"`
	GameDescription localized `json:"game_description"`
	WebURL          string    `json:"web_url"`
	Status          localized `json:"production_status"`
	IsPowerSuit     bool      `json:"is_power_suit"`
}

// shorten collapses whitespace and cuts text to about n characters, preferring a sentence end.
func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndex(cut, ". "); i > n/2 {
		return cut[:i+1]
	}
	return strings.TrimSpace(cut) + "…"
}

func capFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[:1])) + string(r[1:])
}

func (v apiVehicle) ship() Ship {
	role := v.Career
	switch {
	case v.Career == "":
		role = v.Role
	case v.Role != "" && v.Role != v.Career:
		role = v.Career + " · " + v.Role
	}
	desc := string(v.Description)
	if desc == "" {
		desc = string(v.GameDescription)
	}
	return Ship{
		Name: strings.TrimSpace(v.Name), Manufacturer: v.Manufacturer.Name, Role: role, Size: capFirst(string(v.Size)),
		CrewMin: v.Crew.Min.int(), CrewMax: v.Crew.Max.int(), CargoSCU: v.CargoCapacity.int(),
		SCM: v.Speed.SCM.int(), MSRP: v.MSRP.int(), Status: string(v.Status),
		Description: shorten(desc, 350), URL: v.WebURL, Credit: shipCredit,
	}
}

// toShips drops power suits, merges same-named entries (the API lists display variants
// as separate records, "...-display"), and sorts by name.
func toShips(vs []apiVehicle) []Ship {
	best := map[string]apiVehicle{}
	for _, v := range vs {
		key := strings.ToLower(strings.TrimSpace(v.Name))
		if v.IsPowerSuit || key == "" {
			continue
		}
		cur, ok := best[key]
		if !ok {
			best[key] = v
			continue
		}
		curDisplay, vDisplay := strings.HasSuffix(cur.Slug, "-display"), strings.HasSuffix(v.Slug, "-display")
		if (curDisplay && !vDisplay) || (curDisplay == vDisplay && len(v.Slug) < len(cur.Slug)) {
			best[key] = v
		}
	}
	out := make([]Ship, 0, len(best))
	for _, v := range best {
		out = append(out, v.ship())
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// ---- the API-backed source ----

// apiShips holds a snapshot of every ship from the Star Citizen Wiki API. It downloads the whole
// list in the background (the per-ship records are large and search is rate-limited to 60/min),
// so lookups and autocomplete are instant and never hit the API.
type apiShips struct {
	base   string
	client *http.Client
	log    *slog.Logger
	pause  time.Duration // between page requests, to be polite

	mu    sync.RWMutex
	ships []Ship
}

func newAPIShips(base string, log *slog.Logger) *apiShips {
	return &apiShips{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: 90 * time.Second}, log: log, pause: 2 * time.Second}
}

func (a *apiShips) snapshot() []Ship {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ships
}

func (a *apiShips) ready() bool { return len(a.snapshot()) > 0 }

func (a *apiShips) Find(_ context.Context, q string) (Ship, bool, error) {
	s, ok := matchShip(a.snapshot(), q)
	return s, ok, nil
}

func (a *apiShips) Suggest(_ context.Context, q string, limit int) ([]string, error) {
	return suggest(a.snapshot(), q, limit), nil
}

type apiPage struct {
	Data []apiVehicle `json:"data"`
	Meta struct {
		CurrentPage int `json:"current_page"`
		LastPage    int `json:"last_page"`
	} `json:"meta"`
}

func (a *apiShips) fetchPage(ctx context.Context, n int) (apiPage, error) {
	var p apiPage
	q := url.Values{"page[size]": {"100"}, "page[number]": {strconv.Itoa(n)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/api/vehicles?"+q.Encode(), nil)
	if err != nil {
		return p, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "discord-org-bot/1.0 (non-commercial)")
	resp, err := a.client.Do(req)
	if err != nil {
		return p, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return p, fmt.Errorf("ship API returned %s", resp.Status)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&p)
	return p, err
}

// refresh downloads every page and swaps in the new snapshot. On any error the old snapshot stays.
func (a *apiShips) refresh(ctx context.Context) error {
	var all []apiVehicle
	for page := 1; page <= 20; page++ { // 20 pages of 100 is far beyond the real size; guards against a runaway loop
		p, err := a.fetchPage(ctx, page)
		if err != nil {
			return fmt.Errorf("page %d: %w", page, err)
		}
		all = append(all, p.Data...)
		if p.Meta.CurrentPage >= p.Meta.LastPage {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.pause):
		}
	}
	ships := toShips(all)
	if len(ships) == 0 {
		return fmt.Errorf("ship API returned no ships")
	}
	a.mu.Lock()
	a.ships = ships
	a.mu.Unlock()
	a.log.Info("ship data refreshed", "ships", len(ships))
	return nil
}

// run refreshes now and then every 12 hours (retrying every 5 minutes after a failure) until ctx ends.
func (a *apiShips) run(ctx context.Context) {
	for {
		wait := 12 * time.Hour
		if err := a.refresh(ctx); err != nil {
			a.log.Warn("ship data refresh failed", "err", err)
			wait = 5 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// layered uses the API snapshot once it has loaded, and the local JSON file until then (or if it never does).
type layered struct {
	api      *apiShips
	fallback ShipSource
}

func (l layered) Find(ctx context.Context, q string) (Ship, bool, error) {
	if l.api.ready() {
		return l.api.Find(ctx, q)
	}
	return l.fallback.Find(ctx, q)
}

func (l layered) Suggest(ctx context.Context, q string, limit int) ([]string, error) {
	if l.api.ready() {
		return l.api.Suggest(ctx, q, limit)
	}
	return l.fallback.Suggest(ctx, q, limit)
}

// newShipSource picks the API (with JSON fallback), or JSON only when SHIP_API_URL is "off".
func newShipSource(ctx context.Context, d commands.Deps) ShipSource {
	j := jsonShips{path: filepath.Join(d.ContentDir, "ships.json")}
	if d.ShipAPI == "" {
		return j
	}
	a := newAPIShips(d.ShipAPI, d.Log)
	go a.run(ctx)
	return layered{api: a, fallback: j}
}
