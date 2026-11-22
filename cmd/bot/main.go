package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/starjunkies/bot/internal/bot"
	"github.com/starjunkies/bot/internal/config"
	"github.com/starjunkies/bot/internal/db"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := bot.Run(ctx, cfg, database, log); err != nil {
		log.Error("bot", "err", err)
		os.Exit(1)
	}
}
