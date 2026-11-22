package moderation

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
	"github.com/starjunkies/bot/internal/features/setup"
)

// AddHandlers hooks gateway events: server logging and anti-spam.
// Needs the Server Members, Message Content and Guild Bans intents, and State.MaxMessageCount > 0
// (otherwise deleted/edited messages can't show their old text).
func AddHandlers(ctx context.Context, s *discordgo.Session, d commands.Deps) {
	// discordgo runs each handler in its own goroutine, so waiting inside one is fine.
	with := func(f func(context.Context)) {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		f(c)
	}
	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		with(func(c context.Context) { onJoin(c, s, d, m) })
	})
	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
		with(func(c context.Context) { onLeave(c, s, d, m) })
	})
	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildBanAdd) {
		with(func(c context.Context) { onBan(c, s, d, m) })
	})
	s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageDelete) {
		with(func(c context.Context) { onDelete(c, s, d, m) })
	})
	s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
		with(func(c context.Context) { onEdit(c, s, d, m) })
	})
	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
		with(func(c context.Context) { onRoles(c, s, d, m) })
	})
	s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		with(func(c context.Context) { onMessage(c, s, d, m) })
	})
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func logEmbed(title string, color int, desc string, fields ...*discordgo.MessageEmbedField) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: title, Color: color, Description: desc, Fields: fields, Timestamp: time.Now().Format(time.RFC3339)}
}

func field(name, value string, inline bool) *discordgo.MessageEmbedField {
	if strings.TrimSpace(value) == "" {
		value = "—"
	}
	return &discordgo.MessageEmbedField{Name: name, Value: trunc(value, 1000), Inline: inline}
}

func who(u *discordgo.User) string {
	if u == nil {
		return "unknown"
	}
	return fmt.Sprintf("<@%s> (%s)", u.ID, u.Username)
}

// roleDiff returns roles gained and lost between two role lists.
func roleDiff(before, after []string) (added, removed []string) {
	for _, r := range after {
		if !slices.Contains(before, r) {
			added = append(added, r)
		}
	}
	for _, r := range before {
		if !slices.Contains(after, r) {
			removed = append(removed, r)
		}
	}
	return
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// auditFind finds a very recent audit-log entry of the given type aimed at userID.
func auditFind(ctx context.Context, s *discordgo.Session, guildID, userID string, action discordgo.AuditLogAction) *discordgo.AuditLogEntry {
	al, err := s.GuildAuditLog(guildID, "", "", int(action), 10, discordgo.WithContext(ctx))
	if err != nil {
		return nil // no View Audit Log permission, or Discord hiccup
	}
	for _, e := range al.AuditLogEntries {
		if e.TargetID != userID {
			continue
		}
		if t, err := discordgo.SnowflakeTimestamp(e.ID); err == nil && time.Since(t) < 30*time.Second {
			return e
		}
	}
	return nil
}

func onJoin(ctx context.Context, s *discordgo.Session, d commands.Deps, m *discordgo.GuildMemberAdd) {
	created := ""
	if t, err := discordgo.SnowflakeTimestamp(m.User.ID); err == nil {
		created = fmt.Sprintf("<t:%d:R>", t.Unix())
	}
	commands.PostLog(ctx, s, d, m.GuildID, logEmbed("Member joined", commands.ColorTeal, who(m.User), field("Account created", created, false)))
}

func onLeave(ctx context.Context, s *discordgo.Session, d commands.Deps, m *discordgo.GuildMemberRemove) {
	if m.User == nil || !sleep(ctx, 2*time.Second) { // audit entries land a moment after the event
		return
	}
	if recent.has("kick", m.GuildID, m.User.ID, time.Minute) || recent.has("ban", m.GuildID, m.User.ID, time.Minute) {
		return // our own command already logged it
	}
	if auditFind(ctx, s, m.GuildID, m.User.ID, discordgo.AuditLogActionMemberBanAdd) != nil {
		return // onBan logs it
	}
	if e := auditFind(ctx, s, m.GuildID, m.User.ID, discordgo.AuditLogActionMemberKick); e != nil {
		record(ctx, d, m.GuildID, m.User.ID, e.UserID, "kick", e.Reason)
		commands.PostLog(ctx, s, d, m.GuildID, logEmbed("Kick", commands.ColorOrange, who(m.User),
			field("Moderator", "<@"+e.UserID+">", true), field("Reason", e.Reason, false)))
		return
	}
	commands.PostLog(ctx, s, d, m.GuildID, logEmbed("Member left", commands.ColorIndigo, who(m.User)))
}

func onBan(ctx context.Context, s *discordgo.Session, d commands.Deps, m *discordgo.GuildBanAdd) {
	if recent.has("ban", m.GuildID, m.User.ID, time.Minute) || !sleep(ctx, time.Second) {
		return
	}
	mod, reason := "unknown", ""
	if e := auditFind(ctx, s, m.GuildID, m.User.ID, discordgo.AuditLogActionMemberBanAdd); e != nil {
		mod, reason = e.UserID, e.Reason
		record(ctx, d, m.GuildID, m.User.ID, e.UserID, "ban", e.Reason)
		mod = "<@" + mod + ">"
	}
	commands.PostLog(ctx, s, d, m.GuildID, logEmbed("Ban", commands.ColorOrange, who(m.User),
		field("Moderator", mod, true), field("Reason", reason, false)))
}

func onDelete(ctx context.Context, s *discordgo.Session, d commands.Deps, m *discordgo.MessageDelete) {
	if m.GuildID == "" || recent.has("spamdel", m.GuildID, m.ID, time.Minute) {
		return // DMs, or the anti-spam already logged this one
	}
	if m.ChannelID == commands.ChannelID(ctx, d, m.GuildID, "server-logs") {
		return
	}
	b := m.BeforeDelete
	if b == nil {
		commands.PostLog(ctx, s, d, m.GuildID, logEmbed("Message deleted", commands.ColorIndigo,
			"A message in <#"+m.ChannelID+"> was deleted (it was too old for me to remember what it said)."))
		return
	}
	if b.Author != nil && b.Author.Bot {
		return
	}
	files := make([]string, 0, len(b.Attachments))
	for _, a := range b.Attachments {
		files = append(files, a.Filename)
	}
	commands.PostLog(ctx, s, d, m.GuildID, logEmbed("Message deleted", commands.ColorIndigo, "",
		field("Author", who(b.Author), true), field("Channel", "<#"+m.ChannelID+">", true),
		field("Content", b.Content, false), field("Attachments", strings.Join(files, ", "), false)))
}

func onEdit(ctx context.Context, s *discordgo.Session, d commands.Deps, m *discordgo.MessageUpdate) {
	b := m.BeforeUpdate
	if m.GuildID == "" || b == nil || m.Message == nil || b.Content == m.Content {
		return // DM, not cached, or just an embed refresh
	}
	if b.Author != nil && b.Author.Bot {
		return
	}
	link := fmt.Sprintf("[Jump to message](https://discord.com/channels/%s/%s/%s)", m.GuildID, m.ChannelID, m.ID)
	commands.PostLog(ctx, s, d, m.GuildID, logEmbed("Message edited", commands.ColorTeal, link,
		field("Author", who(b.Author), true), field("Channel", "<#"+m.ChannelID+">", true),
		field("Before", b.Content, false), field("After", m.Content, false)))
}

func onRoles(ctx context.Context, s *discordgo.Session, d commands.Deps, m *discordgo.GuildMemberUpdate) {
	if m.BeforeUpdate == nil || m.Member == nil {
		return
	}
	added, removed := roleDiff(m.BeforeUpdate.Roles, m.Roles)
	// Self-serve ping-role toggles would drown the log, so skip those.
	skip := map[string]bool{}
	for _, name := range setup.PingRoles {
		if id := commands.RoleID(ctx, d, m.GuildID, setup.PingKey(name)); id != "" {
			skip[id] = true
		}
	}
	mention := func(ids []string) string {
		var out []string
		for _, id := range ids {
			if !skip[id] {
				out = append(out, "<@&"+id+">")
			}
		}
		return strings.Join(out, " ")
	}
	a, r := mention(added), mention(removed)
	if a == "" && r == "" {
		return
	}
	var fields []*discordgo.MessageEmbedField
	if a != "" {
		fields = append(fields, field("Added", a, true))
	}
	if r != "" {
		fields = append(fields, field("Removed", r, true))
	}
	commands.PostLog(ctx, s, d, m.GuildID, logEmbed("Roles changed", commands.ColorTeal, who(m.User), fields...))
}
