package fun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
)

type lfcPost struct {
	ID                                                        int64
	GuildID, ChannelID, MessageID, Host, Activity, Ship, Note string
	Slots                                                     int
}

func lfcEmbed(p lfcPost, members []string) *discordgo.MessageEmbed {
	crew := "No one yet. Be the first!"
	if len(members) > 0 {
		lines := make([]string, len(members))
		for i, m := range members {
			lines[i] = "<@" + m + ">"
		}
		crew = strings.Join(lines, "\n")
	}
	full := len(members) >= p.Slots
	e := &discordgo.MessageEmbed{
		Title:       "🚀 Looking for crew: " + p.Activity,
		Description: p.Note,
		Color:       commands.ColorTeal,
		Fields:      []*discordgo.MessageEmbedField{{Name: "Host", Value: "<@" + p.Host + ">", Inline: true}},
		Footer:      &discordgo.MessageEmbedFooter{Text: "LFC #" + strconv.FormatInt(p.ID, 10) + " • Hit Join to grab a seat"},
	}
	if p.Ship != "" {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Ship", Value: p.Ship, Inline: true})
	}
	e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: fmt.Sprintf("Crew (%d/%d)", len(members), p.Slots), Value: crew})
	if full {
		e.Color = commands.ColorOrange
		e.Footer.Text = "LFC #" + strconv.FormatInt(p.ID, 10) + " • Crew's full!"
	}
	return e
}

func lfcButtons(p lfcPost, filled int) []discordgo.MessageComponent {
	id := strconv.FormatInt(p.ID, 10)
	join := discordgo.Button{Label: "Join", Style: discordgo.SuccessButton, CustomID: "lfc:join:" + id}
	if filled >= p.Slots {
		join.Label, join.Disabled = "Full", true
	}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		join,
		discordgo.Button{Label: "Leave", Style: discordgo.SecondaryButton, CustomID: "lfc:leave:" + id},
	}}}
}

func lfcMembers(ctx context.Context, d commands.Deps, id int64) []string {
	rows, err := d.DB.QueryContext(ctx, `SELECT user_id FROM lfc_members WHERE lfc_id = ? ORDER BY joined_at, rowid`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			out = append(out, u)
		}
	}
	return out
}

func registerLFC(r *commands.Registry, d commands.Deps) {
	noDM := false
	minSlots := 1.0
	str := func(name, desc string, req bool, max int) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: name, Description: desc, Required: req, MaxLength: max}
	}
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "lfc", Description: "Post a looking-for-crew call", DMPermission: &noDM,
			Options: []*discordgo.ApplicationCommandOption{
				str("activity", "What are you up to? (hauling, mining, vibes...)", true, 60),
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "slots", Description: "How many crew seats are open (1-10)", Required: true, MinValue: &minSlots, MaxValue: 10},
				str("ship", "Which ship?", false, 60),
				str("note", "Anything else to know", false, 200),
			},
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			createLFC(ctx, s, i, d)
		},
	})
	r.Component("lfc", func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
		lfcClick(ctx, s, i, d)
	})
}

func createLFC(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	if i.Member == nil {
		return
	}
	o := commands.OptMap(i.ApplicationCommandData().Options)
	get := func(k string) string {
		if v := o[k]; v != nil {
			return strings.TrimSpace(v.StringValue())
		}
		return ""
	}
	p := lfcPost{GuildID: i.GuildID, Host: i.Member.User.ID, Activity: get("activity"), Ship: get("ship"), Note: get("note"), Slots: int(o["slots"].IntValue())}
	p.ChannelID = commands.ChannelID(ctx, d, i.GuildID, "looking-for-crew")
	if p.ChannelID == "" {
		p.ChannelID = i.ChannelID
	}
	res, err := d.DB.ExecContext(ctx, `INSERT INTO lfc_posts (guild_id, channel_id, host_id, activity, ship, note, slots, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.GuildID, p.ChannelID, p.Host, p.Activity, p.Ship, p.Note, p.Slots, time.Now().Unix())
	if err != nil {
		d.Log.Error("insert lfc", "err", err)
		_ = commands.Ephemeral(s, i, "Couldn't save that, try again in a bit.")
		return
	}
	p.ID, _ = res.LastInsertId()
	msg, err := s.ChannelMessageSendComplex(p.ChannelID, &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{lfcEmbed(p, nil)},
		Components:      lfcButtons(p, 0),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}, discordgo.WithContext(ctx))
	if err != nil {
		_, _ = d.DB.ExecContext(ctx, `DELETE FROM lfc_posts WHERE id = ?`, p.ID)
		_ = commands.Ephemeral(s, i, "I couldn't post there. Check my permissions in that channel.")
		return
	}
	_, _ = d.DB.ExecContext(ctx, `UPDATE lfc_posts SET message_id = ? WHERE id = ?`, msg.ID, p.ID)
	_ = commands.Ephemeral(s, i, "Your crew call is up in <#"+p.ChannelID+"> 🚀")
}

func lfcClick(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	parts := strings.Split(i.MessageComponentData().CustomID, ":")
	if len(parts) != 3 || i.Member == nil || (parts[1] != "join" && parts[1] != "leave") {
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return
	}
	uid := i.Member.User.ID

	// One transaction so two people can't grab the last seat at once.
	// The DB has a single connection: don't touch d.DB until the tx is finished.
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		_ = commands.Ephemeral(s, i, "Database hiccup, try again.")
		return
	}
	defer tx.Rollback()
	var p lfcPost
	err = tx.QueryRowContext(ctx, `SELECT id, guild_id, channel_id, message_id, host_id, activity, ship, note, slots FROM lfc_posts WHERE id = ? AND guild_id = ?`,
		id, i.GuildID).Scan(&p.ID, &p.GuildID, &p.ChannelID, &p.MessageID, &p.Host, &p.Activity, &p.Ship, &p.Note, &p.Slots)
	if errors.Is(err, sql.ErrNoRows) {
		_ = commands.Ephemeral(s, i, "That crew call is gone.")
		return
	}
	if err != nil {
		_ = commands.Ephemeral(s, i, "Database hiccup, try again.")
		return
	}
	var n, mine int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM lfc_members WHERE lfc_id = ?`, id).Scan(&n)
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM lfc_members WHERE lfc_id = ? AND user_id = ?`, id, uid).Scan(&mine)

	switch {
	case parts[1] == "join" && uid == p.Host:
		_ = commands.Ephemeral(s, i, "You're the host, your seat's already saved 😄")
		return
	case parts[1] == "join" && mine > 0:
		_ = commands.Ephemeral(s, i, "You're already in this crew.")
		return
	case parts[1] == "join" && n >= p.Slots:
		_ = commands.Ephemeral(s, i, "Sorry, the crew just filled up. Try the next one!")
		return
	case parts[1] == "join":
		_, err = tx.ExecContext(ctx, `INSERT INTO lfc_members (lfc_id, user_id, joined_at) VALUES (?, ?, ?)`, id, uid, time.Now().Unix())
	case mine == 0:
		_ = commands.Ephemeral(s, i, "You're not in this crew.")
		return
	default:
		_, err = tx.ExecContext(ctx, `DELETE FROM lfc_members WHERE lfc_id = ? AND user_id = ?`, id, uid)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		d.Log.Error("lfc click", "err", err)
		_ = commands.Ephemeral(s, i, "Couldn't save that, try again.")
		return
	}
	members := lfcMembers(ctx, d, id)
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:          []*discordgo.MessageEmbed{lfcEmbed(p, members)},
			Components:      lfcButtons(p, len(members)),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
}
