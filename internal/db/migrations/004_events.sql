CREATE TABLE events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id       TEXT NOT NULL,
    title          TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    start_unix     INTEGER NOT NULL, -- unix seconds, UTC instant
    ping_role_id   TEXT NOT NULL DEFAULT '',
    channel_id     TEXT NOT NULL DEFAULT '',
    message_id     TEXT NOT NULL DEFAULT '',
    native_id      TEXT NOT NULL DEFAULT '', -- Discord Scheduled Event ID, if created
    creator_id     TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active', -- active, cancelled
    reminded_1h    INTEGER NOT NULL DEFAULT 0,
    reminded_start INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL
);
CREATE INDEX events_upcoming ON events (status, start_unix);

CREATE TABLE event_rsvps (
    event_id   INTEGER NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL,
    status     TEXT NOT NULL, -- going, maybe, no
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (event_id, user_id)
);
