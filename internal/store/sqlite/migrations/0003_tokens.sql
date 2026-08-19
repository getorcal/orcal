CREATE TABLE tokens (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    hash         TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,
    scopes       TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT,
    last_used_at TEXT,
    revoked_at   TEXT
);

-- Partial, so revoking a token frees its name for reuse. Revocation is a tombstone rather
-- than a delete because audit events name the actor, and a deleted row leaves them anonymous.
CREATE UNIQUE INDEX tokens_live_name ON tokens (name) WHERE revoked_at IS NULL;
