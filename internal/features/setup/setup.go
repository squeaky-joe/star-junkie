// Package setup implements /setup: build the whole server layout, idempotently.
package setup

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
	"github.com/starjunkies/bot/internal/db"
)

func Register(r *commands.Registry, d commands.Deps) {
	perm := int64(discordgo.PermissionAdministrator)
	noDM := false
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name:                     "setup",
			Description:              "Build the Star Junkies server layout (admin only, safe to re-run)",
			DefaultMemberPermissions: &perm,
			DMPermission:             &noDM,
			Options: []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "dry-run",
				Description: "Just report what would change",
			}},
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			handle(ctx, s, i, d)
		},
	})
}

func handle(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	if !commands.IsAdmin(i) {
		_ = commands.Ephemeral(s, i, "Sorry, /setup is for admins only 🔒")
		return
	}
	dry := false
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "dry-run" {
			dry = o.BoolValue()
		}
	}
	if err := commands.DeferEphemeral(s, i); err != nil {
		d.Log.Error("setup: defer", "err", err)
		return
	}

	rep, err := run(ctx, s, d, i.GuildID, dry)
	msg := rep.String(dry)
	if err != nil {
		d.Log.Error("setup failed", "guild", i.GuildID, "err", err)
		msg += "\n\n⚠️ Stopped early: " + err.Error() + "\nFix that and run /setup again, it'll pick up where it left off."
	}
	if len(msg) > 1900 {
		msg = msg[:1900] + "…"
	}
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg})
}

type report struct {
	created, warnings []string
	skipped           int
}

func (r *report) String(dry bool) string {
	var b strings.Builder
	head := "Created"
	if dry {
		head = "Would create"
	}
	if len(r.created) == 0 {
		b.WriteString("✅ Nothing to do, everything's already in place.")
	} else {
		fmt.Fprintf(&b, "**%s (%d):**\n", head, len(r.created))
		b.WriteString(strings.Join(r.created, ", "))
	}
	if r.skipped > 0 {
		fmt.Fprintf(&b, "\n\n⏭️ Already there: %d", r.skipped)
	}
	for _, w := range r.warnings {
		b.WriteString("\n⚠️ " + w)
	}
	if dry {
		b.WriteString("\n\n(Dry run, nothing changed.)")
	}
	return b.String()
}

func lower(s string) string { return strings.ToLower(s) }

// run walks the layout and creates whatever is missing. In dry mode nothing is written.
func run(ctx context.Context, s *discordgo.Session, d commands.Deps, guildID string, dry bool) (*report, error) {
	rep := &report{}
	opt := discordgo.WithContext(ctx)
	botID := s.State.User.ID

	guild, err := s.Guild(guildID, opt)
	if err != nil {
		return rep, fmt.Errorf("fetch server: %w", err)
	}
	existingRoles, err := s.GuildRoles(guildID, opt)
	if err != nil {
		return rep, fmt.Errorf("fetch roles: %w", err)
	}
	existingChans, err := s.GuildChannels(guildID, opt)
	if err != nil {
		return rep, fmt.Errorf("fetch channels: %w", err)
	}

	// ---- roles ----
	roleID := map[string]string{} // lowercase name -> id
	for _, r := range existingRoles {
		roleID[lower(r.Name)] = r.ID
	}
	botTop := 0 // highest position among the bot's own roles
	if me, err := s.GuildMember(guildID, botID, opt); err == nil {
		for _, rid := range me.Roles {
			for _, r := range existingRoles {
				if r.ID == rid && r.Position > botTop {
					botTop = r.Position
				}
			}
		}
	}

	var newRoles []*discordgo.Role // created this run, highest first, for reordering
	ensureRole := func(p discordgo.RoleParams, cfgKey string) error {
		id, ok := roleID[lower(p.Name)]
		if ok {
			rep.skipped++
		} else {
			rep.created = append(rep.created, "role "+p.Name)
			if dry {
				return nil
			}
			r, err := s.GuildRoleCreate(guildID, &p, opt)
			if err != nil {
				return fmt.Errorf("create role %s: %w", p.Name, err)
			}
			id = r.ID
			roleID[lower(p.Name)] = id
			newRoles = append(newRoles, &discordgo.Role{ID: id})
			d.Log.Info("setup: created role", "name", p.Name)
		}
		if dry {
			return nil
		}
		return db.SetConfig(ctx, d.DB, guildID, "role."+cfgKey, id)
	}
	for _, rd := range Roles {
		color, perms, hoist, ment := rd.Color, rd.Perms, rd.Hoist, rd.Mentionable
		if err := ensureRole(discordgo.RoleParams{Name: rd.Name, Color: &color, Permissions: &perms, Hoist: &hoist, Mentionable: &ment}, rd.Key); err != nil {
			return rep, err
		}
	}
	for _, name := range PingRoles {
		yes := true
		if err := ensureRole(discordgo.RoleParams{Name: name, Mentionable: &yes}, strings.ReplaceAll(lower(name), " ", "-")); err != nil {
			return rep, err
		}
	}

	// New roles land at the bottom. Put them in layout order (Founder on top), just under the bot's role.
	if len(newRoles) > 0 {
		if botTop-1 < len(newRoles) {
			rep.warnings = append(rep.warnings, "Bot's role is too low to order the new roles. Drag the bot's role to the top in Server Settings → Roles, then re-run.")
		} else {
			for n, r := range newRoles {
				r.Position = botTop - 1 - n
			}
			if _, err := s.GuildRoleReorder(guildID, newRoles, opt); err != nil {
				rep.warnings = append(rep.warnings, "Couldn't reorder roles: "+err.Error())
			}
		}
	}
	// Give the bot the Bots role (best effort; it also gets per-channel overwrites).
	if id := roleID[lower(Roles[2].Name)]; id != "" && !dry {
		_ = s.GuildMemberRoleAdd(guildID, botID, id, opt)
	}

	ids := RoleIDs{
		Everyone: guildID,
		Founder:  roleID[lower(Roles[0].Name)],
		CoPilot:  roleID[lower(Roles[1].Name)],
		Bots:     roleID[lower(Roles[2].Name)],
		Junkie:   roleID[lower(Roles[3].Name)],
		Guest:    roleID[lower(Roles[4].Name)],
		BotUser:  botID,
	}

	// ---- categories and channels ----
	community := false
	for _, f := range guild.Features {
		if f == discordgo.GuildFeatureCommunity {
			community = true
		}
	}
	// existing channels keyed "parentID|lowername" ("" parent for categories)
	chanID := map[string]string{}
	for _, c := range existingChans {
		chanID[c.ParentID+"|"+lower(c.Name)] = c.ID
	}
	mention := map[string]string{} // channel name -> "<#id>" for post placeholders
	type pending struct {
		id string
		p  *Post
	}
	var posts []pending
	afkID, afkMade := "", false

	for _, cat := range Categories {
		catID, ok := chanID["|"+lower(cat.Name)]
		if ok {
			rep.skipped++
		} else {
			rep.created = append(rep.created, "category "+cat.Name)
			catID = "dry:" + cat.Name
			if !dry {
				c, err := s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
					Name: cat.Name, Type: discordgo.ChannelTypeGuildCategory,
					PermissionOverwrites: Overwrites(ids, cat.Access, false),
				}, opt)
				if err != nil {
					return rep, fmt.Errorf("create category %s: %w", cat.Name, err)
				}
				catID = c.ID
				d.Log.Info("setup: created category", "name", cat.Name)
			}
		}

		for _, ch := range cat.Channels {
			key := catID + "|" + lower(ch.Name)
			if id, ok := chanID[key]; ok {
				rep.skipped++
				mention[ch.Name] = "<#" + id + ">"
				if ch.AFK {
					afkID = id
				}
				if !dry {
					if err := db.SetConfig(ctx, d.DB, guildID, "channel."+ch.Name, id); err != nil {
						return rep, err
					}
				}
				continue
			}
			if ch.Kind == Stage && !community {
				rep.warnings = append(rep.warnings, "Skipped stage channel "+ch.Name+" (turn on Community in Server Settings, then re-run).")
				continue
			}
			rep.created = append(rep.created, "#"+ch.Name)
			if dry {
				continue
			}
			data := discordgo.GuildChannelCreateData{Name: ch.Name, Topic: ch.Topic, ParentID: catID}
			switch ch.Kind {
			case Voice:
				data.Type = discordgo.ChannelTypeGuildVoice
			case Stage:
				data.Type = discordgo.ChannelTypeGuildStageVoice
			default:
				data.Type = discordgo.ChannelTypeGuildText
			}
			data.PermissionOverwrites = Overwrites(ids, cat.Access, ch.ReadOnly && !ch.Open)
			c, err := s.GuildChannelCreateComplex(guildID, data, opt)
			if err != nil {
				return rep, fmt.Errorf("create #%s: %w", ch.Name, err)
			}
			mention[ch.Name] = "<#" + c.ID + ">"
			d.Log.Info("setup: created channel", "name", ch.Name)
			if err := db.SetConfig(ctx, d.DB, guildID, "channel."+ch.Name, c.ID); err != nil {
				return rep, err
			}
			if ch.Post != nil {
				posts = append(posts, pending{c.ID, ch.Post})
			}
			if ch.AFK {
				afkID, afkMade = c.ID, true
			}
		}
	}

	// ---- AFK channel (don't override one the admin already chose) ----
	if afkID != "" && (afkMade || guild.AfkChannelID == "") && !dry {
		if _, err := s.GuildEdit(guildID, &discordgo.GuildParams{AfkChannelID: afkID, AfkTimeout: afkSeconds}, opt); err != nil {
			rep.warnings = append(rep.warnings, "Couldn't set the AFK channel: "+err.Error())
		}
	}

	// ---- welcome + rules (only in channels created this run) ----
	pairs := make([]string, 0, len(mention)*2)
	for name, m := range mention {
		pairs = append(pairs, "#{"+name+"}", m)
	}
	rpl := strings.NewReplacer(pairs...)
	for _, p := range posts {
		_, err := s.ChannelMessageSendComplex(p.id, &discordgo.MessageSend{
			Embeds:          []*discordgo.MessageEmbed{{Title: p.p.Title, Description: rpl.Replace(p.p.Body), Color: embedColor}},
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}, opt)
		if err != nil {
			rep.warnings = append(rep.warnings, "Couldn't post a message: "+err.Error())
		}
	}
	return rep, nil
}
