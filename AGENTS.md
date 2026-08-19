# AGENTS.md

Working notes for AI coding agents in this repository.

## When Work Counts As Finished

Finished means you watched it work. Until you have run `orcald` against a live Docker daemon and driven the change through its happy path, the work is not done, not ready, not good to go, and should not be described that way.

The reason is specific to this codebase. Nearly every test here runs against `internal/runtime/fake`, an in-memory stand-in for Docker. A stand-in cannot fail the way a container runtime fails, so it will happily pass over permission bits the kernel ignores, archive behavior that diverges from `docker cp`, containers that die a millisecond after start, and resources left stranded on the host. A setuid-stripping path in this repository was dead code for its entire existence while every unit test covering it reported success, because the fake never modeled the bit that matters. Treat your change as having the same defect until real Docker proves otherwise.

The gate before you report back:

1. `make lint` clean and `gofmt -l .` silent.
2. `go test ./... -count=1` and `go test -race ./... -count=1` green.
3. `make verify-generate` clean, whenever `spec/` or `internal/apigen/` moved.
4. `make test-docker` green, with the changed path covered by something in that suite.
5. `./bin/orcald` running locally, driven through at least one real end-to-end interaction with `./bin/orcal`.
6. No panic, clean JSON log, and `docker ps -a --filter label=orcal.managed` plus `docker images` back at their pre-run counts.

No Docker daemon within reach? Say that plainly and hand verification to the user. Skipping step 4 quietly and calling the work ready is the failure mode this section exists to prevent. A green test run is evidence, not proof.

### Which binaries to run

Verification uses `./bin/orcald` and `./bin/orcal` built from this checkout, and nothing else. Not `orcal` off `PATH`, not a copy unpacked from a release tarball, not an `orcald` container that was already running.

`make build` produces those two files; they are the only artifacts carrying your edit. `make up` rebuilds the Compose image, so anything started before your change is still executing the old code — pair it with `make down` every time you want Compose to reflect what you just wrote. Refer to the binaries by `./bin/orcal` from the repository root or by absolute path in every shell you touch; a bare `orcal` is never right during development.

One caution on the other side of that rule: when someone reports the bug is still there after your fix, the first hypothesis is that your fix is incomplete, not that they launched the wrong binary. Investigate the code. Ask before concluding it was a stale binary.

## Toolchain

Go 1.25. The `go` directive reads `go 1.25.0`, which is the floor promised to consumers; CI exercises both that exact floor and the newest 1.25 patch.

```bash
make build             # both binaries into bin/
make test              # go test ./...
make test-docker       # integration suite; needs a live Docker daemon
make lint              # golangci-lint, v2 schema; must report 0 issues
make fmt               # golangci-lint fmt
make generate          # regenerate internal/apigen from spec/openapi.yaml
make verify-generate   # fails if generated types drift from the spec
make up / down / logs  # docker compose in deploy/
```

`go mod tidy` is off limits. Dependencies pin the `go` directive floor and tidy rewrites it without saying so; `TestGoDirectiveMatchesTheBuildImage` in `internal/architecture_test.go` catches the move, and CI's generate job runs `git diff --exit-code go.mod go.sum` to catch the rest. Dependency changes are deliberate hand-edits to `go.mod`.

`internal/apigen/apigen.gen.go` is generated and never edited by hand. Change `spec/openapi.yaml`, run `make generate`. Contract-first is what makes the SDK and conformance work of later sub-projects possible at all, so the ordering is structural rather than cosmetic.

## Style

Run `gofmt` over everything before committing; `make fmt` applies the project's full formatter set, which is gofmt plus goimports with `local-prefixes` pointed at the module path.

No emojis anywhere: source, output, docs, commit messages. In prose, a double hyphen is never a dash — use an emdash sparingly or rewrite the sentence.

Flags are lowercase, and any flag needing more than one word uses kebab-case rather than camelCase. Environment variables are `ORCAL_`-prefixed `SCREAMING_SNAKE_CASE`, and wire JSON is `snake_case` as declared in `spec/openapi.yaml`. Go field names come from the generator; don't fight them.

Export as little as possible. A declaration goes public only when something outside its package uses it, which is most of why the codebase lives under `internal/`.

## Package Boundaries

`cmd/orcald/main.go` is the composition root: it wires stores, services, and the server, and holds no feature logic of its own.

`internal/api` owns HTTP and only HTTP — handlers, middleware, and the one error-mapping table. Domain rules do not live there. Conversely `internal/sandbox`, `internal/exec`, `internal/snapshot`, and `internal/files` own behavior and typed sentinel errors, and know nothing about status codes. `internal/runtime` defines the runtime abstraction, with `docker/` and `fake/` as its two implementations. `internal/store/sqlite` owns persistence and hands back domain types and domain sentinels, never a raw `sql.ErrNoRows`. `pkg/orcalclient` is the only public package, consumed by both the CLI and the integration suite, which is what keeps it honest.

One import rule is enforced mechanically: **`github.com/docker/...` may be imported only by `internal/runtime/docker`.** `internal/architecture_test.go` walks the graph and fails the build on any violation. The runtime interface derives from what the standard needs rather than what Docker happens to expose, and that test is the only thing stopping the standard from decaying into Docker's API with different nouns.

### Before adding a feature

Work through these in order, and define the contract first if any answer is fuzzy:

1. Which package owns the behavior?
2. What is the typed contract, and which sentinels does it produce?
3. Must the runtime interface change, or can a service derive this from methods that already exist?
4. Does it need persistence, and therefore a numbered migration?
5. What does it look like in `spec/openapi.yaml`, and what does that generate?
6. What can only be proven against real Docker, and which integration test proves it?

Question 3 defaults to **no**. Every method on `runtime.Runtime` is a method every future adapter has to implement forever. The entire file plane is six endpoints resting on three runtime methods, with everything else derived a layer up. Growing that interface is a position you argue for, not a shortcut you take.

### Adding an endpoint

Spec first, always: add the path, operation, and schemas to `spec/openapi.yaml`, then `make generate`. From there, write the handler in the owning file under `internal/api`, map any new sentinel in `internal/api/errors.go:classify`, add the client method in `pkg/orcalclient`, and add the CLI verb in `cmd/orcal` so that human and JSON output render from the same value.

`internal/api/contract_test.go` checks real responses against the document with kin-openapi, so a handler emitting a shape the spec never declared fails there rather than in review.

New error codes belong in the `ErrorBody.code` enum. That vocabulary is closed and deliberately small — extending it is a contract change that SDKs and the conformance suite will bind to.

## Runtime Configuration

`orcald` takes its entire configuration from `ORCAL_`-prefixed environment variables, parsed once at startup. `internal/config/config.go` is the authority on their names and defaults. There is no config file for the daemon.

Numeric variables must parse as integers above zero, and a malformed value aborts startup rather than falling back — a limit that silently reverts to a default is a limit nobody actually has.

Everything persistent sits under `ORCAL_DATA_DIR`: `orcal.db` for sandboxes, execs, snapshots, and settings, and `execs/` for captured output. Container filesystems are never mirrored into the database. The container is authoritative for its own contents, and a file table would be a cache that goes stale the instant an exec writes anything.

The CLI resolves its settings by precedence: flags (`--url`, `--token`, `--output`, `--config`), then `ORCAL_URL` and `ORCAL_TOKEN`, then `~/.config/orcal/config.yaml`, then built-in defaults.

## Security Posture

At present the API sits behind one bearer token, required everywhere except `GET /v1/healthz` and `GET /v1/version`, compared in constant time and stored as a SHA-256 hash in the `settings` table. With `ORCAL_TOKEN` unset, the daemon mints one on first boot, persists the hash, and prints the plaintext a single time. Nothing turns authentication off, because an open daemon that runs arbitrary code is a remote code execution service with a nicer name.

Hardening is applied at container creation in `internal/runtime/docker/harden.go` and asserted by `test/integration/harden_test.go`: every capability dropped, `no-new-privileges` set, Docker's default seccomp profile kept, memory capped with swap disabled, CPU and pids limits applied, no host mounts, no published ports, and a shared bridge network built with `enable_icc=false` so sandboxes cannot see one another.

Three things this project pointedly does not claim:

* `orcald` holds the Docker socket, which is root-equivalent on the host. Reaching the API means being able to do anything the daemon can, which is why authentication is mandatory and the default bind is loopback.
* Read-only rootfs is off, and disk usage goes unlimited unless the storage driver supports quotas. The daemon logs a startup warning where it cannot enforce them.
* The isolation boundary is a container, not a VM. No production-grade hostile-code guarantee is offered.

Changes here touch `harden.go` and its integration test in the same commit. A hardening setting with no test that goes red when you delete it is decoration.

## Go Patterns

### Errors

Domain packages export sentinels — `ErrNotFound`, `ErrNameTaken`, `ErrInvalidState`, `ErrResourceExhausted` — wrapped with `%w` and matched with `errors.Is`. Translation into an HTTP status and code happens in exactly one place, `internal/api/errors.go:classify`; handlers never assemble HTTP errors themselves. Arm order in that switch is load-bearing, since fork and restore wrap both snapshot and sandbox sentinels and the first matching arm wins.

The Docker adapter is the exception that proves the rule: `internal/runtime/docker/docker.go` and `files.go` format their causes with `%v` rather than `%w`, on purpose, so Docker's concrete types cannot escape the abstraction through `errors.As`. `errorlint` is disabled for those two files in `.golangci.yml`, and `TestTranslateDoesNotLeakUnclassifiedDockerErrorType` fails the moment somebody "corrects" it. Leave it alone.

Internal errors get logged with their cause and returned as a generic message plus a request id. Error strings from inside the daemon never reach a response body.

### Concurrency and lifetime

Work that must outlive its request context uses `context.WithoutCancel` rather than `context.Background()`, keeping values while dropping cancellation.

`sandbox.Service` serializes per-sandbox operations behind a keyed mutex, and any read-act-write sequence holds that lock for its whole duration. Note the trap: **`Create` drops its lock before returning, so the value it hands back is already stale.** Because `repo.Update` rewrites every column, updating from that stale struct can quietly resurrect a concurrently destroyed sandbox as `running` with a dead runtime id. Re-read under the lock before writing.

### Storage

Timestamps are TEXT in the fixed-width `timeFormat` constant from `internal/store/sqlite/sqlite.go`. `time.RFC3339Nano` is wrong here: it trims trailing zeros, so values sort incorrectly as strings and cursor pagination breaks.

Migrations are ordinal-numbered files under `internal/store/sqlite/migrations/`, tracked in `schema_migrations`. A shipped migration is immutable — add another one.

IDs are UUIDv7, which makes `ORDER BY id` chronological and cursor pagination a plain `WHERE id > ?`. Maps persist as JSON columns. Uniqueness that has to survive deletion uses a partial index such as `WHERE name != '' AND state != 'destroyed'`, since a plain unique index would burn the name permanently.

The database opens with `SetMaxOpenConns(1)`. SQLite allows a single writer, and serializing every connection trades throughput this daemon never needs for the absence of `SQLITE_BUSY`. Do not add code that assumes parallel writes.

### HTTP and streaming

A chunked request body reports `ContentLength == -1`, so gating on `r.ContentLength > 0` silently discards every chunked upload.

Resolve the target resource before buffering a body. Buffering first lets a request naming a sandbox that does not exist consume memory anyway, which any token holder can turn into a denial of service.

`internal/api/sse.go` type-asserts `http.Flusher` and abandons streaming when the assertion fails. Any middleware wrapping `http.ResponseWriter` therefore has to provide `Flush()` and `Unwrap() http.ResponseWriter`, or exec log streaming dies in production while the unit suite stays green.

Large payloads stream. `io.Pipe` paired with a counting reader is the established way to bound an upload without holding it in memory.

### Archives

File transfer rides Docker's archive API, the only mechanism that requires nothing from the image and works against a stopped container. Both properties carry weight and both are proven by integration tests — one against a stopped sandbox, one against a distroless image. Reimplementing any file operation as an `exec` forfeits both at once, so don't.

Two tar details that have already cost time here: POSIX permission bits in a tar header are not `fs.FileMode` bits, with setuid living at `0o4000` in the header and `0o40000000` in `fs.FileMode`, so code confusing the two compiles, runs, and accomplishes nothing. And a symlink's `Linkname` resolves relative to its own directory while a hardlink's names an archive member relative to the root, so validating both identically leaves an escape open.

## Testing

Name a test for the property it pins down rather than the function it calls. `TestWriteRefusesToOverwriteADirectory` survives a rename; `TestWrite3` tells a future reader nothing.

The fake runtime has to mirror Docker's semantics, refusals included. `internal/runtime/fake` exists so services can be exercised without a daemon, and a fake more permissive than the real thing devalues every test stacked on top of it. It refuses extraction into a missing destination because Docker refuses, and it preserves mode bits because Docker preserves them. Loosening the fake to make a test pass is backwards.

Write tests that are capable of failing. After adding one for a correctness or security property, break the property on purpose and confirm it goes red. This repository's history includes a permission strip that was dead code, a recursion filter incapable of detecting recursion, and a status assertion that never reached the status it claimed to assert — all under passing tests. Walking a table or a registry beats hand-enumerating cases, because the walk also covers whatever someone adds next year.

Integration tests live in `test/integration/` behind the `docker` build tag, clean up every container, image, and network they create, and must keep `go vet -tags docker ./...` passing alongside the untagged run. They also must fail rather than skip: the CI job greps its own output for the skip message and fails on sight, then fails again if nothing reported a pass. A suite that skips quietly is a suite that never ran.

## Commits

Short, lowercase, imperative, one line, conventional prefix:

```
feat: add archive upload and download
fix: strip setuid/setgid via posix bits, not fs.FileMode
test: add file plane integration suite
docs: add AGENTS.md
```

`feat` for a new user-facing capability, `fix` for wrong behavior corrected, `test` for coverage that changes no behavior, `docs` for documentation alone, `chore` for tooling, CI, and dependencies.

Commit once per finished piece of work — not per file, not per edit. Never land a knowingly broken state on the assumption a later commit repairs it. Pull request titles follow the same imperative style, without bracketed prefixes.

## CI

`.github/workflows/ci.yml` runs on pushes to `main` and on every pull request, as six independent jobs:

* **lint** — `make lint` plus a `gofmt -l .` check
* **generate** — `make verify-generate`, `go mod verify`, and `git diff --exit-code go.mod go.sum`
* **test** — three-way matrix: ubuntu on go `1.25.0` (the declared floor, so a red here means the promised minimum is a lie), ubuntu on `1.25.x`, macos on `1.25.x`, each running vet, the suite, and the race suite
* **integration** — the `docker`-tagged suite against the runner's daemon, with a 25 minute timeout, a skip guard, a pass guard, and a leaked-container check that runs even after failure
* **image** — builds `deploy/Dockerfile` and asserts both binaries exist and are executable inside it
* **vuln** — `govulncheck`, gating only on findings reachable from this project's own code, with accepted findings recorded in `.github/vuln-allowlist.txt` alongside written reasons; the job fails once an accepted finding gains a fix, so the allowlist expires itself

Go installs with `check-latest: true` deliberately. Runners cache a patch that can trail the newest by one release, which is enough to fail the vulnerability gate over an advisory that was already fixed.

A CI result belongs to exactly one commit. Never report work as passing on the strength of a stale, queued, cancelled, or partial run.

## Chasing Docker-Only Bugs

Bugs that surface only against real Docker are the expensive category here, and the fake cannot reproduce them by construction. Three approaches, cheapest first.

Drive the daemon by hand when the behavior is reachable through the API. It is the fastest loop available, and the daemon's JSON log on stdout beats guessing.

```bash
make build
ORCAL_DATA_DIR=/tmp/orcal-dev ./bin/orcald
./bin/orcal --url http://127.0.0.1:8080 --token <printed-token> create --image alpine:3.20
```

Once you can reproduce it by hand, pin it with a targeted integration test. Write that test before the fix, watch it fail, then fix — a test authored afterwards proves only that the code you just wrote does what you just wrote.

```bash
go test -tags docker ./test/integration/... -run TestSomething -v -count=1
```

When the daemon's view and the host's view disagree, go look at what Docker actually did. The usual culprits are a setting the SDK accepted and ignored, and a container that exited immediately after start.

```bash
docker ps -a --filter label=orcal.managed
docker inspect <container> | jq '.[0].HostConfig'
docker exec <container> sh -c '<probe>'
```

## Cleaning Up After Yourself

Every sandbox is a real container and every snapshot a real image, and both outlive the process that created them when code leaks them. CI fails on any container still wearing the `orcal.managed` label, and that check runs even when the suite itself failed.

Locally, compare counts across a run — they have to match:

```bash
docker ps -aq | wc -l && docker images -q | wc -l
make test-docker
docker ps -aq | wc -l && docker images -q | wc -l
```

A test registers cleanup with `t.Cleanup` at the moment it creates a resource, never at the end of the test body, so an intervening `t.Fatal` still cleans up. The daemon carries the same obligation at runtime: a failed snapshot insert deletes the image it just built, so no host resource outlives the row meant to point at it. Any new step that creates a host resource before its database row exists ships with its rollback in the same commit.

## Keeping Docs Honest

User-facing changes update the API contract in `spec/openapi.yaml` first, then the command help text in `cmd/orcal`, then the `## Unreleased` section of `CHANGELOG.md`, then this file when a convention, trap, or workflow rule moved.

`CHANGELOG.md` is public product copy, not a work log. Describe what a user can now observe, and leave refactors, CI changes, test work, commit hashes, and contributor names out of it. Work with no user-visible outcome needs no entry at all.

Never describe planned behavior as though it already ships. Leave it out, or say outright that it does not exist yet.

## Releases

A pushed `v*` tag triggers `.github/workflows/release.yml`, which verifies, cross-compiles `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64`, publishes a GitHub Release with checksums, and pushes a multi-arch image to `ghcr.io/getorcal/orcal` with a signed build-provenance attestation.

The release body comes from `CHANGELOG.md`, not from commit subjects. Before tagging, rename `## Unreleased` to the version, wrap that entry in `<!-- release:start -->` and `<!-- release:end -->`, and strip those markers from the previous entry. The workflow fails when the tag has no matching heading, when the marker count is not exactly one, or when the marked block is empty.

Every target cross-compiles without a C toolchain because `modernc.org/sqlite` is pure Go. Keep it that way — a cgo-requiring dependency would cost the entire release matrix.

## Repository

The canonical repository is `getorcal/orcal` on GitHub, and every URL, link, and reference uses that path. Licensed under MIT.

## Hard Nos

* `go mod tidy`
* Hand-editing `internal/apigen/apigen.gen.go`
* Importing `github.com/docker/...` outside `internal/runtime/docker`
* Adding a `runtime.Runtime` method a service could derive
* Implementing a file or filesystem operation as an `exec`
* Building HTTP status codes inside a handler instead of mapping the sentinel in `classify`
* Putting domain rules in `internal/api`, or HTTP concerns in a domain package
* Loosening the fake runtime past Docker's behavior to get a test passing
* Gating request handling on `r.ContentLength`
* Pushing, force-pushing, or creating tags without being asked
* Calling work ready without having run the daemon against real Docker
