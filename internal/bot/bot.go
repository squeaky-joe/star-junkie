// Package bot wires config, the Discord session, and feature registration together.
package bot

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/starjunkies/bot/internal/commands"
	"github.com/starjunkies/bot/internal/config"
	"github.com/starjunkies/bot/internal/features/applications"
	"github.com/starjunkies/bot/internal/features/events"
	"github.com/starjunkies/bot/internal/features/onboarding"
	"github.com/starjunkies/bot/internal/features/setup"
)

// Run connects, registers commands, and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg config.Config, db *sql.DB, log *slog.Logger) error {
	s, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return err
	}
	// Add intents per feature as they land (see README). GuildMembers is privileged.
	s.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMembers

	reg := commands.NewRegistry()
	deps := commands.Deps{DB: db, Log: log}
	setup.Register(reg, deps)
	onboarding.Register(reg, deps)
	applications.Register(reg, deps)
	events.Register(reg, deps)
	onboarding.AddHandlers(ctx, s, deps)

	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		hctx, cancel := context.WithTimeout(ctx, 14*time.Minute) // interaction tokens last 15 min
		defer cancel()
		reg.Dispatch(hctx, s, i)
	})
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Info("connected", "user", r.User.Username, "guilds", len(r.Guilds))
	})

	if err := s.Open(); err != nil {
		return fmt.Errorf("open session: %w", err)
	}
	defer s.Close()

	// Empty GuildID = global commands (can take a while to propagate); set GUILD_ID in dev.
	if _, err := s.ApplicationCommandBulkOverwrite(s.State.User.ID, cfg.GuildID, reg.Defs()); err != nil {
		return fmt.Errorf("register commands: %w", err)
	}
	events.StartReminders(ctx, s, deps)
	log.Info("commands registered", "count", len(reg.Defs()), "guild", cfg.GuildID)

	<-ctx.Done()
	log.Info("shutting down")
	return nil
}
