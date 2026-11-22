// Package moderation: /warn /timeout /kick /ban /modlog, server logging, and conservative anti-spam.
package moderation

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
)

// marks remembers things the bot itself just did, so gateway events caused by them aren't logged twice.
type marks struct {
	mu sync.Mutex
	m  map[string]time.Time
}

var recent = &marks{m: map[string]time.Time{}}

func (k *marks) set(kind, guild, id string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := time.Now()
	k.m[kind+"|"+guild+"|"+id] = now
	if len(k.m) > 1000 { // sweep stale marks
		for key, t := range k.m {
			if now.Sub(t) > 5*time.Minute {
				delete(k.m, key)
			}
		}
	}
}

func (k *marks) has(kind, guild, id string, within time.Duration) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	t, ok := k.m[kind+"|"+guild+"|"+id]
	return ok && time.Since(t) <= within
}

var durRe = regexp.MustCompile(`^(\d{1,4})\s*([mhdw])$`)

// parseDuration reads "10m", "2h", "1d", "1w" (1 minute to 28 days, Discord's timeout limit).
func parseDuration(s string) (time.Duration, error) {
	m := durRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return 0, fmt.Errorf("duration should look like 10m, 2h, 1d or 1w")
	}
	n, _ := strconv.Atoi(m[1])
	unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
	d := time.Duration(n) * unit
	if d < time.Minute || d > 28*24*time.Hour {
		return 0, fmt.Errorf("timeouts can be from 1 minute up to 28 days")
	}
	return d, nil
}

type modEntry struct {
	Action, Reason, ModID string
	At                    int64
}

// formatModlog renders history lines, newest first as given. Reasons are shortened to keep it readable.
func formatModlog(es []modEntry) string {
	var b strings.Builder
	for _, e := range es {
		fmt.Fprintf(&b, "<t:%d:d> **%s** by <@%s>", e.At, e.Action, e.ModID)
		if r := strings.TrimSpace(e.Reason); r != "" {
			rs := []rune(r)
			if len(rs) > 120 {
				r = string(rs[:119]) + "…"
			}
			b.WriteString(": " + r)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func record(ctx context.Context, d commands.Deps, guildID, userID, modID, action, reason string) {
	if _, err := d.DB.ExecContext(ctx, `INSERT INTO mod_actions (guild_id, user_id, mod_id, action, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		guildID, userID, modID, action, reason, time.Now().Unix()); err != nil {
		d.Log.Error("record mod action", "action", action, "err", err)
	}
}

func Register(r *commands.Registry, d commands.Deps) {
	mod := int64(discordgo.PermissionModerateMembers) // Co-Pilots have it; the runtime check is the real gate
	noDM := false
	user := func(desc string) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: desc, Required: true}
	}
	reason := &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: "reason",
		Description: "Why (kept in the mod log)", Required: true, MaxLength: 500}
	duration := &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: "duration",
		Description: "Like 10m, 2h, 1d (max 28d)", Required: true, MaxLength: 6}

	add := func(name, desc string, opts ...*discordgo.ApplicationCommandOption) {
		r.Add(commands.Command{
			Def: &discordgo.ApplicationCommand{Name: name, Description: desc, Options: opts, DefaultMemberPermissions: &mod, DMPermission: &noDM},
			Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
				if name == "modlog" {
					showModlog(ctx, s, i, d)
					return
				}
				act(ctx, s, i, d, name)
			},
		})
	}
	add("warn", "Give someone a warning", user("Who to warn"), reason)
	add("timeout", "Time someone out", user("Who to time out"), duration, reason)
	add("kick", "Kick someone", user("Who to kick"), reason)
	add("ban", "Ban someone", user("Who to ban"), reason)
	add("modlog", "See someone's moderation history", user("Whose history"))

	registerAntispam(r, d)
}

func isStaffMember(ctx context.Context, d commands.Deps, guildID string, m *discordgo.Member) bool {
	for _, key := range []string{"copilot", "founder"} {
		if id := commands.RoleID(ctx, d, guildID, key); id != "" {
			for _, r := range m.Roles {
				if r == id {
					return true
				}
			}
		}
	}
	return false
}

var dmText = map[string]string{
	"warn":    "⚠️ A Co-Pilot gave you a heads-up in Star Junkies: %s\nNo big deal, just keep it chill. 💛",
	"timeout": "⏳ You've been timed out in Star Junkies for %s: %s\nTake a breather, you can jump back in after.",
	"kick":    "👢 You were removed from Star Junkies: %s",
	"ban":     "🚫 You were banned from Star Junkies: %s",
}

func act(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps, action string) {
	if !commands.IsStaff(ctx, d, i) {
		_ = commands.Ephemeral(s, i, "Only Co-Pilots and up can do that 🔒")
		return
	}
	o := commands.OptMap(i.ApplicationCommandData().Options)
	target := o["user"].UserValue(s)
	reason := strings.TrimSpace(o["reason"].StringValue())
	opt := discordgo.WithContext(ctx)

	if target.ID == i.Member.User.ID || target.ID == s.State.User.ID {
		_ = commands.Ephemeral(s, i, "Nice try 🙂 Pick someone else.")
		return
	}
	member, _ := s.GuildMember(i.GuildID, target.ID, opt)
	if member != nil && isStaffMember(ctx, d, i.GuildID, member) {
		_ = commands.Ephemeral(s, i, "That's a Co-Pilot or Founder. Take it to the Founder instead.")
		return
	}
	if member == nil && action != "warn" && action != "ban" {
		_ = commands.Ephemeral(s, i, "They're not in the server.")
		return
	}

	var dur time.Duration
	if action == "timeout" {
		var err error
		if dur, err = parseDuration(o["duration"].StringValue()); err != nil {
			_ = commands.Ephemeral(s, i, err.Error())
			return
		}
	}
	var text string
	if action == "timeout" {
		text = fmt.Sprintf(dmText[action], dur, reason)
	} else {
		text = fmt.Sprintf(dmText[action], reason)
	}

	var err, dmErr error
	switch action {
	case "warn":
		dmErr = commands.SendDM(s, target.ID, text)
	case "timeout":
		until := time.Now().Add(dur)
		if err = s.GuildMemberTimeout(i.GuildID, target.ID, &until, opt); err == nil {
			dmErr = commands.SendDM(s, target.ID, text)
		}
	case "kick": // DM first: afterwards they share no server with us
		dmErr = commands.SendDM(s, target.ID, text)
		recent.set("kick", i.GuildID, target.ID)
		err = s.GuildMemberDeleteWithReason(i.GuildID, target.ID, reason, opt)
	case "ban":
		dmErr = commands.SendDM(s, target.ID, text)
		recent.set("ban", i.GuildID, target.ID)
		err = s.GuildBanCreateWithReason(i.GuildID, target.ID, reason, 0, opt)
	}
	if err != nil {
		_ = commands.Ephemeral(s, i, fmt.Sprintf("Couldn't %s them (is my role above theirs?): %s", action, err))
		return
	}

	stored := reason
	title := strings.ToUpper(action[:1]) + action[1:]
	if action == "timeout" {
		stored = fmt.Sprintf("%s (%s)", reason, dur)
		title += " (" + dur.String() + ")"
	}
	record(ctx, d, i.GuildID, target.ID, i.Member.User.ID, action, stored)
	commands.PostLog(ctx, s, d, i.GuildID, &discordgo.MessageEmbed{
		Title: title, Color: commands.ColorOrange, Timestamp: time.Now().Format(time.RFC3339),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "User", Value: fmt.Sprintf("<@%s> (%s)", target.ID, target.Username), Inline: true},
			{Name: "Moderator", Value: "<@" + i.Member.User.ID + ">", Inline: true},
			{Name: "Reason", Value: reason},
		},
	})
	msg := fmt.Sprintf("✅ %s <@%s>.", map[string]string{"warn": "Warned", "timeout": "Timed out", "kick": "Kicked", "ban": "Banned"}[action], target.ID)
	if dmErr != nil {
		msg += " (Their DMs are closed, so they didn't get a message.)"
	}
	_ = commands.Ephemeral(s, i, msg)
}

func showModlog(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	if !commands.IsStaff(ctx, d, i) {
		_ = commands.Ephemeral(s, i, "Only Co-Pilots and up can do that 🔒")
		return
	}
	target := commands.OptMap(i.ApplicationCommandData().Options)["user"].UserValue(s)
	rows, err := d.DB.QueryContext(ctx, `SELECT action, reason, mod_id, created_at FROM mod_actions WHERE guild_id = ? AND user_id = ? ORDER BY id DESC LIMIT 10`,
		i.GuildID, target.ID)
	if err != nil {
		_ = commands.Ephemeral(s, i, "Couldn't load that right now.")
		return
	}
	defer rows.Close()
	var es []modEntry
	for rows.Next() {
		var e modEntry
		if rows.Scan(&e.Action, &e.Reason, &e.ModID, &e.At) == nil {
			es = append(es, e)
		}
	}
	if len(es) == 0 {
		_ = commands.Ephemeral(s, i, fmt.Sprintf("<@%s> has a clean record ✨", target.ID))
		return
	}
	_ = commands.Ephemeral(s, i, fmt.Sprintf("**Mod log for <@%s>** (latest %d)\n%s", target.ID, len(es), formatModlog(es)))
}
