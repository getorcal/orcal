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

-- Partial index so unnamed snapshots (name = '') can coexist while named ones stay unique,
-- and so deleting a snapshot frees its name for reuse.
CREATE UNIQUE INDEX snapshots_live_name ON snapshots (name) WHERE name != '';
CREATE INDEX snapshots_sandbox ON snapshots (sandbox_id, id);
CREATE INDEX snapshots_parent ON snapshots (parent_id);

-- Deliberately NOT "REFERENCES snapshots (id)". Lineage is derived, not maintained, and
-- snapshots outlive the sandboxes that made them. A live FK here would make snapshot.Delete
-- fail with an opaque SQLite error for any snapshot a sandbox was forked from, because its
-- only guard is CountChildren, which counts child snapshots via parent_id and knows nothing
-- about sandboxes. A dangling pointer after a delete is the accepted cost.
ALTER TABLE sandboxes ADD COLUMN parent_snapshot_id TEXT;
