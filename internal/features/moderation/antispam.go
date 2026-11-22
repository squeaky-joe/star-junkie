package moderation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
	"github.com/starjunkies/bot/internal/db"
)

// Conservative defaults. Everything the filter removes is posted to #server-logs, and staff are exempt.
type spamCfg struct {
	Enabled                   bool
	Repeats, Mentions, Window int // identical messages, mentions in one message, seconds
}

var defaultSpam = spamCfg{Enabled: true, Repeats: 5, Mentions: 10, Window: 15}

type spamEntry struct {
	msgID, chID, norm string
	at                time.Time
}

type tracker struct {
	mu sync.Mutex
	m  map[string][]spamEntry
}

var spam = &tracker{m: map[string][]spamEntry{}}

func normalize(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// add records a message for key and returns the offending entries once `repeats` identical
// messages land within `window`. Flagged entries are dropped so the next burst starts fresh.
func (t *tracker) add(key string, e spamEntry, window time.Duration, repeats int) []spamEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	cut := e.at.Add(-window)
	var kept, same []spamEntry
	for _, x := range t.m[key] {
		if x.at.After(cut) {
			kept = append(kept, x)
		}
	}
	kept = append(kept, e)
	for _, x := range kept {
		if x.norm == e.norm {
			same = append(same, x)
		}
	}
	if len(same) >= repeats {
		var rest []spamEntry
		for _, x := range kept {
			if x.norm != e.norm {
				rest = append(rest, x)
			}
		}
		if len(rest) == 0 {
			delete(t.m, key)
		} else {
			t.m[key] = rest
		}
		return same
	}
	t.m[key] = kept
	if len(t.m) > 5000 { // sweep idle users
		for k, v := range t.m {
			if v[len(v)-1].at.Before(cut) {
				delete(t.m, k)
			}
		}
	}
	return nil
}

func uniqueMentions(m *discordgo.Message) int {
	seen := map[string]bool{}
	for _, u := range m.Mentions {
		seen[u.ID] = true
	}
	for _, r := range m.MentionRoles {
		seen["r"+r] = true
	}
	return len(seen)
}

// ---- settings (cached so busy chats don't hammer the DB) ----

type cachedCfg struct {
	cfg spamCfg
	at  time.Time
}

var (
	cfgMu    sync.Mutex
	cfgCache = map[string]cachedCfg{}
)

func loadSpam(ctx context.Context, d commands.Deps, guildID string) spamCfg {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if c, ok := cfgCache[guildID]; ok && time.Since(c.at) < 30*time.Second {
		return c.cfg
	}
	cfg := defaultSpam
	get := func(k string) string { v, _ := db.GetConfig(ctx, d.DB, guildID, "spam."+k); return v }
	if v := get("enabled"); v != "" {
		cfg.Enabled = v == "1"
	}
	for k, dst := range map[string]*int{"repeats": &cfg.Repeats, "mentions": &cfg.Mentions, "window": &cfg.Window} {
		if n, err := strconv.Atoi(get(k)); err == nil && n > 0 {
			*dst = n
		}
	}
	cfgCache[guildID] = cachedCfg{cfg, time.Now()}
	return cfg
}

func summary(c spamCfg) string {
	state := "off"
	if c.Enabled {
		state = "on"
	}
	return fmt.Sprintf("Anti-spam is **%s**: %d identical messages within %ds, or %d+ mentions in one message. "+
		"Everything it removes is posted in #server-logs. Co-Pilots and up are exempt.", state, c.Repeats, c.Window, c.Mentions)
}

func registerAntispam(r *commands.Registry, d commands.Deps) {
	admin := int64(discordgo.PermissionAdministrator)
	noDM := false
	intOpt := func(name, desc string, min, max float64) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionInteger, Name: name, Description: desc, MinValue: &min, MaxValue: max}
	}
	r.Add(commands.Command{
		Def: &discordgo.ApplicationCommand{
			Name: "antispam", Description: "Show or change the anti-spam settings (admin)",
			DefaultMemberPermissions: &admin, DMPermission: &noDM,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionBoolean, Name: "enabled", Description: "Turn the filter on or off"},
				intOpt("repeats", "Identical messages that count as spam (2-20)", 2, 20),
				intOpt("mentions", "Mentions in one message that count as spam (3-50)", 3, 50),
				intOpt("window", "Seconds to watch for repeats (5-60)", 5, 60),
			},
		},
		Handle: func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
			if !commands.IsAdmin(i) {
				_ = commands.Ephemeral(s, i, "Admins only, sorry 🔒")
				return
			}
			for _, o := range i.ApplicationCommandData().Options {
				val := strconv.FormatInt(o.IntValue(), 10)
				if o.Type == discordgo.ApplicationCommandOptionBoolean {
					val = "0"
					if o.BoolValue() {
						val = "1"
					}
				}
				if err := db.SetConfig(ctx, d.DB, i.GuildID, "spam."+o.Name, val); err != nil {
					_ = commands.Ephemeral(s, i, "Couldn't save that: "+err.Error())
					return
				}
			}
			cfgMu.Lock()
			delete(cfgCache, i.GuildID)
			cfgMu.Unlock()
			_ = commands.Ephemeral(s, i, summary(loadSpam(ctx, d, i.GuildID)))
		},
	})
}

// ---- the filter ----

func onMessage(ctx context.Context, s *discordgo.Session, d commands.Deps, m *discordgo.MessageCreate) {
	if m.GuildID == "" || m.Author == nil || m.Author.Bot || m.Member == nil {
		return
	}
	cfg := loadSpam(ctx, d, m.GuildID)
	if !cfg.Enabled {
		return
	}
	// Never delete without a place to log it.
	if commands.ChannelID(ctx, d, m.GuildID, "server-logs") == "" {
		return
	}
	if exempt(ctx, s, d, m) {
		return
	}

	var victims []spamEntry
	reason := ""
	if n := uniqueMentions(m.Message); n >= cfg.Mentions {
		victims = []spamEntry{{msgID: m.ID, chID: m.ChannelID}}
		reason = fmt.Sprintf("Mass mention (%d mentions in one message)", n)
	} else if norm := normalize(m.Content); norm != "" {
		e := spamEntry{msgID: m.ID, chID: m.ChannelID, norm: norm, at: time.Now()}
		if hit := spam.add(m.GuildID+"|"+m.Author.ID, e, time.Duration(cfg.Window)*time.Second, cfg.Repeats); hit != nil {
			victims = hit
			reason = fmt.Sprintf("Repeated the same message %d times in %ds", len(hit), cfg.Window)
		}
	}
	if len(victims) == 0 {
		return
	}

	removed := 0
	for _, v := range victims {
		recent.set("spamdel", m.GuildID, v.msgID) // so the delete event isn't logged a second time
		if err := s.ChannelMessageDelete(v.chID, v.msgID, discordgo.WithContext(ctx)); err != nil {
			d.Log.Warn("antispam delete", "err", err)
			continue
		}
		removed++
	}
	d.Log.Info("antispam removed", "user", m.Author.ID, "count", removed, "reason", reason)
	commands.PostLog(ctx, s, d, m.GuildID, logEmbed("Anti-spam removed message(s)", commands.ColorOrange, "",
		field("User", who(m.Author), true), field("Channel", "<#"+m.ChannelID+">", true),
		field("Why", reason, false), field("Removed", strconv.Itoa(removed), true), field("Latest message", m.Content, false)))
}

// exempt: bots are filtered earlier; staff (Manage Messages / Administrator, or Co-Pilot/Founder role) are never touched.
func exempt(ctx context.Context, s *discordgo.Session, d commands.Deps, m *discordgo.MessageCreate) bool {
	if isStaffMember(ctx, d, m.GuildID, m.Member) {
		return true
	}
	p, err := s.State.UserChannelPermissions(m.Author.ID, m.ChannelID)
	return err == nil && p&(discordgo.PermissionAdministrator|discordgo.PermissionManageMessages) != 0
}
