CREATE TABLE lfc_posts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id   TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    message_id TEXT NOT NULL DEFAULT '',
    host_id    TEXT NOT NULL,
    activity   TEXT NOT NULL,
    ship       TEXT NOT NULL DEFAULT '',
    note       TEXT NOT NULL DEFAULT '',
    slots      INTEGER NOT NULL, -- open seats, not counting the host
    created_at INTEGER NOT NULL  -- unix seconds
);

CREATE TABLE lfc_members (
    lfc_id    INTEGER NOT NULL REFERENCES lfc_posts (id) ON DELETE CASCADE,
    user_id   TEXT NOT NULL,
    joined_at INTEGER NOT NULL,
    PRIMARY KEY (lfc_id, user_id)
);
