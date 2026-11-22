package fun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
)

type Ship struct {
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer"`
	Role         string `json:"role"`
	Size         string `json:"size"`
	CrewMin      int    `json:"crew_min"`
	CrewMax      int    `json:"crew_max"`
	CargoSCU     int    `json:"cargo_scu"`
	Description  string `json:"description"`
}

// ShipSource is where ship info comes from. Today it's a JSON file; to use a real API,
// implement these two methods and pass it to newShipHandlers in registerShip.
type ShipSource interface {
	Find(ctx context.Context, query string) (Ship, bool, error)
	Suggest(ctx context.Context, query string, limit int) ([]string, error)
}

// jsonShips reads content/ships.json on every call, so edits show up without a restart.
// ponytail: re-reads the file per lookup; fine for a small file, cache by mtime if it ever grows.
type jsonShips struct{ path string }

func (j jsonShips) load() ([]Ship, error) {
	data, err := os.ReadFile(j.path)
	if err != nil {
		return nil, err
	}
	var f struct {
		Ships []Ship `json:"ships"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", j.path, err)
	}
	return f.Ships, nil
}

func (j jsonShips) Find(_ context.Context, q string) (Ship, bool, error) {
	ships, err := j.load()
	if err != nil {
		return Ship{}, false, err
	}
	s, ok := matchShip(ships, q)
	return s, ok, nil
}

func (j jsonShips) Suggest(_ context.Context, q string, limit int) ([]string, error) {
	ships, err := j.load()
	if err != nil {
		return nil, err
	}
	return suggest(ships, q, limit), nil
}

// matchShip: exact name, else the only ship whose name contains the query,
// else the only one whose name starts with it.
func matchShip(ships []Ship, q string) (Ship, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return Ship{}, false
	}
	var contains, prefix []Ship
	for _, s := range ships {
		n := strings.ToLower(s.Name)
		switch {
		case n == q:
			return s, true
		case strings.HasPrefix(n, q):
			prefix = append(prefix, s)
			contains = append(contains, s)
		case strings.Contains(n, q):
			contains = append(contains, s)
		}
	}
	if len(contains) == 1 {
		return contains[0], true
	}
	if len(prefix) == 1 {
		return prefix[0], true
	}
	return Ship{}, false
}

func suggest(ships []Ship, q string, limit int) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []string
	for _, s := range ships {
		if len(out) == limit {
			break
		}
		if strings.Contains(strings.ToLower(s.Name), q) {
			out = append(out, s.Name)
		}
	}
	return out
}

func shipEmbed(s Ship) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{
		Title: s.Name, Description: s.Description, Color: commands.ColorOrange,
		Footer: &discordgo.MessageEmbedFooter{Text: "Specs are approximate. Check the official site for current numbers."},
	}
	add := func(name, val string) {
		if val != "" {
			e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: name, Value: val, Inline: true})
		}
	}
	add("Manufacturer", s.Manufacturer)
	add("Role", s.Role)
	add("Size", s.Size)
	switch {
	case s.CrewMax > s.CrewMin && s.CrewMin > 0:
		add("Crew", fmt.Sprintf("%d–%d", s.CrewMin, s.CrewMax))
	case s.CrewMax > 0:
		add("Crew", fmt.Sprint(s.CrewMax))
	}
	if s.CargoSCU > 0 {
		add("Cargo", fmt.Sprintf("%d SCU", s.CargoSCU))
	}
	return e
}

func registerShip(r *commands.Registry, d commands.Deps) {
	src := ShipSource(jsonShips{path: filepath.Join(d.ContentDir, "ships.json")})
	noDM := false
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "ship", Description: "Look up a ship", DMPermission: &noDM,
			Options: []*discordgo.ApplicationCommandOption{{
				Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Ship name",
				Required: true, Autocomplete: true, MaxLength: 60,
			}},
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			q := i.ApplicationCommandData().Options[0].StringValue()
			ship, ok, err := src.Find(ctx, q)
			if err != nil {
				d.Log.Error("ship lookup", "err", err)
				_ = commands.Ephemeral(s, i, "I can't read my ship list right now. Ask a Founder to check content/ships.json.")
				return
			}
			if !ok {
				msg := fmt.Sprintf("I don't have %q in my ship list yet.", q)
				if alts, _ := src.Suggest(ctx, q, 5); len(alts) > 0 {
					msg += " Did you mean: " + strings.Join(alts, ", ") + "?"
				}
				_ = commands.Ephemeral(s, i, msg)
				return
			}
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{shipEmbed(ship)}, AllowedMentions: &discordgo.MessageAllowedMentions{},
				},
			})
		},
		Autocomplete: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			q := ""
			for _, o := range i.ApplicationCommandData().Options {
				if o.Focused {
					q = o.StringValue()
				}
			}
			names, _ := src.Suggest(ctx, q, 25)
			choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(names))
			for _, n := range names {
				choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: n, Value: n})
			}
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionApplicationCommandAutocompleteResult,
				Data: &discordgo.InteractionResponseData{Choices: choices},
			})
		},
	})
}
