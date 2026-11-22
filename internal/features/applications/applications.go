// Package applications: /apply modal, staff review buttons, accept/decline.
package applications

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
)

func Register(r *commands.Registry, d commands.Deps) {
	noDM := false
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "apply", Description: "Apply to become a Star Junkie", DMPermission: &noDM,
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			openModal(ctx, s, i, d)
		},
	})
	// "app:submit" (modal), "app:accept:<id>", "app:decline:<id>" (buttons)
	r.Component("app", func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type == discordgo.InteractionModalSubmit {
			submit(ctx, s, i, d)
			return
		}
		decide(ctx, s, i, d)
	})
}

const (
	fHandle = "handle"
	fFound  = "found"
	fLikes  = "likes"
)

func input(id, label, placeholder string, style discordgo.TextInputStyle, max int) discordgo.ActionsRow {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{
		CustomID: id, Label: label, Placeholder: placeholder, Style: style, Required: true, MaxLength: max,
	}}}
}

func openModal(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	if i.Member == nil {
		return
	}
	if j := commands.RoleID(ctx, d, i.GuildID, "junkie"); j != "" && slices.Contains(i.Member.Roles, j) {
		_ = commands.Ephemeral(s, i, "You're already a Star Junkie, no application needed 🚀")
		return
	}
	var n int
	_ = d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM applications WHERE guild_id = ? AND user_id = ? AND status = 'pending'`,
		i.GuildID, i.Member.User.ID).Scan(&n)
	if n > 0 {
		_ = commands.Ephemeral(s, i, "You've already got an application in. A Co-Pilot will get to it soon 🙂")
		return
	}
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "app:submit",
			Title:    "Join Star Junkies",
			Components: []discordgo.MessageComponent{
				input(fHandle, "Star Citizen handle", "Your in-game handle", discordgo.TextInputShort, 60),
				input(fFound, "How did you find us?", "A friend, Reddit, Spectrum...", discordgo.TextInputShort, 100),
				input(fLikes, "What do you like doing in the verse?", "Hauling, mining, vibes, getting lost...", discordgo.TextInputParagraph, 800),
			},
		},
	})
	if err != nil {
		d.Log.Error("open apply modal", "err", err)
	}
}

// modalValues flattens a modal submission into custom-ID -> text.
func modalValues(rows []discordgo.MessageComponent) map[string]string {
	out := map[string]string{}
	for _, row := range rows {
		ar, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, c := range ar.Components {
			if ti, ok := c.(*discordgo.TextInput); ok {
				out[ti.CustomID] = strings.TrimSpace(ti.Value)
			}
		}
	}
	return out
}

func submit(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	if i.Member == nil {
		return
	}
	v := modalValues(i.ModalSubmitData().Components)
	if v[fHandle] == "" || v[fFound] == "" || v[fLikes] == "" {
		_ = commands.Ephemeral(s, i, "Looks like a field was blank. Give /apply another go?")
		return
	}
	ch := commands.ChannelID(ctx, d, i.GuildID, "applications")
	if ch == "" {
		_ = commands.Ephemeral(s, i, "Applications aren't set up yet. Ask a Co-Pilot to run /setup.")
		return
	}
	user := i.Member.User
	res, err := d.DB.ExecContext(ctx, `INSERT INTO applications (guild_id, user_id, handle, found_via, interests, channel_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, i.GuildID, user.ID, v[fHandle], v[fFound], v[fLikes], ch, time.Now().Unix())
	if err != nil {
		d.Log.Error("save application", "err", err)
		_ = commands.Ephemeral(s, i, "Something went wrong saving that. Please try again in a bit.")
		return
	}
	id, _ := res.LastInsertId()

	msg, err := s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Title:       "New application: " + v[fHandle],
			Description: fmt.Sprintf("From <@%s>", user.ID),
			Color:       commands.ColorOrange,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "How they found us", Value: v[fFound]},
				{Name: "What they like doing", Value: v[fLikes]},
			},
			Footer:    &discordgo.MessageEmbedFooter{Text: "Pending • application #" + strconv.FormatInt(id, 10)},
			Timestamp: time.Now().Format(time.RFC3339),
		}},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Accept", Style: discordgo.SuccessButton, CustomID: customID("accept", id)},
			discordgo.Button{Label: "Decline", Style: discordgo.SecondaryButton, CustomID: customID("decline", id)},
		}}},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}, discordgo.WithContext(ctx))
	if err != nil {
		d.Log.Error("post application", "err", err)
		_, _ = d.DB.ExecContext(ctx, `DELETE FROM applications WHERE id = ?`, id) // so they can retry
		_ = commands.Ephemeral(s, i, "I couldn't reach the staff channel. Please try again shortly.")
		return
	}
	_, _ = d.DB.ExecContext(ctx, `UPDATE applications SET message_id = ? WHERE id = ?`, msg.ID, id)
	_ = commands.Ephemeral(s, i, "Thanks! Your application is in. A Co-Pilot will take a look soon 🚀")
}

func customID(action string, id int64) string {
	return "app:" + action + ":" + strconv.FormatInt(id, 10)
}

// parseCustomID splits "app:accept:12" into ("accept", 12).
func parseCustomID(s string) (action string, id int64, ok bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 || parts[0] != "app" || (parts[1] != "accept" && parts[1] != "decline") {
		return "", 0, false
	}
	n, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return parts[1], n, true
}

func decide(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, d commands.Deps) {
	action, id, ok := parseCustomID(i.MessageComponentData().CustomID)
	if !ok || i.Member == nil {
		return
	}
	if !commands.IsStaff(ctx, d, i) {
		_ = commands.Ephemeral(s, i, "Only Co-Pilots and up can review applications 🔒")
		return
	}
	status := map[string]string{"accept": "accepted", "decline": "declined"}[action]

	// Claim it first so a double-click (or two Co-Pilots) can't decide twice.
	res, err := d.DB.ExecContext(ctx, `UPDATE applications SET status = ?, decided_at = ?, decided_by = ?
		WHERE id = ? AND guild_id = ? AND status = 'pending'`, status, time.Now().Unix(), i.Member.User.ID, id, i.GuildID)
	if err != nil {
		d.Log.Error("claim application", "err", err)
		_ = commands.Ephemeral(s, i, "Database hiccup, try again.")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = commands.Ephemeral(s, i, "Someone already handled this one 🙂")
		return
	}
	var applicant string
	if err := d.DB.QueryRowContext(ctx, `SELECT user_id FROM applications WHERE id = ?`, id).Scan(&applicant); err != nil {
		d.Log.Error("load application", "err", err)
		revert(ctx, d, id)
		_ = commands.Ephemeral(s, i, "Database hiccup, try again.")
		return
	}

	opt := discordgo.WithContext(ctx)
	dm := "Thanks so much for applying to Star Junkies! We're not able to bring you in right now, but you're always welcome to hang out as a Guest and try again later. 💛"
	if action == "accept" {
		junkie, guest := commands.RoleID(ctx, d, i.GuildID, "junkie"), commands.RoleID(ctx, d, i.GuildID, "guest")
		if junkie == "" {
			revert(ctx, d, id)
			_ = commands.Ephemeral(s, i, "The Star Junkie role isn't set up. Run /setup first.")
			return
		}
		if err := s.GuildMemberRoleAdd(i.GuildID, applicant, junkie, opt); err != nil {
			revert(ctx, d, id) // leave it pending so staff can retry
			_ = commands.Ephemeral(s, i, "Couldn't give them the role (did they leave, or is my role too low?): "+err.Error())
			return
		}
		if guest != "" {
			_ = s.GuildMemberRoleRemove(i.GuildID, applicant, guest, opt)
		}
		dm = "Welcome aboard, Star Junkie! 🚀 Your application was accepted. Head into the server, grab your ping roles, and jump in whenever you like. No quotas, no pressure."
	}

	dmNote := ""
	if err := sendDM(s, applicant, dm); err != nil {
		dmNote = " (Their DMs are closed, so I couldn't message them.)"
	}

	// Update the review message: new colour, decision in the footer, buttons gone.
	var embeds []*discordgo.MessageEmbed
	if len(i.Message.Embeds) > 0 {
		e := i.Message.Embeds[0]
		e.Color = map[string]int{"accept": commands.ColorTeal, "decline": commands.ColorIndigo}[action]
		e.Footer = &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("%s by %s", strings.ToUpper(status[:1])+status[1:], i.Member.User.Username)}
		embeds = []*discordgo.MessageEmbed{e}
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Embeds: embeds, Components: []discordgo.MessageComponent{},
			AllowedMentions: &discordgo.MessageAllowedMentions{}},
	})
	if dmNote != "" {
		_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: "Done." + dmNote, Flags: discordgo.MessageFlagsEphemeral})
	}
	commands.PostLog(ctx, s, d, i.GuildID, &discordgo.MessageEmbed{
		Title:       "Application " + status,
		Description: fmt.Sprintf("<@%s> by <@%s>", applicant, i.Member.User.ID),
		Color:       commands.ColorOrange,
		Timestamp:   time.Now().Format(time.RFC3339),
	})
}

func revert(ctx context.Context, d commands.Deps, id int64) {
	_, _ = d.DB.ExecContext(ctx, `UPDATE applications SET status = 'pending', decided_at = NULL, decided_by = '' WHERE id = ?`, id)
}

func sendDM(s *discordgo.Session, userID, text string) error {
	ch, err := s.UserChannelCreate(userID)
	if err != nil {
		return err
	}
	_, err = s.ChannelMessageSend(ch.ID, text)
	return err
}
