// Package onboarding: self-serve ping roles, join welcome, and /promote /demote.
package onboarding

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
	"github.com/starjunkies/bot/internal/db"
	"github.com/starjunkies/bot/internal/features/setup"
)

func Register(r *commands.Registry, d commands.Deps) {
	admin := int64(discordgo.PermissionAdministrator)
	mod := int64(discordgo.PermissionModerateMembers) // Co-Pilots have it; runtime check is the real gate
	noDM := false
	userOpt := func(desc string) []*discordgo.ApplicationCommandOption {
		return []*discordgo.ApplicationCommandOption{{
			Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: desc, Required: true,
		}}
	}

	r.Component("role", func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
		toggleRole(ctx, s, i, d)
	})
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "rolepanel", Description: "Post the ping-role buttons in #get-roles (admin)",
			DefaultMemberPermissions: &admin, DMPermission: &noDM,
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			postPanel(ctx, s, i, d)
		},
	})
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "promote", Description: "Make a Guest a Star Junkie", Options: userOpt("Who to promote"),
			DefaultMemberPermissions: &mod, DMPermission: &noDM,
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			move(ctx, s, i, d, "junkie", "guest", "promote", "is now a 🚀 Star Junkie. Welcome to the crew!")
		},
	})
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "demote", Description: "Move a Star Junkie back to Guest", Options: userOpt("Who to demote"),
			DefaultMemberPermissions: &mod, DMPermission: &noDM,
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			move(ctx, s, i, d, "guest", "junkie", "demote", "is back to 🛰️ Guest for now.")
		},
	})
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "config", Description: "Bot settings (admin)",
			DefaultMemberPermissions: &admin, DMPermission: &noDM,
			Options: []*discordgo.ApplicationCommandOption{{
				Type: discordgo.ApplicationCommandOptionSubCommand, Name: "welcome-channel",
				Description: "Where to welcome new members",
				Options: []*discordgo.ApplicationCommandOption{{
					Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Text channel", Required: true,
					ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText},
				}},
			}, {
				Type: discordgo.ApplicationCommandOptionSubCommand, Name: "timezone",
				Description: "Default time zone for /event create",
				Options: []*discordgo.ApplicationCommandOption{{
					Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Like America/New_York", Required: true,
				}},
			}},
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			setWelcome(ctx, s, i, d)
		},
	})
}

// AddHandlers hooks gateway events (needs the Server Members intent).
func AddHandlers(ctx context.Context, s *discordgo.Session, d commands.Deps) {
	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		onJoin(cctx, s, d, m)
	})
}

// ---- ping-role panel ----

// panelRows lays the ping roles out as buttons, five per row (Discord's limit).
func panelRows() []discordgo.MessageComponent {
	var rows []discordgo.MessageComponent
	var cur []discordgo.MessageComponent
	for _, name := range setup.PingRoles {
		cur = append(cur, discordgo.Button{Label: name, Style: discordgo.SecondaryButton, CustomID: "role:" + setup.PingKey(name)})
		if len(cur) == 5 {
			rows = append(rows, discordgo.ActionsRow{Components: cur})
			cur = nil
		}
	}
	if len(cur) > 0 {
		rows = append(rows, discordgo.ActionsRow{Components: cur})
	}
	return rows
}

func postPanel(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	if !commands.IsAdmin(i) {
		_ = commands.Ephemeral(s, i, "Admins only, sorry 🔒")
		return
	}
	ch := commands.ChannelID(ctx, d, i.GuildID, "get-roles")
	if ch == "" {
		_ = commands.Ephemeral(s, i, "I can't find #get-roles. Run /setup first.")
		return
	}
	_, err := s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Title:       "Pick your pings 🔔",
			Description: "Tap a button to turn that ping on or off. Only get pinged for stuff you actually care about.",
			Color:       commands.ColorTeal,
		}},
		Components:      panelRows(),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}, discordgo.WithContext(ctx))
	if err != nil {
		_ = commands.Ephemeral(s, i, "Couldn't post the panel: "+err.Error())
		return
	}
	_ = commands.Ephemeral(s, i, "Posted the role panel in <#"+ch+"> ✅")
}

func toggleRole(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	_, key, _ := strings.Cut(i.MessageComponentData().CustomID, ":")
	// Only the known ping roles can be toggled, never staff roles.
	name := ""
	for _, n := range setup.PingRoles {
		if setup.PingKey(n) == key {
			name = n
		}
	}
	id := commands.RoleID(ctx, d, i.GuildID, key)
	if name == "" || id == "" || i.Member == nil {
		_ = commands.Ephemeral(s, i, "That role isn't set up yet. Ask a Co-Pilot to run /setup.")
		return
	}
	opt := discordgo.WithContext(ctx)
	var err error
	msg := "🔔 You'll get **" + name + "** pings."
	if slices.Contains(i.Member.Roles, id) {
		err = s.GuildMemberRoleRemove(i.GuildID, i.Member.User.ID, id, opt)
		msg = "🔕 **" + name + "** pings are off."
	} else {
		err = s.GuildMemberRoleAdd(i.GuildID, i.Member.User.ID, id, opt)
	}
	if err != nil {
		d.Log.Error("toggle role", "role", key, "err", err)
		msg = "Hmm, that didn't work. Ping a Co-Pilot and we'll sort it."
	}
	_ = commands.Ephemeral(s, i, msg)
}

// ---- join ----

func onJoin(ctx context.Context, s *discordgo.Session, d commands.Deps, m *discordgo.GuildMemberAdd) {
	if m.User.Bot {
		return
	}
	if guest := commands.RoleID(ctx, d, m.GuildID, "guest"); guest != "" {
		if err := s.GuildMemberRoleAdd(m.GuildID, m.User.ID, guest, discordgo.WithContext(ctx)); err != nil {
			d.Log.Error("assign guest", "user", m.User.ID, "err", err)
		}
	}
	ch, _ := db.GetConfig(ctx, d.DB, m.GuildID, "welcome_channel")
	if ch == "" {
		ch = commands.ChannelID(ctx, d, m.GuildID, "general-chat")
	}
	if ch == "" {
		return
	}
	text := fmt.Sprintf("Hey <@%s>, welcome to **Star Junkies** 🚀 You're a 🛰️ Guest for now. Say hi in introductions and hang out. No pressure, ever.", m.User.ID)
	_, err := s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{
		Content:         text,
		AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{m.User.ID}},
	}, discordgo.WithContext(ctx))
	if err != nil {
		d.Log.Warn("welcome message", "err", err)
	}
}

func setWelcome(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	if !commands.IsAdmin(i) {
		_ = commands.Ephemeral(s, i, "Admins only, sorry 🔒")
		return
	}
	sub := i.ApplicationCommandData().Options[0]
	if sub.Name == "timezone" {
		name := sub.Options[0].StringValue()
		if _, err := time.LoadLocation(name); err != nil {
			_ = commands.Ephemeral(s, i, "I don't know that time zone. Try a name like America/New_York or Europe/London.")
			return
		}
		if err := db.SetConfig(ctx, d.DB, i.GuildID, "timezone", name); err != nil {
			_ = commands.Ephemeral(s, i, "Couldn't save that: "+err.Error())
			return
		}
		_ = commands.Ephemeral(s, i, "Default event time zone is now **"+name+"** 🕒")
		return
	}
	ch := sub.Options[0].ChannelValue(s)
	if err := db.SetConfig(ctx, d.DB, i.GuildID, "welcome_channel", ch.ID); err != nil {
		_ = commands.Ephemeral(s, i, "Couldn't save that: "+err.Error())
		return
	}
	_ = commands.Ephemeral(s, i, "Got it, I'll welcome new folks in <#"+ch.ID+"> 👋")
}

// ---- promote / demote ----

func move(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps, addKey, removeKey, action, verb string) {
	if !commands.IsStaff(ctx, d, i) {
		_ = commands.Ephemeral(s, i, "Only Co-Pilots and up can do that 🔒")
		return
	}
	target := i.ApplicationCommandData().Options[0].UserValue(s)
	add, remove := commands.RoleID(ctx, d, i.GuildID, addKey), commands.RoleID(ctx, d, i.GuildID, removeKey)
	if add == "" || remove == "" {
		_ = commands.Ephemeral(s, i, "Guest/Star Junkie roles aren't set up yet. Run /setup first.")
		return
	}
	opt := discordgo.WithContext(ctx)
	member, err := s.GuildMember(i.GuildID, target.ID, opt)
	if err != nil {
		_ = commands.Ephemeral(s, i, "I can't find that person in the server.")
		return
	}
	if slices.Contains(member.Roles, add) {
		_ = commands.Ephemeral(s, i, "They're already there, nothing to change 🙂")
		return
	}
	if err := s.GuildMemberRoleAdd(i.GuildID, target.ID, add, opt); err != nil {
		_ = commands.Ephemeral(s, i, "Couldn't change their role (is my role above theirs?): "+err.Error())
		return
	}
	_ = s.GuildMemberRoleRemove(i.GuildID, target.ID, remove, opt)

	if _, err := d.DB.ExecContext(ctx, `INSERT INTO mod_actions (guild_id, user_id, mod_id, action, created_at) VALUES (?, ?, ?, ?, ?)`,
		i.GuildID, target.ID, i.Member.User.ID, action, time.Now().Unix()); err != nil {
		d.Log.Error("record "+action, "err", err)
	}
	commands.PostLog(ctx, s, d, i.GuildID, &discordgo.MessageEmbed{
		Title:       strings.ToUpper(action[:1]) + action[1:],
		Description: fmt.Sprintf("<@%s> by <@%s>", target.ID, i.Member.User.ID),
		Color:       commands.ColorOrange,
		Timestamp:   time.Now().Format(time.RFC3339),
	})
	_ = commands.Public(s, i, fmt.Sprintf("<@%s> %s", target.ID, verb), target.ID)
}
