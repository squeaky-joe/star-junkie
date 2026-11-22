# Star Junkies Bot

Custom Discord bot for the Star Junkies Star Citizen org. Slash commands and components only, SQLite storage, pure Go (no CGO).

## Setup

1. Create an application at <https://discord.com/developers/applications>, add a **Bot**, copy the token.
2. Copy `.env.example` to `.env` and set `DISCORD_TOKEN` (and `GUILD_ID` while developing).
3. Invite the bot with scopes **`bot`** and **`applications.commands`** and these permissions:
   Manage Roles, Manage Channels, Manage Server, Manage Events, View Channels, Send Messages, Embed Links, Read Message History.
   (Later features add Kick/Ban/Moderate Members, Manage Messages, Manage Events, View Audit Log. The easy route is Administrator.)
4. In **Server Settings → Roles**, drag the bot's role to the **top**. It can only create and order roles below its own.
5. Run it:
   - Local: `go run ./cmd/bot`
   - Docker: `docker compose up -d --build` (DB lives in the `botdata` volume)
6. In Discord, run `/setup dry-run:true` to preview, then `/setup`. Re-running is safe; existing things are skipped.
7. Run `/rolepanel` to post the ping-role buttons in #get-roles. Optional: `/config welcome-channel` (defaults to #general-chat).

8. Set your default event time zone: `/config timezone America/New_York` (otherwise UTC). Then `/event create title date time` (e.g. `2026-12-05` `20:00` or `8pm`). Events show in everyone's local time; Star Junkies and staff can create them.

Enable **Community** in Server Settings first if you want the 🎙️ Town Hall stage channel; otherwise it's skipped (re-run `/setup` after enabling).

## Intents

| Intent | Needed for | Portal toggle |
| --- | --- | --- |
| Guilds | everything (on now) | none |
| Server Members | join/leave, auto-Guest (feature 2+) | **Privileged**, enable it |
| Message Content | anti-spam, message logs (feature 5) | **Privileged**, enable it |

## Layout

```
cmd/bot            entrypoint
internal/bot       session + wiring
internal/commands  command registry, reply helpers, org colors
internal/config    env/.env loading
internal/db        SQLite + embedded migrations (internal/db/migrations/*.sql)
internal/features  one package per feature (setup, ...)
```

The whole server layout (roles, categories, channels, welcome/rules text) is data in `internal/features/setup/layout.go`. Edit it there.

## Dev

```
go vet ./... && go build ./... && go test ./...
```
