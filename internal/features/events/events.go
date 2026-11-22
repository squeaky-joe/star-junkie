// Package events: /event create|list|edit|cancel, RSVP buttons, reminders, optional native events.
package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
	"github.com/starjunkies/bot/internal/db"
)

type event struct {
	ID                                                        int64
	GuildID, Title, Desc                                      string
	Start                                                     time.Time
	PingRole, ChannelID, MessageID, NativeID, Creator, Status string
}

const eventCols = `id, guild_id, title, description, start_unix, ping_role_id, channel_id, message_id, native_id, creator_id, status`

type scanner interface{ Scan(...any) error }

func scanEvent(sc scanner) (event, error) {
	var e event
	var start int64
	err := sc.Scan(&e.ID, &e.GuildID, &e.Title, &e.Desc, &start, &e.PingRole, &e.ChannelID, &e.MessageID, &e.NativeID, &e.Creator, &e.Status)
	e.Start = time.Unix(start, 0)
	return e, err
}

func loadEvent(ctx context.Context, d commands.Deps, guildID string, id int64) (event, error) {
	return scanEvent(d.DB.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE id = ? AND guild_id = ?`, id, guildID))
}

// rsvps returns user IDs grouped by status ("going", "maybe", "no"), oldest answer first.
func rsvps(ctx context.Context, d commands.Deps, id int64) map[string][]string {
	out := map[string][]string{}
	rows, err := d.DB.QueryContext(ctx, `SELECT user_id, status FROM event_rsvps WHERE event_id = ? ORDER BY updated_at, rowid`, id)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var u, st string
		if rows.Scan(&u, &st) == nil {
			out[st] = append(out[st], u)
		}
	}
	return out
}

func renderEmbed(ev event, r map[string][]string) *discordgo.MessageEmbed {
	desc := strings.TrimSpace(ev.Desc)
	if desc != "" {
		desc += "\n\n"
	}
	e := &discordgo.MessageEmbed{
		Title:       ev.Title,
		Description: desc + "🗓️ " + timeTags(ev.Start),
		Color:       commands.ColorTeal,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Event #" + strconv.FormatInt(ev.ID, 10)},
	}
	if ev.Status == "cancelled" {
		e.Title = "❌ Cancelled: " + ev.Title
		e.Color = commands.ColorIndigo
		return e
	}
	field := func(name string, ids []string) *discordgo.MessageEmbedField {
		return &discordgo.MessageEmbedField{Name: fmt.Sprintf("%s (%d)", name, len(ids)), Value: mentionList(ids, 25), Inline: true}
	}
	e.Fields = []*discordgo.MessageEmbedField{field("✅ Going", r["going"]), field("🤔 Maybe", r["maybe"]), field("❌ Can't", r["no"])}
	return e
}

func rsvpButtons(id int64) []discordgo.MessageComponent {
	sid := strconv.FormatInt(id, 10)
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Going", Style: discordgo.SuccessButton, CustomID: "ev:going:" + sid},
		discordgo.Button{Label: "Maybe", Style: discordgo.SecondaryButton, CustomID: "ev:maybe:" + sid},
		discordgo.Button{Label: "Can't", Style: discordgo.DangerButton, CustomID: "ev:no:" + sid},
	}}}
}

func Register(r *commands.Registry, d commands.Deps) {
	noDM := false
	strOpt := func(name, desc string, req bool, max int) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: name, Description: desc, Required: req, MaxLength: max}
	}
	idOpt := &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionInteger, Name: "id", Description: "Event number (see /event list)", Required: true}
	sub := func(name, desc string, opts ...*discordgo.ApplicationCommandOption) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionSubCommand, Name: name, Description: desc, Options: opts}
	}
	date := "Date, like 2026-12-05"
	clock := "Time, like 20:00 or 8pm"
	tz := "Time zone, like America/New_York (default: server setting)"

	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "event", Description: "Plan and RSVP to org events", DMPermission: &noDM,
			Options: []*discordgo.ApplicationCommandOption{
				sub("create", "Create an event",
					strOpt("title", "Event name", true, 100), strOpt("date", date, true, 10), strOpt("time", clock, true, 10),
					strOpt("description", "What's the plan?", false, 1000), strOpt("timezone", tz, false, 64),
					&discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionRole, Name: "ping-role", Description: "Role to ping (ping roles only)"},
					&discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionBoolean, Name: "native", Description: "Also create a Discord Scheduled Event"},
				),
				sub("list", "Upcoming events"),
				sub("edit", "Change an event", idOpt,
					strOpt("title", "New name", false, 100), strOpt("description", "New description", false, 1000),
					strOpt("date", date, false, 10), strOpt("time", clock, false, 10), strOpt("timezone", tz, false, 64)),
				sub("cancel", "Cancel an event", idOpt),
			},
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			sc := i.ApplicationCommandData().Options[0]
			switch sc.Name {
			case "create":
				create(ctx, s, i, d, sc)
			case "list":
				list(ctx, s, i, d)
			case "edit":
				edit(ctx, s, i, d, sc)
			case "cancel":
				cancel(ctx, s, i, d, sc)
			}
		},
	})
	r.Component("ev", func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
		rsvp(ctx, s, i, d)
	})
}

func optMap(opts []*discordgo.ApplicationCommandInteractionDataOption) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	m := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
	for _, o := range opts {
		m[o.Name] = o
	}
	return m
}

func str(m map[string]*discordgo.ApplicationCommandInteractionDataOption, k string) string {
	if o := m[k]; o != nil {
		return o.StringValue()
	}
	return ""
}

// locFor resolves an explicit zone name, else the server default (/config timezone), else UTC.
func locFor(ctx context.Context, d commands.Deps, guildID, override string) (*time.Location, error) {
	name := strings.TrimSpace(override)
	if name == "" {
		name, _ = db.GetConfig(ctx, d.DB, guildID, "timezone")
	}
	if name == "" {
		name = "UTC"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("I don't know the time zone %q. Try something like America/New_York or Europe/London", name)
	}
	return loc, nil
}

func canCreate(ctx context.Context, d commands.Deps, i *discordgo.InteractionCreate) bool {
	if commands.IsStaff(ctx, d, i) {
		return true
	}
	j := commands.RoleID(ctx, d, i.GuildID, "junkie")
	return i.Member != nil && j != "" && slices.Contains(i.Member.Roles, j)
}

func canManage(ctx context.Context, d commands.Deps, i *discordgo.InteractionCreate, ev event) bool {
	return commands.IsStaff(ctx, d, i) || (i.Member != nil && i.Member.User.ID == ev.Creator)
}

func nativeParams(ev event) *discordgo.GuildScheduledEventParams {
	start, end := ev.Start, ev.Start.Add(2*time.Hour)
	return &discordgo.GuildScheduledEventParams{
		Name: truncate(ev.Title, 100), Description: truncate(ev.Desc, 1000),
		ScheduledStartTime: &start, ScheduledEndTime: &end,
		PrivacyLevel:   discordgo.GuildScheduledEventPrivacyLevelGuildOnly,
		EntityType:     discordgo.GuildScheduledEventEntityTypeExternal,
		EntityMetadata: &discordgo.GuildScheduledEventEntityMetadata{Location: "Star Junkies voice"},
	}
}

func finish(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg})
}

func create(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps, sc *discordgo.ApplicationCommandInteractionDataOption) {
	if !canCreate(ctx, d, i) {
		_ = commands.Ephemeral(s, i, "Events are for Star Junkies and staff. Apply with /apply to join the crew 🚀")
		return
	}
	o := optMap(sc.Options)
	loc, err := locFor(ctx, d, i.GuildID, str(o, "timezone"))
	if err != nil {
		_ = commands.Ephemeral(s, i, err.Error())
		return
	}
	start, err := parseWhen(str(o, "date"), str(o, "time"), loc, time.Time{})
	if err != nil {
		_ = commands.Ephemeral(s, i, err.Error())
		return
	}
	now := time.Now()
	if !start.After(now) {
		_ = commands.Ephemeral(s, i, "That time's already passed. Pick something in the future 🙂")
		return
	}
	pingRole := ""
	if ro := o["ping-role"]; ro != nil {
		role := ro.RoleValue(s, i.GuildID)
		if role == nil || role.ID == i.GuildID || !role.Mentionable {
			_ = commands.Ephemeral(s, i, "I can only ping roles marked mentionable (like the ping roles in #get-roles), never @everyone.")
			return
		}
		pingRole = role.ID
	}
	ch := commands.ChannelID(ctx, d, i.GuildID, "event-calendar")
	if ch == "" {
		_ = commands.Ephemeral(s, i, "I can't find #event-calendar. Run /setup first.")
		return
	}
	if err := commands.DeferEphemeral(s, i); err != nil {
		return
	}

	ev := event{GuildID: i.GuildID, Title: str(o, "title"), Desc: str(o, "description"), Start: start, PingRole: pingRole, ChannelID: ch, Creator: i.Member.User.ID, Status: "active"}
	sent1h := 0
	if start.Sub(now) <= time.Hour { // already inside the hour, skip the "1 hour" reminder
		sent1h = 1
	}
	res, err := d.DB.ExecContext(ctx, `INSERT INTO events (guild_id, title, description, start_unix, ping_role_id, channel_id, creator_id, reminded_1h, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, ev.GuildID, ev.Title, ev.Desc, start.Unix(), pingRole, ch, ev.Creator, sent1h, now.Unix())
	if err != nil {
		d.Log.Error("insert event", "err", err)
		finish(s, i, "Couldn't save that event, try again in a bit.")
		return
	}
	ev.ID, _ = res.LastInsertId()
	opt := discordgo.WithContext(ctx)

	note := ""
	if nv := o["native"]; nv != nil && nv.BoolValue() {
		if ne, err := s.GuildScheduledEventCreate(i.GuildID, nativeParams(ev), opt); err != nil {
			note = "\n(Couldn't create the Discord Scheduled Event. The bot needs Manage Events.)"
			d.Log.Warn("native event", "err", err)
		} else {
			ev.NativeID = ne.ID
		}
	}

	send := &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{renderEmbed(ev, nil)},
		Components:      rsvpButtons(ev.ID),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
	if pingRole != "" {
		send.Content = "<@&" + pingRole + ">"
		send.AllowedMentions = &discordgo.MessageAllowedMentions{Roles: []string{pingRole}}
	}
	msg, err := s.ChannelMessageSendComplex(ch, send, opt)
	if err != nil {
		d.Log.Error("post event", "err", err)
		_, _ = d.DB.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, ev.ID)
		if ev.NativeID != "" {
			_ = s.GuildScheduledEventDelete(i.GuildID, ev.NativeID, opt)
		}
		finish(s, i, "I couldn't post in #event-calendar. Check my permissions there.")
		return
	}
	_, _ = d.DB.ExecContext(ctx, `UPDATE events SET message_id = ?, native_id = ? WHERE id = ?`, msg.ID, ev.NativeID, ev.ID)
	finish(s, i, fmt.Sprintf("Event #%d is up in <#%s> 🗓️%s", ev.ID, ch, note))
}

func list(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	rows, err := d.DB.QueryContext(ctx, `SELECT `+eventCols+` FROM events WHERE guild_id = ? AND status = 'active' AND start_unix > ? ORDER BY start_unix LIMIT 10`,
		i.GuildID, time.Now().Unix())
	if err != nil {
		_ = commands.Ephemeral(s, i, "Couldn't load events right now.")
		return
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		if ev, err := scanEvent(rows); err == nil {
			fmt.Fprintf(&b, "**#%d** %s: %s\n", ev.ID, ev.Title, timeTags(ev.Start))
		}
	}
	if b.Len() == 0 {
		_ = commands.Ephemeral(s, i, "Nothing planned yet. Make something with /event create 🚀")
		return
	}
	_ = commands.Ephemeral(s, i, "**Upcoming events**\n"+b.String())
}

// syncMessage re-renders the calendar message (and keeps or drops the buttons).
func syncMessage(ctx context.Context, s *discordgo.Session, d commands.Deps, ev event) {
	if ev.MessageID == "" {
		return
	}
	embeds := []*discordgo.MessageEmbed{renderEmbed(ev, rsvps(ctx, d, ev.ID))}
	comps := rsvpButtons(ev.ID)
	if ev.Status != "active" {
		comps = []discordgo.MessageComponent{}
	}
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: ev.MessageID, Channel: ev.ChannelID, Embeds: &embeds, Components: &comps}, discordgo.WithContext(ctx)); err != nil {
		d.Log.Warn("sync event message", "event", ev.ID, "err", err)
	}
}

func lookup(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps, sc *discordgo.ApplicationCommandInteractionDataOption) (event, bool) {
	id := optMap(sc.Options)["id"].IntValue()
	ev, err := loadEvent(ctx, d, i.GuildID, id)
	if errors.Is(err, sql.ErrNoRows) {
		_ = commands.Ephemeral(s, i, fmt.Sprintf("I can't find event #%d. /event list shows what's coming up.", id))
		return ev, false
	}
	if err != nil {
		_ = commands.Ephemeral(s, i, "Couldn't load that event right now.")
		return ev, false
	}
	if !canManage(ctx, d, i, ev) {
		_ = commands.Ephemeral(s, i, "Only the host or a Co-Pilot can change that event 🔒")
		return ev, false
	}
	if ev.Status != "active" {
		_ = commands.Ephemeral(s, i, "That event's already cancelled.")
		return ev, false
	}
	return ev, true
}

func cancel(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps, sc *discordgo.ApplicationCommandInteractionDataOption) {
	ev, ok := lookup(ctx, s, i, d, sc)
	if !ok {
		return
	}
	if _, err := d.DB.ExecContext(ctx, `UPDATE events SET status = 'cancelled' WHERE id = ?`, ev.ID); err != nil {
		_ = commands.Ephemeral(s, i, "Couldn't cancel that, try again.")
		return
	}
	ev.Status = "cancelled"
	syncMessage(ctx, s, d, ev)
	if ev.NativeID != "" {
		_ = s.GuildScheduledEventDelete(i.GuildID, ev.NativeID, discordgo.WithContext(ctx))
	}
	_ = commands.Ephemeral(s, i, fmt.Sprintf("Event #%d cancelled.", ev.ID))
}

func edit(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps, sc *discordgo.ApplicationCommandInteractionDataOption) {
	ev, ok := lookup(ctx, s, i, d, sc)
	if !ok {
		return
	}
	o := optMap(sc.Options)
	if v := str(o, "title"); v != "" {
		ev.Title = v
	}
	if v := str(o, "description"); v != "" {
		ev.Desc = v
	}
	timeChanged := false
	if str(o, "date") != "" || str(o, "time") != "" || str(o, "timezone") != "" {
		loc, err := locFor(ctx, d, i.GuildID, str(o, "timezone"))
		if err != nil {
			_ = commands.Ephemeral(s, i, err.Error())
			return
		}
		start, err := parseWhen(str(o, "date"), str(o, "time"), loc, ev.Start)
		if err != nil {
			_ = commands.Ephemeral(s, i, err.Error())
			return
		}
		if !start.After(time.Now()) {
			_ = commands.Ephemeral(s, i, "That time's already passed. Pick something in the future 🙂")
			return
		}
		ev.Start, timeChanged = start, true
	}
	sent1h := 0
	if time.Until(ev.Start) <= time.Hour {
		sent1h = 1
	}
	if _, err := d.DB.ExecContext(ctx, `UPDATE events SET title = ?, description = ?, start_unix = ?, reminded_1h = ?, reminded_start = 0 WHERE id = ?`,
		ev.Title, ev.Desc, ev.Start.Unix(), sent1h, ev.ID); err != nil {
		_ = commands.Ephemeral(s, i, "Couldn't save that, try again.")
		return
	}
	syncMessage(ctx, s, d, ev)
	if ev.NativeID != "" {
		if _, err := s.GuildScheduledEventEdit(i.GuildID, ev.NativeID, nativeParams(ev), discordgo.WithContext(ctx)); err != nil {
			d.Log.Warn("edit native event", "err", err)
		}
	}
	msg := fmt.Sprintf("Event #%d updated ✅", ev.ID)
	if timeChanged {
		msg += " New time: " + timeTags(ev.Start)
	}
	_ = commands.Ephemeral(s, i, msg)
}

func rsvp(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	parts := strings.Split(i.MessageComponentData().CustomID, ":")
	if len(parts) != 3 || i.Member == nil || !slices.Contains([]string{"going", "maybe", "no"}, parts[1]) {
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return
	}
	ev, err := loadEvent(ctx, d, i.GuildID, id)
	if err != nil || ev.Status != "active" {
		_ = commands.Ephemeral(s, i, "That event isn't taking RSVPs anymore.")
		return
	}
	uid := i.Member.User.ID
	var cur string
	_ = d.DB.QueryRowContext(ctx, `SELECT status FROM event_rsvps WHERE event_id = ? AND user_id = ?`, id, uid).Scan(&cur)
	if cur == parts[1] { // tapping your current answer again clears it
		_, err = d.DB.ExecContext(ctx, `DELETE FROM event_rsvps WHERE event_id = ? AND user_id = ?`, id, uid)
	} else {
		_, err = d.DB.ExecContext(ctx, `INSERT INTO event_rsvps (event_id, user_id, status, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(event_id, user_id) DO UPDATE SET status = excluded.status, updated_at = excluded.updated_at`,
			id, uid, parts[1], time.Now().Unix())
	}
	if err != nil {
		d.Log.Error("rsvp", "err", err)
		_ = commands.Ephemeral(s, i, "Couldn't save your RSVP, try again.")
		return
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:          []*discordgo.MessageEmbed{renderEmbed(ev, rsvps(ctx, d, id))},
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
}

// ---- reminders ----

// StartReminders runs the reminder loop until ctx is cancelled. Call after the session is open.
func StartReminders(ctx context.Context, s *discordgo.Session, d commands.Deps) {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			remind(ctx, s, d)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func remind(ctx context.Context, s *discordgo.Session, d commands.Deps) {
	now := time.Now()
	type due struct {
		ev                event
		sent1h, sentStart bool
	}
	var todo []due
	// Collect first: the DB has a single connection, so rows must be closed before other queries.
	rows, err := d.DB.QueryContext(ctx, `SELECT `+eventCols+`, reminded_1h, reminded_start FROM events
		WHERE status = 'active' AND reminded_start = 0 AND start_unix <= ?`, now.Add(time.Hour).Unix())
	if err != nil {
		d.Log.Error("reminder query", "err", err)
		return
	}
	for rows.Next() {
		var ev event
		var start int64
		var a, b int
		if err := rows.Scan(&ev.ID, &ev.GuildID, &ev.Title, &ev.Desc, &start, &ev.PingRole, &ev.ChannelID, &ev.MessageID, &ev.NativeID, &ev.Creator, &ev.Status, &a, &b); err == nil {
			ev.Start = time.Unix(start, 0)
			todo = append(todo, due{ev, a == 1, b == 1})
		}
	}
	rows.Close()

	for _, t := range todo {
		kind := reminderDue(t.ev.Start, now, t.sent1h, t.sentStart)
		var q string
		switch kind {
		case "skip", "start":
			q = `UPDATE events SET reminded_1h = 1, reminded_start = 1 WHERE id = ? AND reminded_start = 0`
		case "1h":
			q = `UPDATE events SET reminded_1h = 1 WHERE id = ? AND reminded_1h = 0`
		default:
			continue
		}
		res, err := d.DB.ExecContext(ctx, q, t.ev.ID)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 || kind == "skip" {
			continue
		}
		sendReminder(ctx, s, d, t.ev, kind)
	}
}

func sendReminder(ctx context.Context, s *discordgo.Session, d commands.Deps, ev event, kind string) {
	r := rsvps(ctx, d, ev.ID)
	users := append(slices.Clone(r["going"]), r["maybe"]...)
	text := fmt.Sprintf("⏰ **%s** is starting now! <https://discord.com/channels/%s/%s/%s>", ev.Title, ev.GuildID, ev.ChannelID, ev.MessageID)
	if kind == "1h" {
		text = fmt.Sprintf("⏰ **%s** starts <t:%d:R>! <https://discord.com/channels/%s/%s/%s>", ev.Title, ev.Start.Unix(), ev.GuildID, ev.ChannelID, ev.MessageID)
	}
	groups := chunk(users, 50)
	if len(groups) == 0 {
		groups = [][]string{nil}
	}
	for n, g := range groups {
		content := text
		if n > 0 {
			content = "…and more:"
		}
		if len(g) > 0 {
			content += "\n" + mentionList(g, len(g))
		}
		_, err := s.ChannelMessageSendComplex(ev.ChannelID, &discordgo.MessageSend{
			Content:         content,
			AllowedMentions: &discordgo.MessageAllowedMentions{Users: g},
		}, discordgo.WithContext(ctx))
		if err != nil {
			d.Log.Warn("send reminder", "event", ev.ID, "err", err)
			return
		}
	}
}
