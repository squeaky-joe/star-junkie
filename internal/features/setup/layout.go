package setup

import (
	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
)

// Everything /setup builds lives in this file. Edit the data, not the logic.

// Access says who can see a category or channel.
type Access int

const (
	Public  Access = iota // everyone
	Members               // Guest + Star Junkie + staff
	Junkies               // Star Junkie + staff
	Staff                 // Co-Pilot + Founder
)

type RoleDef struct {
	Key         string // stored in the DB as role.<key>
	Name        string
	Color       int
	Perms       int64
	Hoist       bool
	Mentionable bool
}

type Kind int

const (
	Text Kind = iota
	Voice
	Stage
)

// Post is the first message dropped into a freshly created channel.
// "#{name}" is replaced with a real channel mention.
type Post struct{ Title, Body string }

type ChannelDef struct {
	Name     string
	Kind     Kind
	Topic    string
	ReadOnly bool // only staff/bots can talk
	Open     bool // override: everyone can talk even if the category is read-only
	AFK      bool // voice: set as the server AFK channel (15 min)
	Post     *Post
}

type CategoryDef struct {
	Name     string
	Access   Access
	Channels []ChannelDef
}

// Roles, highest first.
var Roles = []RoleDef{
	{Key: "founder", Name: "👑 Founder", Color: 0xF2A04B, Perms: discordgo.PermissionAdministrator, Hoist: true},
	{Key: "copilot", Name: "🛠️ Co-Pilot", Color: 0xE67E3A, Hoist: true, Perms: discordgo.PermissionManageMessages |
		discordgo.PermissionKickMembers | discordgo.PermissionModerateMembers | discordgo.PermissionVoiceMuteMembers |
		discordgo.PermissionVoiceMoveMembers | discordgo.PermissionManageNicknames | discordgo.PermissionManageEvents |
		discordgo.PermissionViewAuditLogs},
	{Key: "bots", Name: "🤖 Bots", Color: 0x3B9C9C, Hoist: true},
	{Key: "junkie", Name: "🚀 Star Junkie", Color: 0xF3EAD3, Hoist: true},
	{Key: "guest", Name: "🛰️ Guest", Color: 0x8A87A8},
}

// Self-assignable ping roles (the picker panel comes in feature 2).
var PingRoles = []string{
	"Game Night", "Trading", "Mining", "Exploring",
	"Bounty Hunting", "Cargo Hauling", "Racing", "Other Games",
}

var Categories = []CategoryDef{
	{Name: "📌 START HERE", Access: Public, Channels: []ChannelDef{
		{Name: "welcome", ReadOnly: true, Post: &Post{
			Title: "Welcome aboard, Star Junkie 🚀",
			Body: "Star Junkies is a laid-back Star Citizen org, founded in 2956 (that's 2026-11-18 out here in the real world).\n\n" +
				"No quotas, no pressure, no mandatory fleet nights. We just like hanging out, chilling, and flying together when it suits you.\n\n" +
				"**Start here:**\n• Give the rules a quick read in #{rules}\n• Grab ping roles in #{get-roles}\n• Say hi in #{introductions}\n\nGlad you're here!"}},
		{Name: "rules", ReadOnly: true, Post: &Post{
			Title: "The rules (short, we promise)",
			Body: "**1. Be chill.** Be kind. We're here to relax.\n" +
				"**2. No harassment, hate, or drama.** Zero tolerance.\n" +
				"**3. Keep it friendly.** No NSFW stuff, anywhere.\n" +
				"**4. No pressure.** Play when you want, how you want. Nobody owes the org their time.\n" +
				"**5. Don't grief your fellow Junkies.** PvP only when everyone's into it.\n" +
				"**6. No spam or ads.** Use the right channel for the right chat.\n" +
				"**7. Co-Pilots keep the peace.** If one asks you to cool it, please do.\n\nBreaking a rule? Expect a friendly nudge first. Repeat it and we'll get more serious."}},
		{Name: "announcements", ReadOnly: true},
		{Name: "get-roles", ReadOnly: true, Topic: "Pick the pings you actually want."},
		{Name: "introductions", Open: true, Topic: "Tell us your handle and what you like doing in the verse."},
	}},
	{Name: "💬 THE LOUNGE", Access: Members, Channels: []ChannelDef{
		{Name: "general-chat"}, {Name: "memes-and-clips"}, {Name: "off-topic"},
		{Name: "pets-and-food"}, {Name: "bot-commands"},
	}},
	{Name: "🚀 STAR CITIZEN", Access: Junkies, Channels: []ChannelDef{
		{Name: "sc-chat"}, {Name: "looking-for-crew"}, {Name: "patch-news"}, {Name: "tips-and-help"},
		{Name: "ship-showcase"}, {Name: "screenshots"}, {Name: "trading-and-routes"},
		{Name: "bounties-and-combat"}, {Name: "mining-and-salvage"}, {Name: "exploration-and-lore"},
	}},
	{Name: "🗓️ EVENTS", Access: Members, Channels: []ChannelDef{
		{Name: "event-calendar", ReadOnly: true},
		{Name: "game-night-planning"}, {Name: "event-recaps"},
	}},
	{Name: "🎮 OTHER GAMES", Access: Members, Channels: []ChannelDef{
		{Name: "other-games-chat"}, {Name: "looking-for-group"},
	}},
	// Voice access wasn't specified; Members (Guest + Star Junkie + staff) matches the lounge.
	{Name: "🔊 VOICE", Access: Members, Channels: []ChannelDef{
		{Name: "🛰️ Main Hangar", Kind: Voice},
		{Name: "🚀 Fleet Ops 1", Kind: Voice},
		{Name: "🚀 Fleet Ops 2", Kind: Voice},
		{Name: "🌌 Chill Zone", Kind: Voice},
		{Name: "🎧 AFK / Drifting", Kind: Voice, AFK: true},
		{Name: "🎙️ Town Hall", Kind: Stage}, // needs Community enabled; skipped otherwise
	}},
	{Name: "🛠️ ADMIN", Access: Staff, Channels: []ChannelDef{
		{Name: "mod-chat"}, {Name: "applications"}, {Name: "server-logs"}, {Name: "bot-config"},
	}},
}

const (
	embedColor = commands.ColorIndigo
	afkSeconds = 15 * 60
)
