// Package fun: /roll /coinflip /verse-fortune, /history /charter, plus the LFC board and /ship (see lfc.go, ships.go).
package fun

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
)

func Register(r *commands.Registry, d commands.Deps) {
	registerDice(r)
	registerContent(r, d)
	registerLFC(r, d)
	registerShip(r, d)
}

// ---- dice, coin, fortune ----

var rollRe = regexp.MustCompile(`^(\d{0,2})d(\d{1,4})\s*(?:([+-])\s*(\d{1,4}))?$`)

// parseRoll reads "d20", "2d6", "3d8+2", "d100 - 5".
func parseRoll(s string) (n, sides, mod int, err error) {
	m := rollRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return 0, 0, 0, fmt.Errorf("try something like d20 or 2d6+3")
	}
	n = 1
	if m[1] != "" {
		n, _ = strconv.Atoi(m[1])
	}
	sides, _ = strconv.Atoi(m[2])
	if m[4] != "" {
		mod, _ = strconv.Atoi(m[4])
		if m[3] == "-" {
			mod = -mod
		}
	}
	if n < 1 || n > 50 {
		return 0, 0, 0, fmt.Errorf("roll between 1 and 50 dice")
	}
	if sides < 2 || sides > 1000 {
		return 0, 0, 0, fmt.Errorf("dice need 2 to 1000 sides")
	}
	return n, sides, mod, nil
}

// rollDice takes the random source as a parameter so tests can control it.
func rollDice(n, sides, mod int, intn func(int) int) (rolls []int, total int) {
	for range n {
		v := intn(sides) + 1
		rolls = append(rolls, v)
		total += v
	}
	return rolls, total + mod
}

func formatRoll(n, sides, mod int, rolls []int, total int) string {
	label := fmt.Sprintf("%dd%d", n, sides)
	if n == 1 {
		label = fmt.Sprintf("d%d", sides)
	}
	switch {
	case mod > 0:
		label += fmt.Sprintf("+%d", mod)
	case mod < 0:
		label += fmt.Sprintf("%d", mod)
	}
	if n == 1 && mod == 0 {
		return fmt.Sprintf("🎲 %s → **%d**", label, total)
	}
	parts := make([]string, len(rolls))
	for i, v := range rolls {
		parts[i] = strconv.Itoa(v)
	}
	out := fmt.Sprintf("🎲 %s → [%s]", label, strings.Join(parts, ", "))
	switch {
	case mod > 0:
		out += fmt.Sprintf(" + %d", mod)
	case mod < 0:
		out += fmt.Sprintf(" - %d", -mod)
	}
	return out + fmt.Sprintf(" = **%d**", total)
}

var fortunes = []string{
	"You will land on the pad on the first try. Mostly.",
	"A stranger will offer you a fair price for your cargo. Take it.",
	"Your quantum drive spools without a single hiccup today.",
	"Someone in your crew will say \"one more run\". It will not be one more run.",
	"Beware the elevator. You know the one.",
	"Your next landing will be smoother than your last. The bar is low.",
	"You'll find the perfect ship parking spot. Right as you read this, someone else takes it.",
	"A mysterious beacon will lead you somewhere. Pack snacks.",
	"The cargo you hauled will arrive intact. Your dignity may not.",
	"Today's lucky number is 46. Your cargo grid will thank you.",
	"A friendly face will show up exactly when you need a gunner.",
	"The patch will fix the thing you hate. It will also add a new thing to hate.",
	"You will finally find that one lost item. In the last place you looked, naturally.",
	"A quiet moment in the verse awaits. Take a screenshot.",
	"Your mining run will hit a big rock. Keep an eye on that instability bar.",
	"Fortune smiles on you: no one will ask you to \"quickly\" help move cargo today.",
	"The stars align: everyone shows up on time for game night.",
	"You will take a wrong turn and discover something amazing.",
	"Your ship will be fine. Probably. Hit the wing before you say that out loud.",
	"Chill vibes incoming. Doing nothing counts as a plan.",
	"Someone's about to say \"I can fly it\". Brace.",
	"The verse is big and the crew is bigger. You're never flying alone.",
}

func registerDice(r *commands.Registry) {
	noDM := false
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "roll", Description: "Roll some dice", DMPermission: &noDM,
			Options: []*discordgo.ApplicationCommandOption{{
				Type: discordgo.ApplicationCommandOptionString, Name: "dice", MaxLength: 16,
				Description: "Like d20, 2d6 or 3d8+2 (default d20)",
			}},
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			spec := "d20"
			if o := commands.OptMap(i.ApplicationCommandData().Options)["dice"]; o != nil {
				spec = o.StringValue()
			}
			n, sides, mod, err := parseRoll(spec)
			if err != nil {
				_ = commands.Ephemeral(s, i, err.Error())
				return
			}
			rolls, total := rollDice(n, sides, mod, rand.IntN)
			_ = commands.Public(s, i, formatRoll(n, sides, mod, rolls, total))
		},
	})
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{Name: "coinflip", Description: "Flip a coin", DMPermission: &noDM},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			side := map[int]string{0: "Heads", 1: "Tails"}[rand.IntN(2)]
			_ = commands.Public(s, i, "🪙 **"+side+"**!")
		},
	})
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{Name: "verse-fortune", Description: "Get your fortune for the verse", DMPermission: &noDM},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{{
						Title: "🔮 Your verse fortune", Description: fortunes[rand.IntN(len(fortunes))], Color: commands.ColorIndigo,
					}},
					AllowedMentions: &discordgo.MessageAllowedMentions{},
				},
			})
		},
	})
}

// ---- /history and /charter: posted from editable files in the content folder ----

const embedLimit = 4000 // Discord allows 4096 characters in an embed description

// fit trims text to fit one embed.
func fit(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= embedLimit {
		return s
	}
	return string(r[:embedLimit-60]) + "…\n\n*(Trimmed, this one's too long for a single post.)*"
}

func registerContent(r *commands.Registry, d commands.Deps) {
	noDM := false
	for _, c := range []struct{ name, file, title, desc string }{
		{"history", "history.md", "📜 Star Junkies history", "Post the story of the org"},
		{"charter", "charter.md", "📋 Star Junkies charter", "Post the org charter"},
	} {
		r.Add(commands.Command{
			Def: &discordgo.ApplicationCommand{Name: c.name, Description: c.desc, DMPermission: &noDM},
			Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
				data, err := os.ReadFile(filepath.Join(d.ContentDir, c.file)) // fixed names only, never user input
				if err != nil || strings.TrimSpace(string(data)) == "" {
					d.Log.Warn("content file", "file", c.file, "err", err)
					_ = commands.Ephemeral(s, i, "I can't find the text for that yet. Ask a Founder to add content/"+c.file+" 🙂")
					return
				}
				_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Embeds:          []*discordgo.MessageEmbed{{Title: c.title, Description: fit(string(data)), Color: commands.ColorCream}},
						AllowedMentions: &discordgo.MessageAllowedMentions{}, // never pings, even if the text says @everyone
					},
				})
			},
		})
	}
}
