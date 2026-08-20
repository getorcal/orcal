# @orcal/sdk

TypeScript client for [Orcal](https://github.com/getorcal/orcal), self-hosted sandbox infrastructure.

```bash
npm install @orcal/sdk
```

```ts
import { Orcal } from "@orcal/sdk";

const client = new Orcal({ url: "http://localhost:8080", token: "orcal_..." });

await client.withSandbox({ image: "python:3.12-slim" }, async (sb) => {
  await sb.files.write("/app/main.py", "print('hello')");

  const result = await sb.exec("python /app/main.py");
  console.log(result.stdout, result.exitCode);

  const stream = await sb.exec("pip install requests", { stream: true });
  for await (const frame of stream) process.stdout.write(frame.data);
  console.log(stream.exitCode);

  const snap = await sb.snapshot({ name: "deps-ready" });
  for (const variant of ["a", "b", "c"]) {
    await snap.withFork(async (branch) => {
      await branch.files.write("/app/v.txt", variant);
      console.log((await branch.exec("cat /app/v.txt")).stdout);
    });
  }
});
```

`withSandbox` and `withFork` destroy on exit, including when the body throws.
`client.sandbox(...)` returns a sandbox the caller owns and must `destroy()`.

A string command runs through a shell as `sh -c`; pass an array for exact argv.

`sb.files.list(path)` returns the entries as an array carrying a `truncated`
flag, which is `true` when the daemon capped the listing and the result is
incomplete.

Zero runtime dependencies. Requires Node 18 or newer.
