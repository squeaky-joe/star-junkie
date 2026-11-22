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

	ContentDir string // CONTENT_DIR: editable history.md, charter.md, ships.json
}

func Load() (Config, error) {
	_ = godotenv.Load() // .env is optional
	c := Config{
		Token:   os.Getenv("DISCORD_TOKEN"),
		GuildID: os.Getenv("GUILD_ID"),
		DBPath:  os.Getenv("DB_PATH"),

		ContentDir: os.Getenv("CONTENT_DIR"),
	}
	if c.ContentDir == "" {
		c.ContentDir = "content"
	}
	if c.DBPath == "" {
		c.DBPath = "data/bot.db"
	}
	if c.Token == "" {
		return c, errors.New("DISCORD_TOKEN is not set")
	}
	return c, nil
}
