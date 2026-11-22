package commands

import (
	"context"
	"slices"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/db"
)

// RoleID returns the ID /setup recorded for a role key ("guest", "junkie", "copilot", ...), or "".
func RoleID(ctx context.Context, d Deps, guildID, key string) string {
	id, _ := db.GetConfig(ctx, d.DB, guildID, "role."+key)
	return id
}

// ChannelID returns the ID /setup recorded for a channel name, or "".
func ChannelID(ctx context.Context, d Deps, guildID, name string) string {
	id, _ := db.GetConfig(ctx, d.DB, guildID, "channel."+name)
	return id
}

// IsStaff: Administrator, Founder or Co-Pilot.
func IsStaff(ctx context.Context, d Deps, i *discordgo.InteractionCreate) bool {
	if IsAdmin(i) {
		return true
	}
	if i.Member == nil {
		return false
	}
	for _, key := range []string{"copilot", "founder"} {
		if id := RoleID(ctx, d, i.GuildID, key); id != "" && slices.Contains(i.Member.Roles, id) {
			return true
		}
	}
	return false
}

// PostLog drops an embed into #server-logs (if /setup has made it). Never pings anyone.
func PostLog(ctx context.Context, s *discordgo.Session, d Deps, guildID string, e *discordgo.MessageEmbed) bool {
	ch := ChannelID(ctx, d, guildID, "server-logs")
	if ch == "" {
		return false
	}
	if e.Color == 0 {
		e.Color = ColorTeal
	}
	if _, err := s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{e},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}, discordgo.WithContext(ctx)); err != nil {
		d.Log.Warn("post log", "err", err)
		return false
	}
	return true
}

// OptMap indexes a command's options by name.
func OptMap(opts []*discordgo.ApplicationCommandInteractionDataOption) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	m := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(opts))
	for _, o := range opts {
		m[o.Name] = o
	}
	return m
}

// SendDM messages a user privately; it fails if their DMs are closed.
func SendDM(s *discordgo.Session, userID, text string) error {
	ch, err := s.UserChannelCreate(userID)
	if err != nil {
		return err
	}
	_, err = s.ChannelMessageSend(ch.ID, text)
	return err
}

// Public replies in-channel, pinging only the listed users.
func Public(s *discordgo.Session, i *discordgo.InteractionCreate, msg string, pingUsers ...string) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         msg,
			AllowedMentions: &discordgo.MessageAllowedMentions{Users: pingUsers},
		},
	})
}
