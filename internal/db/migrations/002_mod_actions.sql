CREATE TABLE mod_actions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id   TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    mod_id     TEXT NOT NULL,
    action     TEXT NOT NULL, -- promote, demote, warn, timeout, kick, ban
    reason     TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL -- unix seconds
);
CREATE INDEX mod_actions_user ON mod_actions (guild_id, user_id);
