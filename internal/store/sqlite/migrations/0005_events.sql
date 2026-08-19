CREATE TABLE events (
    id             TEXT PRIMARY KEY,
    ts             TEXT NOT NULL,
    actor_token_id TEXT NOT NULL,
    actor_name     TEXT NOT NULL,
    action         TEXT NOT NULL,
    resource_type  TEXT NOT NULL,
    resource_id    TEXT NOT NULL,
    request_id     TEXT NOT NULL,
    status         INTEGER NOT NULL,
    remote_addr    TEXT NOT NULL,
    details        TEXT NOT NULL
);

CREATE INDEX events_action ON events (action, id);
CREATE INDEX events_actor ON events (actor_token_id, id);
CREATE INDEX events_resource ON events (resource_type, resource_id, id);
