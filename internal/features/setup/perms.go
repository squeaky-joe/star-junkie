package setup

import "github.com/bwmarrin/discordgo"

// RoleIDs are the IDs the overwrite builder needs. BotUser may be empty.
type RoleIDs struct {
	Everyone, Founder, CoPilot, Bots, Junkie, Guest, BotUser string
}

const (
	permRead = discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory |
		discordgo.PermissionAddReactions | discordgo.PermissionVoiceConnect
	permTalk = discordgo.PermissionSendMessages | discordgo.PermissionSendMessagesInThreads |
		discordgo.PermissionCreatePublicThreads | discordgo.PermissionEmbedLinks |
		discordgo.PermissionAttachFiles | discordgo.PermissionUseExternalEmojis |
		discordgo.PermissionVoiceSpeak | discordgo.PermissionVoiceStreamVideo | discordgo.PermissionVoiceUseVAD
	// Denied on read-only channels (reactions stay allowed).
	permNoTalk = discordgo.PermissionSendMessages | discordgo.PermissionSendMessagesInThreads |
		discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads
)

// Overwrites builds the permission overwrites for a category/channel. Pure, so it's unit-tested.
func Overwrites(r RoleIDs, a Access, readOnly bool) []*discordgo.PermissionOverwrite {
	var out []*discordgo.PermissionOverwrite
	role := func(id string, allow, deny int64) {
		if id != "" {
			out = append(out, &discordgo.PermissionOverwrite{ID: id, Type: discordgo.PermissionOverwriteTypeRole, Allow: allow, Deny: deny})
		}
	}
	const talk = permRead | permTalk
	// viewer grants read (+talk unless read-only) to a role that can see the channel.
	viewer := func(id string) {
		if readOnly {
			role(id, permRead, permNoTalk)
		} else {
			role(id, talk, 0)
		}
	}

	switch a {
	case Public:
		viewer(r.Everyone)
	case Members:
		role(r.Everyone, 0, discordgo.PermissionViewChannel)
		viewer(r.Guest)
		viewer(r.Junkie)
	case Junkies:
		role(r.Everyone, 0, discordgo.PermissionViewChannel)
		viewer(r.Junkie)
	case Staff:
		role(r.Everyone, 0, discordgo.PermissionViewChannel)
	}
	// Staff and bots can always see and talk.
	for _, id := range []string{r.CoPilot, r.Founder, r.Bots} {
		role(id, talk, 0)
	}
	if r.BotUser != "" {
		out = append(out, &discordgo.PermissionOverwrite{ID: r.BotUser, Type: discordgo.PermissionOverwriteTypeMember, Allow: talk})
	}
	return out
}
