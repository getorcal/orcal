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
- **Mandatory authentication:** Every endpoint except health and version requires
  a bearer token. There is no mode that disables it. A token is generated on
  first start and printed once. The default bind address is loopback.
- **Path validation on transfers:** Archives are rejected when an entry would
  escape the destination through traversal, an absolute path, or a symlink or
  hardlink target. Setuid and setgid bits are stripped from uploads.

### Known limitations

- Snapshots are Docker images on a single host. They cannot move between machines
  and there is no multi-node support.
- A snapshot captures the filesystem only. Process and memory state are not
  preserved, so a fork resumes from disk rather than mid-execution.
- One token grants full access to every operation. Scoped tokens, per-sandbox
  network policy, and an audit trail are in progress.
- The published container image runs as root, and the daemon holds the Docker
  socket, which is root-equivalent on the host.
