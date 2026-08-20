# orcal

Python client for [Orcal](https://github.com/getorcal/orcal), self-hosted sandbox infrastructure.

```bash
pip install orcal
```

```python
from orcal import Orcal

client = Orcal(url="http://localhost:8080", token="orcal_...")

with client.sandbox(image="python:3.12-slim") as sb:
    sb.files.write("/app/main.py", "print('hello')")

    result = sb.exec("python /app/main.py")
    print(result.stdout, result.exit_code)

    stream = sb.exec("pip install requests", stream=True)
    for frame in stream:
        print(frame.data.decode(), end="")
    print(stream.exit_code)

    snap = sb.snapshot(name="deps-ready")

for variant in ("a", "b", "c"):
    with snap.fork() as branch:
        branch.files.write("/app/v.txt", variant)
        print(branch.exec("cat /app/v.txt").stdout)
```

`with` destroys the sandbox on exit, including when the body raises. Without
`with`, the caller owns the lifetime and calls `destroy()`.

A string command runs through a shell as `sh -c`; pass a list for exact argv.

## Isolation

`client.sandbox(image=..., network="none")` creates a sandbox with no route off
its bridge. The mode is fixed at creation.

## Not yet supported

This client is synchronous. An `async` surface will follow once this one has
settled; until then, wrap calls in a thread executor.
