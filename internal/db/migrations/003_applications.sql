CREATE TABLE applications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id   TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    handle     TEXT NOT NULL, -- Star Citizen handle
    found_via  TEXT NOT NULL,
    interests  TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending', -- pending, accepted, declined
    channel_id TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL, -- unix seconds
    decided_at INTEGER,
    decided_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX applications_user ON applications (guild_id, user_id, status);
