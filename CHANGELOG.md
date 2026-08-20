# Changelog

Notable changes to Orcal, newest first.

Entries describe observable behavior. Implementation detail, refactors, CI
changes, and test work belong in the commit history rather than here. Versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html), and until
1.0.0 the minor version may carry breaking changes.

The release workflow publishes the GitHub Release body from this file. Wrap the
entry being released in `<!-- release:start -->` and `<!-- release:end -->`, and
remove those markers from the previous entry so exactly one block is marked at a
time. Tagging fails if the tag has no matching `## <version>` heading here, or if
the marked block is missing or duplicated.

## Unreleased

Nothing has been released yet. Everything below is on `main` and unversioned.

### Added

- **Sandboxes:** Create an isolated environment from any image and address it by
  ID or name. Start, stop, inspect, list, and destroy it over a REST API. CPU,
  memory, and process-count limits are configurable per sandbox and default to
  1 core, 1 GiB, and 512 processes.
- **Exec as a resource:** Commands run inside a sandbox are addressable, so a
  minutes-long build can be started, disconnected from, and reattached to later.
  Output streams live and is replayed in full on reconnect, capped at 64 MiB per
  exec.
- **Snapshots:** Capture a sandbox's filesystem as a named, addressable point in
  time. Snapshots work against a running or stopped sandbox.
- **Restore:** Roll a sandbox back to any of its snapshots in place, keeping its
  ID, name, and configuration.
- **Fork:** Branch a new sandbox from a snapshot. Lineage is recorded, so a
  sandbox knows the snapshot it came from and a snapshot knows its parent.
- **File transfer:** Read, write, stat, and list files inside a sandbox, and move
  whole directory trees in and out as archives. Every file operation works
  against a stopped sandbox and against images with no shell.
- **`orcal cp`:** Copy files and directories between the host and a sandbox in
  either direction, with `scp`-style path syntax and `-r` for trees. Uploads
  merge into an existing directory and never implicitly delete.
- **Command-line client:** `orcal` covers the full API, with `--output json` on
  every command and Docker-compatible exit codes.
- **OpenAPI contract:** The API is defined by a hand-authored OpenAPI document
  that is the source of truth for the implementation, published for clients to
  generate against.
- **Docker Compose deployment:** `docker compose up` brings up a single service
  with an embedded database and no external dependencies.

### Security

- **Hardened sandboxes by default:** Every sandbox drops all Linux capabilities,
  runs with `no-new-privileges`, keeps Docker's default seccomp profile, disables
  swap, mounts nothing from the host, publishes no ports, and cannot reach other
  sandboxes over the network.
- **Scoped, revocable tokens:** Every endpoint except health and version requires
  a bearer token, and there is no mode that disables it. Tokens are scoped to the
  specific operations they grant — reading or managing sandboxes, running
  commands, transferring files, managing snapshots, reading the audit log, and
  managing other tokens are each a separate grant rather than implied by one
  another — and can be listed and revoked at any time. A token is generated on
  first start and printed once. The default bind address is loopback.
- **Per-sandbox network mode:** A sandbox is created with network mode `full`
  (the default, with normal outbound access) or `none`, which denies it a route
  to the internet. The mode is fixed at creation and cannot be changed
  afterward. Restoring a sandbox always keeps its own mode. Forking a sandbox
  from a snapshot inherits the snapshot's mode by default, but an explicit
  `network` on the fork request overrides it.
- **Optional gVisor runtime:** An operator can configure `orcald` to run every
  sandbox under gVisor instead of the default container runtime, trading some
  compatibility and performance for a smaller kernel attack surface. The choice
  is made once, by the operator, for the whole deployment — a caller can never
  request weaker isolation than what was configured. gVisor also needs specific
  Docker daemon configuration to work correctly with snapshots and networking;
  `orcald` checks for it at startup and refuses to run rather than starting in a
  broken state.
- **Audit log:** Every action that changes state, and every request denied for
  being unauthenticated or out of scope, is recorded with who did it, what they
  did, what it affected, and the outcome. The log is queryable and filterable
  over the API.
- **Path validation on transfers:** Archives are rejected when an entry would
  escape the destination through traversal, an absolute path, or a symlink or
  hardlink target. Setuid and setgid bits are stripped from uploads.

### Known limitations

- Snapshots are Docker images on a single host. They cannot move between machines
  and there is no multi-node support.
- A snapshot captures the filesystem only. Process and memory state are not
  preserved, so a fork resumes from disk rather than mid-execution.
- A token's scope limits which operations it may perform, not which sandboxes it
  may perform them on. A token that can manage sandboxes can manage every
  sandbox on the host, not only ones it created.
- The published container image runs as root, and the daemon holds the Docker
  socket, which is root-equivalent on the host.
