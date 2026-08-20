# Security

This document describes Orcal's security model as it actually exists: what is enforced, what
each boundary does and does not guarantee, and every gap the project has knowingly accepted.
It is written for operators deciding how to deploy `orcald` and for anyone evaluating whether
Orcal is suitable for a given workload.

## Authentication

Every request other than `GET /v1/healthz` and `GET /v1/version` requires a bearer token in the
`Authorization` header. There is no configuration flag or environment variable that disables
this check; a caller with no token, or an invalid one, always receives `401 Unauthorized`.

Tokens are opaque, randomly generated strings. `orcald` never stores the plaintext: only a
SHA-256 hash and a short prefix (used to identify a token in listings) are persisted. A token is
shown once, at creation time, and cannot be retrieved again — losing it means revoking it and
minting a new one.

On first start with no existing tokens, `orcald` generates a bootstrap token with every scope
and prints it once to stdout. An operator can instead pin the bootstrap token to a specific
value via `ORCAL_TOKEN`, which is useful for scripted deployments where the token needs to be
known in advance.

By default `orcald` binds to `127.0.0.1`, not `0.0.0.0`. Exposing it beyond localhost — to a
container network, a VM's external interface, or the public internet — is an operator decision
made through `ORCAL_ADDR`, and it should be paired with a reverse proxy that terminates TLS,
since `orcald` itself speaks plain HTTP.

## Scopes

Authorization is scope-based. A token carries one or more of nine scopes:

`sandboxes:read`, `sandboxes:write`, `exec`, `files:read`, `files:write`, `snapshots:read`,
`snapshots:write`, `audit:read`, and `admin` (token management: creating and revoking other
tokens). A tenth value, `*`, grants every scope at once, including ones added in the future.

Scopes are independent: holding `admin` does not imply `sandboxes:write`, and holding
`sandboxes:write` does not imply `exec`. Each route declares the single scope it requires, and a
request is authorized only if the calling token holds that scope directly or holds `*`. A token
can only mint a new token with scopes it itself holds — `admin` lets you create tokens, not
escalate them beyond your own grant.

### Gap: scopes are verb-scoped, not resource-scoped

A scope authorizes an operation, not a target. A token holding `sandboxes:write` can destroy,
stop, or restore *any* sandbox on the host, including ones created by a different token
belonging to a different agent. There is no ownership model tying a sandbox to the token that
created it, and no per-resource ACL.

The practical consequence is that "one token per agent" isolates *what an agent is allowed to
do*, not *which sandboxes it is allowed to do it to*. Two agents sharing an `orcald` instance,
each with its own `sandboxes:write` token, can each tear down the other's sandboxes. Operators
who need per-agent resource isolation should run separate `orcald` instances rather than relying
on scopes to partition a shared one.

## Network modes

A sandbox is created with a network mode that cannot be changed afterward — there is no
endpoint to move a running sandbox between modes. Restoring a sandbox always preserves that
sandbox's own mode: restore never changes it. Forking a sandbox from a snapshot is different —
the new sandbox inherits the snapshot's mode by default, but a `network` field on the fork
request overrides that default explicitly. A caller with `sandboxes:write` can therefore fork a
`none` snapshot with `"network": "full"` and get an internet-connected sandbox holding the
`none` sandbox's filesystem; that is deliberate, not a gap, but it means "created with `none`"
does not imply "every descendant stays `none`" unless the caller forking it says so.

`full`, the default, attaches the sandbox to a bridge network with a normal route to the
internet. `none` attaches the sandbox to a second, Docker-internal bridge network that carries
no default route off the host — a process inside cannot reach the internet through it. Both
networks additionally disable inter-container communication, so two sandboxes on the same
network, regardless of mode, cannot address each other directly.

What `none` guarantees is narrower than "no network access" in the abstract: it guarantees no
route to the internet through the container's network namespace. It says nothing about whether
a process that escapes the container's other boundaries — through a kernel exploit, or by
reaching the Docker daemon's socket — could still act on the network from outside that
namespace. Network isolation and container isolation are separate boundaries, and `none` only
addresses the first.

## Container runtime

By default, sandboxes run under `runc`, Docker's standard OCI runtime — a container, sharing the
host kernel, isolated by Linux namespaces and cgroups.

An operator can instead configure `ORCAL_CONTAINER_RUNTIME=runsc` to run sandboxes under
[gVisor](https://gvisor.dev), which intercepts a container's syscalls in a userspace sentry
process rather than passing them straight to the host kernel. This shrinks the kernel surface a
sandboxed process can reach — most syscalls never touch the real kernel at all — but it is a
**syscall-interception boundary, not a virtual machine**. gVisor is a real but smaller attack
surface, not a categorically different one, and it should not be described or relied on as
VM-grade isolation.

Three practical consequences follow from that:

- gVisor's syscall surface is incomplete. It does not implement every syscall the host kernel
  does, and some workloads — anything that depends on an unimplemented or partially-implemented
  syscall — break outright rather than merely running slower.
- Work that is syscall-heavy or I/O-heavy pays a measurable performance cost, because operations
  that would be a single kernel entry under `runc` are trapped and re-implemented in the sentry.
- gVisor is off by default. Selecting it requires the operator to have installed `runsc` on the
  host and registered it with the Docker daemon; Orcal does not bundle or install it.

### gVisor requires two daemon-level flags, or `orcald` refuses to start

gVisor's defaults are unsafe for how Orcal uses a sandbox, and `orcald` will not boot against a
`runsc` runtime that lacks either of the two `runtimeArgs` below:

- **`--overlay2=none`.** Without it, gVisor's default self-backed root overlay discards a
  container's writes before `docker commit` can see them, so a snapshot taken under the default
  configuration silently captures none of the sandbox's changes — the snapshot API reports
  success and produces an image with no data in it.
- **`--network=host`.** Without it, gVisor's default netstack gets no route on the isolated
  bridge network Orcal creates (the same one that carries `enable_icc=false`), so a sandbox has
  no network at all: no DNS, no outbound connections, nothing.

Both are set on the `runsc` entry in `/etc/docker/daemon.json`:

```json
{
  "runtimes": {
    "runsc": {
      "path": "/usr/bin/runsc",
      "runtimeArgs": ["--overlay2=none", "--network=host"]
    }
  }
}
```

At startup, `orcald` reads the daemon's own reported runtime configuration — a cheap
configuration check, not a container round trip — and if the resolved runtime is `runsc` and
either flag is absent, it refuses to start and names exactly which one is missing. A daemon that
boots and then silently loses every snapshot, or silently strands every sandbox with no network,
is a worse failure than one that never starts at all.

`--network=host` is a real trade, not a free fix, and it is worth stating plainly: it means
gVisor is no longer sandboxing the network stack. The container gets no network namespace or
netstack of its own; networking is handled by the host kernel exactly as it would be under
`runc`. gVisor's syscall-interception boundary, under this configuration, covers every syscall
category except networking.

### `/tmp` never survives a snapshot under gVisor

Independent of either flag above, gVisor always mounts a fresh `tmpfs` over `/tmp` inside every
container it starts. This is inherent to gVisor's default filesystem plumbing, not something
`--overlay2=none` changes: nothing written to `/tmp` survives a snapshot, a fork, or a restore,
in any `runsc` configuration. Anything a snapshot needs to capture — application state, files a
forked or restored sandbox should inherit — must live outside `/tmp`.

The runtime is selected once, at `orcald` startup, from `ORCAL_CONTAINER_RUNTIME`. It is not a
per-request or per-sandbox setting: there is no field on the create-sandbox API that lets a
caller pick a runtime, and the response's read-only `oci_runtime` field only reports what the
operator already configured. This is deliberate — a token holder can never request weaker
isolation than the operator chose, and cannot request gVisor on a host the operator did not
enable it on. If `ORCAL_CONTAINER_RUNTIME` names a runtime the Docker daemon does not have
registered, `orcald` refuses to start rather than silently falling back to `runc`; a daemon that
was told to isolate under `runsc` and quietly ran under `runc` instead would be advertising
isolation it was not providing.

## Audit log

`orcald` records an audit event for every mutating operation — creating, destroying, starting,
stopping, forking, or restoring a sandbox; creating an exec; creating or deleting a snapshot;
reading, writing, uploading, or downloading a file or archive; and creating or revoking a token
— plus every request denied for being unauthenticated or out of scope. Each event carries the
acting token's identity, the action, the affected resource, the request's status code, the
caller's remote address, and a request ID that ties it back to the corresponding log line. The
event never carries request or response bodies: an exec's command, a file's contents, an
environment variable's value, and a newly minted token's plaintext are all excluded by
construction, not by redaction. Events can be listed and filtered through `GET /v1/events`,
gated behind the `audit:read` scope, and are pruned on both an age and a count basis according
to `ORCAL_AUDIT_RETENTION_DAYS` and `ORCAL_AUDIT_MAX_EVENTS`.

Four gaps are worth naming explicitly:

- **`stat` and `list` are not audited.** Every other file operation is, but checking whether a
  path exists or listing a directory's contents leaves no trace in the audit log. An attacker
  who has files:read can enumerate a sandbox's filesystem for reconnaissance without that
  activity ever appearing in `GET /v1/events`.
- **Audit fails open.** The event is written after the response has already been sent back to
  the caller; if writing the event itself fails, the request still succeeds. This is a
  deliberate trade-off — an audit store outage should not become an availability outage for the
  product — but it means the audit log's completeness is not guaranteed under a failing audit
  store, only best-effort.
- **A panicking handler leaves no audit event at all.** Recovery from a panic happens outside
  the audit middleware, so a request that crashes its handler unwinds past the code that would
  have written the event. This is distinct from the fail-open case above, which covers a
  failed *insert*; here nothing is ever attempted.
- **An unauthenticated caller can crowd out real history.** Every rejected request is audited,
  including ones from a caller with no valid token at all, and there is no rate limiting on the
  API. Since pruning by count evicts the oldest events first, anyone who can reach `ORCAL_ADDR`
  can flood it with denied requests and push genuine history out of the retention window. In
  practice this is mitigated by the default loopback bind: it only matters once an operator
  exposes `orcald` beyond `127.0.0.1`.

## Container hardening

Every sandbox container is created with the same fixed configuration, regardless of image or
caller:

- All Linux capabilities are dropped (`CapDrop: ALL`); none are added back.
- `no-new-privileges` is set, so a process inside cannot gain privileges through a setuid or
  setgid binary that it did not already have.
- The container runs under Docker's default seccomp profile — no `unconfined` override — and is
  never started with `--privileged`.
- No host paths are bind-mounted into the container, and no container ports are published to
  the host.
- CPU, memory, and process-count limits (`pids_limit`) are always applied, sized from either the
  request or configured defaults.

Two gaps remain in this configuration:

- **The root filesystem is not mounted read-only.** A sandboxed process can modify its own
  container's filesystem freely; nothing in the hardening configuration constrains writes to
  the container's own root.
- **Disk usage is not limited on every host.** Per-container disk quotas require the `overlay2`
  storage driver on an `xfs` backing filesystem with project quotas enabled. Where that
  combination is not present, `orcald` logs a startup warning and starts anyway — a sandbox on
  such a host can fill the disk the Docker daemon is using, with no per-sandbox cap.

## The Docker socket, and what holding it means

`orcald` talks to Docker over its API socket to create, run, and destroy sandbox containers.
That socket is root-equivalent on the host: anything that can issue commands over it can start
a container with an arbitrary bind mount, an arbitrary capability set, or `--privileged`, which
is a direct path to host root regardless of what any individual sandbox's hardening looks like.

Scopes and the audit log constrain what a *token holder* can ask `orcald` to do through its API.
They do nothing to contain an attacker who reaches the Docker socket directly — by compromising
the host `orcald` runs on, or by escaping a container in a way that reaches the socket. The
socket is trusted at the level of the host, not at the level of an individual API caller, and
that is a boundary Orcal's authorization model does not attempt to cross.

Relatedly, the published container image (`deploy/Dockerfile`) creates a non-root `orcal` user
but never switches to it with `USER`, so the image runs as root by default. This is a hygiene
and container-scanner issue, not a new hole: since the same container already holds the
root-equivalent Docker socket, running its own process as non-root would not contain an attacker
who reaches that socket. Fixing it is deferred to the packaging work, not treated as a security
boundary in its own right.

## The boundary is a container, not a virtual machine

Under both supported runtimes, a sandbox is a container that shares the host's kernel — never a
virtual machine with its own kernel and hardware-virtualized boundary. The two runtimes differ
in how much of that shared kernel a sandboxed process can actually reach, and an operator should
know which one a given deployment is running before relying on it:

**Default runtime (`runc`).** Syscalls from the sandboxed process go straight to the host
kernel, filtered only by the dropped capabilities, seccomp's default profile, and
`no-new-privileges`. A kernel vulnerability reachable through an allowed syscall is reachable
from inside the sandbox. This is standard container isolation: strong against a process that
stays within its intended privileges, and only as strong as the host kernel against one that
finds a kernel bug to exploit.

**gVisor runtime (`runsc`), where configured.** Most syscalls are intercepted and reimplemented
in a userspace sentry process instead of reaching the host kernel directly, which meaningfully
shrinks the kernel surface an escape would need to target. It raises the bar relative to `runc`,
but it is still process-level isolation sharing one kernel underneath the sentry, still subject
to the same incomplete-syscall-surface and performance caveats described above, and still not a
substitute for the hardware-enforced boundary a virtual machine provides. Treat it as a stronger
container, not as VM-grade isolation. Networking is a specific, deliberate exception to all of
this: Orcal's required `--network=host` configuration (see above) means the sentry does not
intercept network syscalls at all, so gVisor's boundary here covers everything except
networking, which runs exactly as it does under `runc`.

Whichever runtime is configured, the Docker-socket gap above applies identically: neither
runtime changes what holding `orcald`'s socket means.
