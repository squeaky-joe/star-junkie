// Package config loads settings from environment variables (and an optional .env file).
package config

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Token   string // DISCORD_TOKEN (required)
	GuildID string // GUILD_ID (optional): register commands to one guild for instant updates in dev
	DBPath  string // DB_PATH
}

func Load() (Config, error) {
	_ = godotenv.Load() // .env is optional
	c := Config{
		Token:   os.Getenv("DISCORD_TOKEN"),
		GuildID: os.Getenv("GUILD_ID"),
		DBPath:  os.Getenv("DB_PATH"),
	}
	if c.DBPath == "" {
		c.DBPath = "data/bot.db"
	}
	if c.Token == "" {
		return c, errors.New("DISCORD_TOKEN is not set")
	}
	return c, nil
}
