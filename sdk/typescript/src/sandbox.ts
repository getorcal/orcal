import type { Transport } from "./transport.js";
import type { Exec as ExecModel, FileInfo, FileList, Sandbox as SandboxModel, Snapshot as SnapshotModel } from "./types.js";
import { Snapshot } from "./snapshot.js";
import { collectExec, ExecStream, type ExecOptions, type ExecResult } from "./exec.js";
import { paginate } from "./pagination.js";

export class FileListing extends Array<FileInfo> {
  truncated = false;

  static get [Symbol.species](): ArrayConstructor {
    return Array;
  }
}

export class SandboxFiles {
  constructor(
    private readonly transport: Transport,
    private readonly ref: string,
  ) {}

  private path(suffix = ""): string {
    return `/v1/sandboxes/${this.transport.escape(this.ref)}/files${suffix}`;
  }

  private archive(): string {
    return `/v1/sandboxes/${this.transport.escape(this.ref)}/archive`;
  }

  async read(path: string): Promise<Uint8Array> {
    const response = await this.transport.request("GET", this.path(), { params: { path } });
    return new Uint8Array(await response.arrayBuffer());
  }

  async write(path: string, content: string | Uint8Array): Promise<void> {
    const raw = typeof content === "string" ? new TextEncoder().encode(content) : content;
    await this.transport.request("PUT", this.path(), { params: { path }, raw });
  }

  async stat(path: string): Promise<FileInfo> {
    const response = await this.transport.request("GET", this.path("/stat"), { params: { path } });
    return (await response.json()) as FileInfo;
  }

  async list(path: string): Promise<FileListing> {
    const response = await this.transport.request("GET", this.path("/list"), { params: { path } });
    const body = (await response.json()) as FileList;
    const listing = new FileListing();
    for (const item of body.items ?? []) listing.push(item);
    listing.truncated = Boolean(body.truncated);
    return listing;
  }

  async download(path: string): Promise<Uint8Array> {
    const response = await this.transport.request("GET", this.archive(), { params: { path } });
    return new Uint8Array(await response.arrayBuffer());
  }

  async upload(path: string, tar: Uint8Array): Promise<void> {
    await this.transport.request("PUT", this.archive(), { params: { path }, raw: tar });
  }
}

export class Sandbox {
  readonly files: SandboxFiles;
  forkFactory: (opts: Record<string, unknown>) => Promise<Sandbox> = () => {
    throw new Error("fork factory not wired");
  };

  constructor(
    private readonly transport: Transport,
    public raw: SandboxModel,
  ) {
    this.files = new SandboxFiles(transport, raw.id);
  }

  get id(): string {
    return this.raw.id;
  }

  get name(): string | undefined {
    return this.raw.name;
  }

  get state(): string {
    return this.raw.state;
  }

  get network(): string | undefined {
    return this.raw.network;
  }

  get ociRuntime(): string | undefined {
    return this.raw.oci_runtime;
  }

  private ref(): string {
    return this.transport.escape(this.raw.id);
  }

  private async replace(response: Response): Promise<this> {
    this.raw = (await response.json()) as SandboxModel;
    return this;
  }

  async refresh(): Promise<this> {
    return this.replace(await this.transport.request("GET", `/v1/sandboxes/${this.ref()}`));
  }

  async start(): Promise<this> {
    return this.replace(await this.transport.request("POST", `/v1/sandboxes/${this.ref()}/start`));
  }

  async stop(): Promise<this> {
    return this.replace(await this.transport.request("POST", `/v1/sandboxes/${this.ref()}/stop`));
  }

  async destroy(): Promise<this> {
    return this.replace(await this.transport.request("DELETE", `/v1/sandboxes/${this.ref()}`));
  }

  async snapshot(options: { name?: string } = {}): Promise<Snapshot> {
    const response = await this.transport.request("POST", `/v1/sandboxes/${this.ref()}/snapshots`, { body: options });
    return new Snapshot(this.transport, (await response.json()) as SnapshotModel, this.forkFactory);
  }

  async restore(snapshot: Snapshot | string): Promise<this> {
    const ref = typeof snapshot === "string" ? snapshot : snapshot.id;
    return this.replace(
      await this.transport.request("POST", `/v1/sandboxes/${this.ref()}/restore`, { body: { snapshot: ref } }),
    );
  }

  async exec(command: string | string[], options?: ExecOptions & { stream?: false }): Promise<ExecResult>;
  async exec(command: string | string[], options: ExecOptions & { stream: true }): Promise<ExecStream>;
  async exec(
    command: string | string[],
    options: ExecOptions & { stream?: boolean } = {},
  ): Promise<ExecResult | ExecStream> {
    const argv = typeof command === "string" ? ["sh", "-c", command] : [...command];
    const body: Record<string, unknown> = { command: argv };
    if (options.env) body.env = options.env;
    if (options.workingDir) body.working_dir = options.workingDir;
    if (options.user) body.user = options.user;
    const response = await this.transport.request("POST", `/v1/sandboxes/${this.ref()}/execs`, { body });
    const created = (await response.json()) as ExecModel;
    if (options.stream) return new ExecStream(this.transport, created.id);
    return collectExec(this.transport, created);
  }

  execs(filters: Record<string, string | number | undefined> = {}): AsyncGenerator<ExecModel> {
    return paginate(this.transport, `/v1/sandboxes/${this.ref()}/execs`, filters, (item: ExecModel) => item);
  }

  snapshots(filters: Record<string, string | number | undefined> = {}): AsyncGenerator<Snapshot> {
    return paginate(this.transport, `/v1/sandboxes/${this.ref()}/snapshots`, filters, (item: SnapshotModel) =>
      new Snapshot(this.transport, item, this.forkFactory),
    );
  }
}
