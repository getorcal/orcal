import { Transport, type TransportOptions } from "./transport.js";
import { Sandbox } from "./sandbox.js";
import { Snapshot } from "./snapshot.js";
import { withCleanup } from "./cleanup.js";
import { paginate } from "./pagination.js";
import type {
  CreatedToken,
  Event as EventModel,
  Exec as ExecModel,
  Sandbox as SandboxModel,
  Snapshot as SnapshotModel,
  Token,
  TokenList,
  Version,
} from "./types.js";

export interface CreateSandboxOptions {
  name?: string;
  image?: string;
  snapshot?: string;
  network?: string;
  cpuMillis?: number;
  memoryBytes?: number;
  pidsLimit?: number;
  env?: Record<string, string>;
  labels?: Record<string, string>;
}

const WIRE_KEYS: Record<string, string> = {
  name: "name",
  image: "image",
  snapshot: "snapshot",
  network: "network",
  cpuMillis: "cpu_millis",
  memoryBytes: "memory_bytes",
  pidsLimit: "pids_limit",
  env: "env",
  labels: "labels",
};

export class Orcal {
  readonly transport: Transport;

  constructor(options: TransportOptions) {
    this.transport = new Transport(options);
  }

  _fromPayload(payload: SandboxModel): Sandbox {
    const sandbox = new Sandbox(this.transport, payload);
    sandbox.forkFactory = (opts) => this.sandbox(opts as CreateSandboxOptions);
    return sandbox;
  }

  async version(): Promise<Version> {
    const response = await this.transport.request("GET", "/v1/version");
    return (await response.json()) as Version;
  }

  async healthz(): Promise<{ status: string }> {
    const response = await this.transport.request("GET", "/v1/healthz");
    return (await response.json()) as { status: string };
  }

  async sandbox(options: CreateSandboxOptions = {}): Promise<Sandbox> {
    const body: Record<string, unknown> = {};
    for (const [key, wire] of Object.entries(WIRE_KEYS)) {
      const value = (options as Record<string, unknown>)[key];
      if (value !== undefined) body[wire] = value;
    }
    const response = await this.transport.request("POST", "/v1/sandboxes", { body });
    return this._fromPayload((await response.json()) as SandboxModel);
  }

  async withSandbox<T>(options: CreateSandboxOptions, body: (sandbox: Sandbox) => Promise<T>): Promise<T> {
    const sandbox = await this.sandbox(options);
    return withCleanup(
      () => body(sandbox),
      () => sandbox.destroy(),
    );
  }

  async getSandbox(ref: string): Promise<Sandbox> {
    const response = await this.transport.request("GET", `/v1/sandboxes/${this.transport.escape(ref)}`);
    return this._fromPayload((await response.json()) as SandboxModel);
  }

  async getSnapshot(ref: string): Promise<Snapshot> {
    const response = await this.transport.request("GET", `/v1/snapshots/${this.transport.escape(ref)}`);
    return new Snapshot(this.transport, (await response.json()) as SnapshotModel, (opts) => this.sandbox(opts));
  }

  async createToken(name: string, scopes: string[], expiresInSeconds?: number): Promise<CreatedToken> {
    const body: Record<string, unknown> = { name, scopes };
    if (expiresInSeconds !== undefined) body.expires_in_seconds = expiresInSeconds;
    const response = await this.transport.request("POST", "/v1/tokens", { body });
    return (await response.json()) as CreatedToken;
  }

  async listTokens(): Promise<Token[]> {
    const response = await this.transport.request("GET", "/v1/tokens");
    const body = (await response.json()) as TokenList;
    return body.items ?? [];
  }

  async revokeToken(id: string): Promise<void> {
    await this.transport.request("DELETE", `/v1/tokens/${this.transport.escape(id)}`);
  }

  sandboxes(filters: Record<string, string | number | undefined> = {}): AsyncGenerator<Sandbox> {
    return paginate(this.transport, "/v1/sandboxes", filters, (item: SandboxModel) => this._fromPayload(item));
  }

  snapshots(filters: Record<string, string | number | undefined> = {}): AsyncGenerator<Snapshot> {
    return paginate(this.transport, "/v1/snapshots", filters, (item: SnapshotModel) =>
      new Snapshot(this.transport, item, (opts) => this.sandbox(opts)),
    );
  }

  events(filters: Record<string, string | number | undefined> = {}): AsyncGenerator<EventModel> {
    return paginate(this.transport, "/v1/events", filters, (item: EventModel) => item);
  }

  async getExec(id: string): Promise<ExecModel> {
    const response = await this.transport.request("GET", `/v1/execs/${this.transport.escape(id)}`);
    return (await response.json()) as ExecModel;
  }
}
