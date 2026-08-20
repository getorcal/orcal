import type { Transport } from "./transport.js";
import type { Snapshot as SnapshotModel } from "./types.js";
import type { Sandbox } from "./sandbox.js";
import { withCleanup } from "./cleanup.js";

export class Snapshot {
  constructor(
    private readonly transport: Transport,
    public raw: SnapshotModel,
    private readonly forkFactory: (opts: Record<string, unknown>) => Promise<Sandbox>,
  ) {}

  get id(): string {
    return this.raw.id;
  }

  get name(): string | undefined {
    return this.raw.name;
  }

  async delete(): Promise<void> {
    await this.transport.request("DELETE", `/v1/snapshots/${this.transport.escape(this.raw.id)}`);
  }

  async fork(options: Record<string, unknown> = {}): Promise<Sandbox> {
    return this.forkFactory({ ...options, snapshot: this.raw.id });
  }

  async withFork<T>(body: (sandbox: Sandbox) => Promise<T>, options: Record<string, unknown> = {}): Promise<T> {
    const sandbox = await this.fork(options);
    return withCleanup(
      () => body(sandbox),
      () => sandbox.destroy(),
    );
  }
}
