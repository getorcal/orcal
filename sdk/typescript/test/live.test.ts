import { test, before } from "node:test";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { Orcal } from "../src/index.js";

const url = process.env.ORCAL_URL;
const token = process.env.ORCAL_TOKEN;
const image = process.env.ORCAL_TEST_IMAGE ?? "alpine:3.20";

if (!url || !token) {
  throw new Error(
    "ORCAL_URL and ORCAL_TOKEN must be set. These tests run against a real orcald and must fail " +
      "rather than skip when one is unreachable: a suite that skips quietly is a suite that never ran.",
  );
}

const client = new Orcal({ url, token });

before(async () => {
  await client.version();
});

test("readme example runs end to end", async () => {
  const snapshotId = await client.withSandbox({ image }, async (sb) => {
    await sb.files.write("/app/marker.txt", "original");
    const result = await sb.exec("cat /app/marker.txt");
    assert.equal(result.exitCode, 0);
    assert.equal(result.stdout.trim(), "original");

    const stream = await sb.exec("echo streamed", { stream: true });
    let seen = "";
    for await (const frame of stream) seen += new TextDecoder().decode(frame.data);
    assert.match(seen, /streamed/);
    assert.equal(stream.exitCode, 0);

    const snap = await sb.snapshot({ name: `v-${Date.now()}` });
    return snap.id;
  });

  const snap = await client.getSnapshot(snapshotId);
  try {
    await snap.withFork(async (branch) => {
      const forked = await branch.exec("cat /app/marker.txt");
      assert.equal(forked.exitCode, 0, "a fork must carry the parent snapshot's filesystem");
      assert.equal(forked.stdout.trim(), "original");
    });
  } finally {
    await snap.delete();
  }
});

test("files round trip binary", async () => {
  const payload = new Uint8Array(Array.from({ length: 256 }, (_, i) => i));
  await client.withSandbox({ image }, async (sb) => {
    await sb.files.write("/app/blob.bin", payload);
    assert.deepEqual(Array.from(await sb.files.read("/app/blob.bin")), Array.from(payload));
  });
});

test("exit code and stderr are reported", async () => {
  await client.withSandbox({ image }, async (sb) => {
    const result = await sb.exec("echo oops >&2; exit 7");
    assert.equal(result.exitCode, 7);
    assert.match(result.stderr, /oops/);
  });
});

test("a none-network sandbox reports its mode", async () => {
  await client.withSandbox({ image, network: "none" }, async (sb) => {
    assert.equal(sb.network, "none");
  });
});

test("the sandbox is destroyed when the body throws", async () => {
  let id = "";
  await assert.rejects(() =>
    client.withSandbox({ image }, async (sb) => {
      id = sb.id;
      throw new Error("boom");
    }),
  );
  const after = await client.getSandbox(id);
  assert.equal(after.state, "destroyed");
});

test("listing sandboxes paginates", async () => {
  await client.withSandbox({ image }, (a) =>
    client.withSandbox({ image }, async (b) => {
      const ids: string[] = [];
      for await (const sb of client.sandboxes()) ids.push(sb.id);
      assert.equal(new Set(ids).size, ids.length);
      assert.ok(ids.includes(a.id), "listing did not surface the sandbox created for this test");
      assert.ok(ids.includes(b.id), "listing did not surface the second sandbox created for this test");
    }),
  );
});

test("forks of the same snapshot are independently writable", async () => {
  const snapshotId = await client.withSandbox({ image }, async (sb) => {
    await sb.files.write("/app/marker.txt", "original");
    const snap = await sb.snapshot({ name: `v-${randomUUID().slice(0, 8)}` });
    return snap.id;
  });

  const snap = await client.getSnapshot(snapshotId);
  const decoder = new TextDecoder();
  try {
    await snap.withFork((forkA) =>
      snap.withFork(async (forkB) => {
        await forkA.files.write("/app/branch.txt", "a");
        await forkB.files.write("/app/branch.txt", "b");

        assert.equal(decoder.decode(await forkA.files.read("/app/branch.txt")), "a");
        assert.equal(decoder.decode(await forkB.files.read("/app/branch.txt")), "b");
        assert.equal(decoder.decode(await forkA.files.read("/app/marker.txt")), "original");
        assert.equal(decoder.decode(await forkB.files.read("/app/marker.txt")), "original");
      }),
    );
  } finally {
    await snap.delete();
  }
});
