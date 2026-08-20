import { test } from "node:test";
import assert from "node:assert/strict";
import { deepEqual as looseDeepEqual } from "node:assert";
import { Orcal } from "../src/client.js";
import { Snapshot } from "../src/snapshot.js";
import type { Sandbox } from "../src/sandbox.js";
import type { Sandbox as SandboxModel } from "../src/types.js";

const sandboxPayload = (id = "sb-1", network: SandboxModel["network"] = "full"): SandboxModel => ({
  id,
  name: "demo",
  image: "alpine:3.20",
  state: "running",
  runtime: "docker",
  network,
  oci_runtime: "runc",
  resources: { cpu_millis: 1000, memory_bytes: 1, pids_limit: 512 },
  created_at: "2026-08-20T10:00:00Z",
  updated_at: "2026-08-20T10:00:00Z",
});

const snapshotPayload = (id = "snap-1", sandboxId = "sb-1") => ({
  id,
  name: "v1",
  sandbox_id: sandboxId,
  runtime_ref: "sha256:x",
  image: "alpine:3.20",
  size_bytes: 1,
  created_at: "2026-08-20T10:00:00Z",
});

function router(routes: Record<string, (init: RequestInit, url: URL) => Response>) {
  const calls: string[] = [];
  const targets: string[] = [];
  const impl = async (input: string | URL, init: RequestInit = {}) => {
    const url = new URL(String(input));
    const key = `${init.method ?? "GET"} ${url.pathname}`;
    calls.push(key);
    targets.push(`${key}${url.search}`);
    const handler = routes[key];
    if (!handler) {
      return new Response(JSON.stringify({ error: { code: "sandbox_not_found", message: key, details: {} } }), { status: 404 });
    }
    return handler(init, url);
  };
  return { impl: impl as unknown as typeof fetch, calls, targets };
}

const ok = (payload: unknown, status = 200) =>
  new Response(JSON.stringify(payload), { status, headers: { "content-type": "application/json" } });

const client = (impl: typeof fetch) => new Orcal({ url: "http://example.test", token: "orcal_secret", fetch: impl });

test("withSandbox destroys on the happy path", async () => {
  const { impl, calls } = router({
    "POST /v1/sandboxes": () => ok(sandboxPayload(), 201),
    "DELETE /v1/sandboxes/sb-1": () => ok(sandboxPayload()),
  });
  await client(impl).withSandbox({ image: "alpine:3.20" }, async (sb) => {
    assert.equal(sb.id, "sb-1");
  });
  assert.ok(calls.includes("DELETE /v1/sandboxes/sb-1"));
});

test("withSandbox destroys even when the body throws", async () => {
  const { impl, calls } = router({
    "POST /v1/sandboxes": () => ok(sandboxPayload(), 201),
    "DELETE /v1/sandboxes/sb-1": () => ok(sandboxPayload()),
  });
  await assert.rejects(
    () => client(impl).withSandbox({ image: "alpine:3.20" }, async () => { throw new Error("boom"); }),
    /boom/,
  );
  assert.ok(calls.includes("DELETE /v1/sandboxes/sb-1"));
});

const cleanupFailure = (message: string) =>
  new Response(JSON.stringify({ error: { code: "internal_error", message, details: {} } }), { status: 500 });

test("withSandbox propagates the original error rather than a cleanup error", async () => {
  const { impl } = router({
    "POST /v1/sandboxes": () => ok(sandboxPayload(), 201),
    "DELETE /v1/sandboxes/sb-1": () => cleanupFailure("cleanup blew up"),
  });
  let caught: unknown;
  try {
    await client(impl).withSandbox({ image: "alpine:3.20" }, async () => {
      throw new Error("original failure");
    });
    assert.fail("expected withSandbox to reject");
  } catch (error) {
    caught = error;
  }
  assert.ok(caught instanceof Error);
  assert.match(caught.message, /original failure/);
  assert.ok(caught.cause instanceof Error);
  assert.match((caught.cause as Error).message, /cleanup blew up/);
});

test("withSandbox surfaces a cleanup failure even when the body succeeds", async () => {
  const { impl } = router({
    "POST /v1/sandboxes": () => ok(sandboxPayload(), 201),
    "DELETE /v1/sandboxes/sb-1": () => cleanupFailure("cleanup blew up"),
  });
  await assert.rejects(
    () => client(impl).withSandbox({ image: "alpine:3.20" }, async (sb) => sb.id),
    /cleanup blew up/,
  );
});

const exhausted = () =>
  new Response(JSON.stringify({ error: { code: "resource_exhausted", message: "no room", details: {} } }), { status: 429 });

test("nested withSandbox destroys the first sandbox when the second creation fails", async () => {
  let created = 0;
  const { impl, calls } = router({
    "POST /v1/sandboxes": () => {
      created += 1;
      return created === 1 ? ok(sandboxPayload("sb-1"), 201) : exhausted();
    },
    "DELETE /v1/sandboxes/sb-1": () => ok({ ...sandboxPayload("sb-1"), state: "destroyed" }),
  });
  const c = client(impl);
  await assert.rejects(
    () => c.withSandbox({ image: "alpine:3.20" }, () => c.withSandbox({ image: "alpine:3.20" }, async () => undefined)),
    /no room/,
  );
  assert.ok(
    calls.includes("DELETE /v1/sandboxes/sb-1"),
    "the first sandbox must be destroyed when the second creation fails",
  );
});

test("nested withFork destroys the first fork when the second fork fails", async () => {
  let forked = 0;
  const { impl, calls } = router({
    "POST /v1/sandboxes": () => {
      forked += 1;
      return forked === 1 ? ok(sandboxPayload("sb-2"), 201) : exhausted();
    },
    "DELETE /v1/sandboxes/sb-2": () => ok({ ...sandboxPayload("sb-2"), state: "destroyed" }),
  });
  const c = client(impl);
  const snapshot = new Snapshot(c.transport, snapshotPayload(), (opts) => c.sandbox(opts as never));
  await assert.rejects(
    () => snapshot.withFork(() => snapshot.withFork(async () => undefined)),
    /no room/,
  );
  assert.ok(calls.includes("DELETE /v1/sandboxes/sb-2"), "the first fork must be destroyed when the second fork fails");
});

test("nested withSandbox destroys the first sandbox even when the second cleanup fails", async () => {
  let created = 0;
  const { impl, calls } = router({
    "POST /v1/sandboxes": () => {
      created += 1;
      return created === 1 ? ok(sandboxPayload("sb-1"), 201) : ok(sandboxPayload("sb-2"), 201);
    },
    "DELETE /v1/sandboxes/sb-2": () => cleanupFailure("second cleanup blew up"),
    "DELETE /v1/sandboxes/sb-1": () => ok({ ...sandboxPayload("sb-1"), state: "destroyed" }),
  });
  const c = client(impl);
  await assert.rejects(
    () => c.withSandbox({ image: "alpine:3.20" }, () => c.withSandbox({ image: "alpine:3.20" }, async () => undefined)),
    /second cleanup blew up/,
  );
  assert.ok(
    calls.includes("DELETE /v1/sandboxes/sb-1"),
    "a failing inner cleanup must not strand the outer sandbox",
  );
});

test("network none is sent and reported", async () => {
  let body = "";
  const { impl } = router({
    "POST /v1/sandboxes": (init) => {
      body = String(init.body);
      return ok(sandboxPayload("sb-1", "none"), 201);
    },
  });
  const sb = await client(impl).sandbox({ image: "alpine:3.20", network: "none" });
  assert.match(body, /"network":"none"/);
  assert.equal(sb.network, "none");
});

test("sandbox create sends only the options that were supplied", async () => {
  let body = "";
  const { impl } = router({
    "POST /v1/sandboxes": (init) => {
      body = String(init.body);
      return ok(sandboxPayload(), 201);
    },
  });
  await client(impl).sandbox({ image: "alpine:3.20", cpuMillis: 500, memoryBytes: 100, pidsLimit: 32, env: { A: "1" }, labels: { team: "x" } });
  const parsed = JSON.parse(body);
  assert.deepEqual(parsed, {
    image: "alpine:3.20",
    cpu_millis: 500,
    memory_bytes: 100,
    pids_limit: 32,
    env: { A: "1" },
    labels: { team: "x" },
  });
});

test("files write accepts a string and bytes, read returns bytes", async () => {
  const bodies: string[] = [];
  const { impl } = router({
    "POST /v1/sandboxes": () => ok(sandboxPayload(), 201),
    "PUT /v1/sandboxes/sb-1/files": (init) => {
      bodies.push(new TextDecoder().decode(init.body as Uint8Array));
      return new Response(null, { status: 204 });
    },
    "GET /v1/sandboxes/sb-1/files": () => new Response(new Uint8Array([0, 1, 2]), { status: 200 }),
  });
  const sb = await client(impl).sandbox({ image: "alpine:3.20" });
  await sb.files.write("/app/a.txt", "hello");
  await sb.files.write("/app/b.txt", new TextEncoder().encode("hello"));
  assert.deepEqual(bodies, ["hello", "hello"]);
  assert.deepEqual(Array.from(await sb.files.read("/app/a.bin")), [0, 1, 2]);
});

test("snapshot then fork posts the snapshot reference", async () => {
  let forkBody = "";
  const { impl } = router({
    "POST /v1/sandboxes": (init) => {
      forkBody = String(init.body);
      return ok(sandboxPayload("sb-2"), 201);
    },
    "POST /v1/sandboxes/sb-1/snapshots": () => ok(snapshotPayload(), 201),
  });
  const c = client(impl);
  const sb = c._fromPayload(sandboxPayload());
  const snap = await sb.snapshot({ name: "v1" });
  assert.equal(snap.name, "v1");
  const forked = await snap.fork();
  assert.equal(forked.id, "sb-2");
  assert.match(forkBody, /snap-1/);
});

test("a ref is escaped in the path", async () => {
  let seen = "";
  const impl = (async (input: string | URL) => {
    seen = new URL(String(input)).pathname;
    return ok(sandboxPayload());
  }) as unknown as typeof fetch;
  await client(impl).getSandbox("a/b");
  assert.equal(seen, "/v1/sandboxes/a%2Fb");
});

test("getSandbox issues a GET against the sandbox resource", async () => {
  const { impl, calls } = router({
    "GET /v1/sandboxes/sb-1": () => ok(sandboxPayload()),
  });
  const sb = await client(impl).getSandbox("sb-1");
  assert.equal(sb.id, "sb-1");
  assert.equal(sb.state, "running");
  assert.equal(sb.ociRuntime, "runc");
  assert.equal(sb.raw.image, "alpine:3.20");
  assert.deepEqual(calls, ["GET /v1/sandboxes/sb-1"]);
});

test("getSnapshot issues a GET against the snapshot resource", async () => {
  const { impl, calls } = router({
    "GET /v1/snapshots/snap-1": () => ok(snapshotPayload()),
  });
  const snap = await client(impl).getSnapshot("snap-1");
  assert.equal(snap.id, "snap-1");
  assert.deepEqual(calls, ["GET /v1/snapshots/snap-1"]);
});

test("getSnapshot escapes its ref in the path", async () => {
  let seen = "";
  const impl = (async (input: string | URL) => {
    seen = new URL(String(input)).pathname;
    return ok(snapshotPayload());
  }) as unknown as typeof fetch;
  await client(impl).getSnapshot("a/b");
  assert.equal(seen, "/v1/snapshots/a%2Fb");
});

test("healthz issues a GET against the healthz endpoint", async () => {
  const { impl, calls } = router({
    "GET /v1/healthz": () => ok({ status: "ok" }),
  });
  const result = await client(impl).healthz();
  assert.equal(result.status, "ok");
  assert.deepEqual(calls, ["GET /v1/healthz"]);
});

test("version issues a GET against the version endpoint", async () => {
  const { impl, calls } = router({
    "GET /v1/version": () => ok({ version: "1.2.3", api_versions: ["v1"] }),
  });
  const result = await client(impl).version();
  assert.equal(result.version, "1.2.3");
  assert.deepEqual(calls, ["GET /v1/version"]);
});

test("createToken posts name, scopes, and expiry when supplied", async () => {
  let body = "";
  const { impl, calls } = router({
    "POST /v1/tokens": (init) => {
      body = String(init.body);
      return ok(
        {
          id: "tok-1",
          name: "ci",
          prefix: "orcal_",
          scopes: ["admin"],
          created_at: "2026-08-20T10:00:00Z",
          token: "orcal_secretvalue",
        },
        201,
      );
    },
  });
  const created = await client(impl).createToken("ci", ["admin"], 3600);
  assert.equal(created.token, "orcal_secretvalue");
  assert.deepEqual(JSON.parse(body), { name: "ci", scopes: ["admin"], expires_in_seconds: 3600 });
  assert.deepEqual(calls, ["POST /v1/tokens"]);
});

test("createToken omits expires_in_seconds when not supplied", async () => {
  let body = "";
  const { impl } = router({
    "POST /v1/tokens": (init) => {
      body = String(init.body);
      return ok({ id: "tok-1", name: "ci", prefix: "orcal_", scopes: ["admin"], created_at: "2026-08-20T10:00:00Z", token: "x" }, 201);
    },
  });
  await client(impl).createToken("ci", ["admin"]);
  assert.deepEqual(JSON.parse(body), { name: "ci", scopes: ["admin"] });
});

test("createToken posts every scope it was given", async () => {
  let body = "";
  const { impl } = router({
    "POST /v1/tokens": (init) => {
      body = String(init.body);
      return ok({ id: "tok-1", name: "ci", prefix: "orcal_", scopes: ["admin"], created_at: "2026-08-20T10:00:00Z", token: "x" }, 201);
    },
  });
  await client(impl).createToken("ci", ["sandboxes:read", "exec", "files:write"]);
  assert.deepEqual(JSON.parse(body).scopes, ["sandboxes:read", "exec", "files:write"]);
});

test("listTokens issues a GET and unwraps items", async () => {
  const { impl, calls } = router({
    "GET /v1/tokens": () =>
      ok({ items: [{ id: "tok-1", name: "ci", prefix: "orcal_", scopes: ["admin"], created_at: "2026-08-20T10:00:00Z" }] }),
  });
  const tokens = await client(impl).listTokens();
  assert.equal(tokens.length, 1);
  assert.equal(tokens[0].id, "tok-1");
  assert.deepEqual(calls, ["GET /v1/tokens"]);
});

test("listTokens returns an empty array when items is absent", async () => {
  const { impl } = router({
    "GET /v1/tokens": () => ok({}),
  });
  const tokens = await client(impl).listTokens();
  assert.deepEqual(tokens, []);
});

test("revokeToken issues a DELETE with the id escaped in the path", async () => {
  const { impl, calls } = router({
    "DELETE /v1/tokens/tok%2F1": () => new Response(null, { status: 204 }),
  });
  await client(impl).revokeToken("tok/1");
  assert.deepEqual(calls, ["DELETE /v1/tokens/tok%2F1"]);
});

test("sandbox start issues a POST against the start action", async () => {
  const { impl, calls } = router({
    "POST /v1/sandboxes/sb-1/start": () => ok(sandboxPayload("sb-1", "full")),
  });
  const sb = client(impl)._fromPayload(sandboxPayload());
  await sb.start();
  assert.deepEqual(calls, ["POST /v1/sandboxes/sb-1/start"]);
});

test("sandbox stop issues a POST against the stop action", async () => {
  const { impl, calls } = router({
    "POST /v1/sandboxes/sb-1/stop": () => ok({ ...sandboxPayload(), state: "stopped" }),
  });
  const sb = client(impl)._fromPayload(sandboxPayload());
  await sb.stop();
  assert.equal(sb.state, "stopped");
  assert.deepEqual(calls, ["POST /v1/sandboxes/sb-1/stop"]);
});

test("sandbox refresh issues a GET against the sandbox resource", async () => {
  const { impl, calls } = router({
    "GET /v1/sandboxes/sb-1": () => ok(sandboxPayload()),
  });
  const sb = client(impl)._fromPayload(sandboxPayload());
  await sb.refresh();
  assert.deepEqual(calls, ["GET /v1/sandboxes/sb-1"]);
});

test("sandbox destroy issues a DELETE against the sandbox resource", async () => {
  const { impl, calls } = router({
    "DELETE /v1/sandboxes/sb-1": () => ok({ ...sandboxPayload(), state: "destroyed" }),
  });
  const sb = client(impl)._fromPayload(sandboxPayload());
  await sb.destroy();
  assert.equal(sb.state, "destroyed");
  assert.deepEqual(calls, ["DELETE /v1/sandboxes/sb-1"]);
});

test("sandbox restore accepts a plain ref string and posts it", async () => {
  let body = "";
  const { impl, calls } = router({
    "POST /v1/sandboxes/sb-1/restore": (init) => {
      body = String(init.body);
      return ok(sandboxPayload());
    },
  });
  const c = client(impl);
  const sb = c._fromPayload(sandboxPayload());
  await sb.restore("snap-1");
  assert.deepEqual(JSON.parse(body), { snapshot: "snap-1" });
  assert.deepEqual(calls, ["POST /v1/sandboxes/sb-1/restore"]);
});

test("sandbox restore accepts a Snapshot object and posts its id", async () => {
  let body = "";
  const { impl } = router({
    "GET /v1/snapshots/snap-1": () => ok(snapshotPayload()),
    "POST /v1/sandboxes/sb-1/restore": (init) => {
      body = String(init.body);
      return ok(sandboxPayload());
    },
  });
  const c = client(impl);
  const sb = c._fromPayload(sandboxPayload());
  const snapshot = await c.getSnapshot("snap-1");
  await sb.restore(snapshot);
  assert.deepEqual(JSON.parse(body), { snapshot: "snap-1" });
});

test("sandbox files escape the sandbox ref in every path", async () => {
  const { impl, calls } = router({
    "GET /v1/sandboxes/a%2Fb/files": () => new Response(new Uint8Array([1]), { status: 200 }),
    "PUT /v1/sandboxes/a%2Fb/files": () => new Response(null, { status: 204 }),
    "GET /v1/sandboxes/a%2Fb/files/stat": () => ok({ name: "x", size: 1, mode: "0644", mtime: "2026-08-20T10:00:00Z", is_dir: false }),
    "GET /v1/sandboxes/a%2Fb/files/list": () => ok({ items: [], truncated: false }),
    "GET /v1/sandboxes/a%2Fb/archive": () => new Response(new Uint8Array([2]), { status: 200 }),
    "PUT /v1/sandboxes/a%2Fb/archive": () => new Response(null, { status: 204 }),
  });
  const sb = client(impl)._fromPayload(sandboxPayload("a/b"));
  await sb.files.read("/x");
  await sb.files.write("/x", "hi");
  await sb.files.stat("/x");
  await sb.files.list("/x");
  await sb.files.download("/x");
  await sb.files.upload("/x", new Uint8Array([1]));
  assert.deepEqual(calls, [
    "GET /v1/sandboxes/a%2Fb/files",
    "PUT /v1/sandboxes/a%2Fb/files",
    "GET /v1/sandboxes/a%2Fb/files/stat",
    "GET /v1/sandboxes/a%2Fb/files/list",
    "GET /v1/sandboxes/a%2Fb/archive",
    "PUT /v1/sandboxes/a%2Fb/archive",
  ]);
});

test("the file path reaches the request as an encoded query parameter", async () => {
  let search = "";
  const impl = (async (input: string | URL) => {
    search = new URL(String(input)).search;
    return new Response(new Uint8Array([1]), { status: 200 });
  }) as unknown as typeof fetch;
  const sb = client(impl)._fromPayload(sandboxPayload());
  await sb.files.read("/tmp/a b/../c.txt");
  assert.equal(search, "?path=%2Ftmp%2Fa+b%2F..%2Fc.txt");
});

test("files.stat returns a FileInfo", async () => {
  const { impl } = router({
    "GET /v1/sandboxes/sb-1/files/stat": () => ok({ name: "a.txt", size: 5, mode: "0644", mtime: "2026-08-20T10:00:00Z", is_dir: false }),
  });
  const sb = client(impl)._fromPayload(sandboxPayload());
  const info = await sb.files.stat("/a.txt");
  assert.equal(info.name, "a.txt");
  assert.equal(info.size, 5);
});

test("files.list unwraps items and returns empty when absent", async () => {
  const { impl } = router({
    "GET /v1/sandboxes/sb-1/files/list": () =>
      ok({ items: [{ name: "a.txt", size: 1, mode: "0644", mtime: "2026-08-20T10:00:00Z", is_dir: false }], truncated: false }),
  });
  const sb = client(impl)._fromPayload(sandboxPayload());
  const items = await sb.files.list("/");
  assert.equal(items.length, 1);
  assert.equal(items[0].name, "a.txt");
});

const fileEntry = (name: string) => ({ name, size: 1, mode: "0644", mtime: "2026-08-20T10:00:00Z", is_dir: false });

const listRoute = (payload: unknown) => router({ "GET /v1/sandboxes/sb-1/files/list": () => ok(payload) });

test("files.list reports a truncated listing rather than looking complete", async () => {
  const { impl } = listRoute({ items: [fileEntry("a.txt")], truncated: true });
  const sb = client(impl)._fromPayload(sandboxPayload());
  const listing = await sb.files.list("/");
  assert.equal(listing.truncated, true);
  assert.equal(listing.length, 1);
});

test("files.list reports a complete listing as complete", async () => {
  const { impl } = listRoute({ items: [fileEntry("a.txt")], truncated: false });
  const sb = client(impl)._fromPayload(sandboxPayload());
  assert.equal((await sb.files.list("/")).truncated, false);
});

test("files.list returns an empty listing when items is absent", async () => {
  const { impl } = listRoute({ truncated: false });
  const sb = client(impl)._fromPayload(sandboxPayload());
  const listing = await sb.files.list("/");
  assert.deepEqual(Array.from(listing), []);
  assert.equal(listing.truncated, false);
});

test("a file listing behaves like an array", async () => {
  const { impl } = listRoute({ items: [fileEntry("a.txt"), fileEntry("b.txt")], truncated: true });
  const sb = client(impl)._fromPayload(sandboxPayload());
  const listing = await sb.files.list("/");
  assert.ok(Array.isArray(listing));
  assert.deepEqual(
    listing.map((entry) => entry.name),
    ["a.txt", "b.txt"],
  );
  assert.deepEqual([...listing].length, 2);
  assert.equal(listing.filter((entry) => entry.name === "a.txt").length, 1);
});

test("files.download reads the archive endpoint and returns bytes", async () => {
  const { impl, calls } = router({
    "GET /v1/sandboxes/sb-1/archive": () => new Response(new Uint8Array([9, 8, 7]), { status: 200 }),
  });
  const sb = client(impl)._fromPayload(sandboxPayload());
  const data = await sb.files.download("/dir");
  assert.deepEqual(Array.from(data), [9, 8, 7]);
  assert.deepEqual(calls, ["GET /v1/sandboxes/sb-1/archive"]);
});

test("a file listing deep-equals the plain array of its entries", async () => {
  const entries = [fileEntry("a.txt"), fileEntry("b.txt")];
  const { impl } = listRoute({ items: entries, truncated: true });
  const sb = client(impl)._fromPayload(sandboxPayload());
  const listing = await sb.files.list("/");
  looseDeepEqual(listing, entries);
  assert.deepEqual(Object.keys(listing), ["0", "1"]);
  assert.equal(listing.truncated, true);
});

const fileOperations: Record<string, { route: string; respond: () => Response; call: (sb: Sandbox, path: string) => Promise<unknown> }> = {
  read: {
    route: "GET /v1/sandboxes/sb-1/files",
    respond: () => new Response(new Uint8Array([1]), { status: 200 }),
    call: (sb, path) => sb.files.read(path),
  },
  write: {
    route: "PUT /v1/sandboxes/sb-1/files",
    respond: () => new Response(null, { status: 204 }),
    call: (sb, path) => sb.files.write(path, "hi"),
  },
  stat: {
    route: "GET /v1/sandboxes/sb-1/files/stat",
    respond: () => ok(fileEntry("a.txt")),
    call: (sb, path) => sb.files.stat(path),
  },
  list: {
    route: "GET /v1/sandboxes/sb-1/files/list",
    respond: () => ok({ items: [], truncated: false }),
    call: (sb, path) => sb.files.list(path),
  },
  download: {
    route: "GET /v1/sandboxes/sb-1/archive",
    respond: () => new Response(new Uint8Array([1]), { status: 200 }),
    call: (sb, path) => sb.files.download(path),
  },
  upload: {
    route: "PUT /v1/sandboxes/sb-1/archive",
    respond: () => new Response(null, { status: 204 }),
    call: (sb, path) => sb.files.upload(path, new Uint8Array([1])),
  },
};

for (const [operation, { route, respond, call }] of Object.entries(fileOperations)) {
  test(`files.${operation} asks for the path it was given`, async () => {
    const { impl, targets } = router({ [route]: respond });
    const sb = client(impl)._fromPayload(sandboxPayload());
    await call(sb, "/app/config.json");
    assert.deepEqual(targets, [`${route}?path=%2Fapp%2Fconfig.json`]);
  });
}

test("files.upload puts raw bytes to the archive endpoint", async () => {
  let received: Uint8Array | undefined;
  const { impl, calls } = router({
    "PUT /v1/sandboxes/sb-1/archive": (init) => {
      received = init.body as Uint8Array;
      return new Response(null, { status: 204 });
    },
  });
  const sb = client(impl)._fromPayload(sandboxPayload());
  const tar = new Uint8Array([1, 2, 3]);
  await sb.files.upload("/dir", tar);
  assert.deepEqual(Array.from(received ?? []), [1, 2, 3]);
  assert.deepEqual(calls, ["PUT /v1/sandboxes/sb-1/archive"]);
});

test("snapshot delete issues a DELETE with the id escaped in the path", async () => {
  const { impl, calls } = router({
    "DELETE /v1/snapshots/snap%2F1": () => new Response(null, { status: 204 }),
  });
  const c = client(impl);
  const snapshot = new Snapshot(c.transport, snapshotPayload("snap/1"), () => {
    throw new Error("unused");
  });
  await snapshot.delete();
  assert.deepEqual(calls, ["DELETE /v1/snapshots/snap%2F1"]);
});

test("snapshot withFork destroys the forked sandbox on the happy path", async () => {
  const { impl, calls } = router({
    "POST /v1/sandboxes": () => ok(sandboxPayload("sb-2"), 201),
    "DELETE /v1/sandboxes/sb-2": () => ok({ ...sandboxPayload("sb-2"), state: "destroyed" }),
  });
  const c = client(impl);
  const snapshot = new Snapshot(c.transport, snapshotPayload(), (opts) => c.sandbox(opts as never));
  const result = await snapshot.withFork(async (forked) => {
    assert.equal(forked.id, "sb-2");
    return "done";
  });
  assert.equal(result, "done");
  assert.ok(calls.includes("DELETE /v1/sandboxes/sb-2"));
});

test("snapshot withFork destroys the forked sandbox even when the body throws", async () => {
  const { impl, calls } = router({
    "POST /v1/sandboxes": () => ok(sandboxPayload("sb-2"), 201),
    "DELETE /v1/sandboxes/sb-2": () => ok({ ...sandboxPayload("sb-2"), state: "destroyed" }),
  });
  const c = client(impl);
  const snapshot = new Snapshot(c.transport, snapshotPayload(), (opts) => c.sandbox(opts as never));
  await assert.rejects(
    () => snapshot.withFork(async () => { throw new Error("fork body failure"); }),
    /fork body failure/,
  );
  assert.ok(calls.includes("DELETE /v1/sandboxes/sb-2"));
});

test("snapshot withFork propagates the body's error rather than a cleanup error", async () => {
  const { impl } = router({
    "POST /v1/sandboxes": () => ok(sandboxPayload("sb-2"), 201),
    "DELETE /v1/sandboxes/sb-2": () => cleanupFailure("cleanup blew up"),
  });
  const c = client(impl);
  const snapshot = new Snapshot(c.transport, snapshotPayload(), (opts) => c.sandbox(opts as never));
  let caught: unknown;
  try {
    await snapshot.withFork(async () => {
      throw new Error("fork body failure");
    });
    assert.fail("expected withFork to reject");
  } catch (error) {
    caught = error;
  }
  assert.ok(caught instanceof Error);
  assert.match(caught.message, /fork body failure/);
  assert.ok(caught.cause instanceof Error);
  assert.match((caught.cause as Error).message, /cleanup blew up/);
});

test("snapshot withFork surfaces a cleanup failure even when the body succeeds", async () => {
  const { impl } = router({
    "POST /v1/sandboxes": () => ok(sandboxPayload("sb-2"), 201),
    "DELETE /v1/sandboxes/sb-2": () => cleanupFailure("cleanup blew up"),
  });
  const c = client(impl);
  const snapshot = new Snapshot(c.transport, snapshotPayload(), (opts) => c.sandbox(opts as never));
  await assert.rejects(
    () => snapshot.withFork(async (forked) => forked.id),
    /cleanup blew up/,
  );
});
