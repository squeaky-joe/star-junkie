# Star Junkies Bot

Custom Discord bot for the Star Junkies Star Citizen org. Slash commands and components only, SQLite storage, pure Go (no CGO).

## Setup

1. Create an application at <https://discord.com/developers/applications>, add a **Bot**, copy the token.
2. Copy `.env.example` to `.env` and set `DISCORD_TOKEN` (and `GUILD_ID` while developing).
3. Invite the bot with scopes **`bot`** and **`applications.commands`** and these permissions:
   Manage Roles, Manage Channels, Manage Server, Manage Events, Manage Messages, Kick Members, Ban Members, Moderate Members, View Audit Log, View Channels, Send Messages, Embed Links, Read Message History.
   (The easy route is Administrator.)
4. In **Server Settings → Roles**, drag the bot's role to the **top**. It can only create and order roles below its own.
5. Run it:
   - Local: `go run ./cmd/bot`
   - Docker: `docker compose up -d --build` (DB lives in the `botdata` volume)
6. In Discord, run `/setup dry-run:true` to preview, then `/setup`. Re-running is safe; existing things are skipped.
7. Run `/rolepanel` to post the ping-role buttons in #get-roles. Optional: `/config welcome-channel` (defaults to #general-chat).

8. Set your default event time zone: `/config timezone America/New_York` (otherwise UTC). Then `/event create title date time` (e.g. `2026-12-05` `20:00` or `8pm`). Events show in everyone's local time; Star Junkies and staff can create them.

9. Moderation (Co-Pilot+): `/warn`, `/timeout`, `/kick`, `/ban`, `/modlog`. Everything is logged to #server-logs. `/antispam` shows or tunes the filter (defaults: 5 identical messages in 15s, or 10+ mentions in one message; staff exempt; every removal is logged, and it does nothing if #server-logs doesn't exist).

10. Fun: `/lfc` (crew call with Join/Leave buttons), `/ship` (autocomplete lookup), `/roll`, `/coinflip`, `/verse-fortune`, `/history`, `/charter`, `/manifesto`, `/rules`.

Ship data for `/ship` comes from the [Star Citizen Wiki API](https://api.star-citizen.wiki/developers) (credited in each embed). The bot downloads the full list every 12 hours (about 15 MB, ~15 seconds), keeps it in memory and saves it to `content/ships.json`, so commands never hit the API and a restart within 12 hours loads from the file instead of downloading again. If the API is down, the last saved list keeps working. Don't hand-edit `ships.json`; the bot overwrites it. Set `SHIP_API_URL=off` to stop updating it and use the file as-is. The content folder must be writable by the bot for the cache to save (if it isn't, you get a log warning and the bot still works).

Edit `content/history.md`, `charter.md`, `manifesto.md` and `rules.md` any time; the bot re-reads them on every use, no restart.

Enable **Community** in Server Settings first if you want the 🎙️ Town Hall stage channel; otherwise it's skipped (re-run `/setup` after enabling).

## Intents

| Intent | Needed for | Portal toggle |
| --- | --- | --- |
| Guilds | everything (on now) | none |
| Server Members | join/leave, auto-Guest, role-change logs | **Privileged**, enable it |
| Message Content | anti-spam, message edit/delete logs | **Privileged**, enable it |
| Guild Moderation (bans) | ban logs | none |

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

## Published image

GitHub Actions (`.github/workflows/docker.yml`) publishes `ghcr.io/squeaky-joe/star-junkie` with these tags:

| Tag | When | Use it for |
|---|---|---|
| `latest` | every release | the newest release |
| `2026.11.3`, `2026.11`, `2026` | every release, from the git tag `2026.11.3` | pinning (`2026.11` follows the month's newest revision) |

Only tagged releases build an image; ordinary pushes to `main` don't.

Releases are versioned `year.month.revision`: `2026.11.1` is the first release of November 2026, then `2026.11.2`, and the revision restarts at 1 each month. **Don't zero-pad the month** (`2026.1.1`, not `2026.01.1`), because the tag tooling only accepts SemVer-shaped numbers.

To cut a release: `git tag 2026.11.1 && git push origin 2026.11.1`. To run the image without building, replace `build: .` in `docker-compose.yml` with `image: ghcr.io/squeaky-joe/star-junkie:2026.11` (or `:latest`). The package is private until you make it public (repo → Packages → Package settings → Change visibility).

## Dev

```
go vet ./... && go build ./... && go test ./...
```

## License

[MIT](LICENSE). Ship data comes from the community-run [Star Citizen Wiki API](https://api.star-citizen.wiki/developers) (please keep the credit and non-commercial use it asks for).

This is an unofficial fan project, not affiliated with or endorsed by Cloud Imperium Games. Star Citizen® and related names are trademarks of Cloud Imperium Rights LLC.
