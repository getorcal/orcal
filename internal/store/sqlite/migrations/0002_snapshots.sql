CREATE TABLE snapshots (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    sandbox_id  TEXT NOT NULL REFERENCES sandboxes (id),
    parent_id   TEXT REFERENCES snapshots (id),
    runtime_ref TEXT NOT NULL,
    image       TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE UNIQUE INDEX snapshots_live_name ON snapshots (name) WHERE name != '';
CREATE INDEX snapshots_sandbox ON snapshots (sandbox_id, id);
CREATE INDEX snapshots_parent ON snapshots (parent_id);

ALTER TABLE sandboxes ADD COLUMN parent_snapshot_id TEXT;
