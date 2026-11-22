// Package commands holds the slash-command registry and small reply helpers shared by features.
package commands

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Org palette.
const (
	ColorIndigo = 0x2A2550
	ColorCream  = 0xF3EAD3
	ColorOrange = 0xF2A04B
	ColorTeal   = 0x3B9C9C
)

// Deps is what feature handlers need.
type Deps struct {
	DB         *sql.DB
	Log        *slog.Logger
	ContentDir string // editable markdown/JSON (history, charter, ships)
	ShipAPI    string // base URL of the Star Citizen Wiki API; empty = use content/ships.json only
}

type HandlerFunc func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate)

type Command struct {
	Def          *discordgo.ApplicationCommand
	Handle       HandlerFunc
	Autocomplete HandlerFunc // optional: answers autocomplete requests for this command's options
}

type Registry struct {
	cmds  map[string]Command
	comps map[string]HandlerFunc // by custom-ID prefix (text before the first ":")
}

func NewRegistry() *Registry {
	return &Registry{cmds: map[string]Command{}, comps: map[string]HandlerFunc{}}
}

// Component routes buttons/selects/modals whose custom ID starts with "prefix:".
func (r *Registry) Component(prefix string, h HandlerFunc) { r.comps[prefix] = h }

func (r *Registry) Add(c Command) { r.cmds[c.Def.Name] = c }

// Defs returns the definitions to send to Discord.
func (r *Registry) Defs() []*discordgo.ApplicationCommand {
	out := make([]*discordgo.ApplicationCommand, 0, len(r.cmds))
	for _, c := range r.cmds {
		out = append(out, c.Def)
	}
	return out
}

func (r *Registry) Dispatch(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		if c, ok := r.cmds[i.ApplicationCommandData().Name]; ok {
			c.Handle(ctx, s, i)
		}
	case discordgo.InteractionApplicationCommandAutocomplete:
		if c, ok := r.cmds[i.ApplicationCommandData().Name]; ok && c.Autocomplete != nil {
			c.Autocomplete(ctx, s, i)
		}
	case discordgo.InteractionMessageComponent, discordgo.InteractionModalSubmit:
		id := ""
		if i.Type == discordgo.InteractionMessageComponent {
			id = i.MessageComponentData().CustomID
		} else {
			id = i.ModalSubmitData().CustomID
		}
		prefix, _, _ := strings.Cut(id, ":")
		if h, ok := r.comps[prefix]; ok {
			h(ctx, s, i)
		}
	}
}

// Ephemeral replies privately (errors, admin output). Never pings anyone.
func Ephemeral(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         msg,
			Flags:           discordgo.MessageFlagsEphemeral,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
}

// DeferEphemeral acknowledges a slow command; follow up with s.InteractionResponseEdit.
func DeferEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
}

func IsAdmin(i *discordgo.InteractionCreate) bool {
	return i.Member != nil && i.Member.Permissions&discordgo.PermissionAdministrator != 0
}
