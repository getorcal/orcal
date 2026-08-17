CREATE TABLE sandboxes (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    image        TEXT NOT NULL,
    state        TEXT NOT NULL,
    runtime      TEXT NOT NULL,
    runtime_id   TEXT NOT NULL,
    cpu_millis   INTEGER NOT NULL,
    memory_bytes INTEGER NOT NULL,
    pids_limit   INTEGER NOT NULL,
    env          TEXT NOT NULL,
    labels       TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE UNIQUE INDEX sandboxes_live_name
    ON sandboxes (name)
    WHERE name != '' AND state != 'destroyed';

CREATE INDEX sandboxes_state ON sandboxes (state);

CREATE TABLE execs (
    id              TEXT PRIMARY KEY,
    sandbox_id      TEXT NOT NULL REFERENCES sandboxes (id),
    runtime_exec_id TEXT NOT NULL,
    command         TEXT NOT NULL,
    env             TEXT NOT NULL,
    working_dir     TEXT NOT NULL,
    user            TEXT NOT NULL,
    state           TEXT NOT NULL,
    exit_code       INTEGER,
    output_bytes    INTEGER NOT NULL DEFAULT 0,
    truncated       INTEGER NOT NULL DEFAULT 0,
    started_at      TEXT NOT NULL,
    finished_at     TEXT
);

CREATE INDEX execs_sandbox ON execs (sandbox_id, id);
CREATE INDEX execs_state ON execs (state);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
