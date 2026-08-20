import type { Transport } from "./transport.js";
import type { FileInfo, FileList, Sandbox as SandboxModel, Snapshot as SnapshotModel } from "./types.js";
import { Snapshot } from "./snapshot.js";

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
    await this.transport.request("PUT", this.path(), { params: { path }, raw: raw as BodyInit });
  }

  async stat(path: string): Promise<FileInfo> {
    const response = await this.transport.request("GET", this.path("/stat"), { params: { path } });
    return (await response.json()) as FileInfo;
  }

  async list(path: string): Promise<FileInfo[]> {
    const response = await this.transport.request("GET", this.path("/list"), { params: { path } });
    const body = (await response.json()) as FileList;
    return body.items ?? [];
  }

  async download(path: string): Promise<Uint8Array> {
    const response = await this.transport.request("GET", this.archive(), { params: { path } });
    return new Uint8Array(await response.arrayBuffer());
  }

  async upload(path: string, tar: Uint8Array): Promise<void> {
    await this.transport.request("PUT", this.archive(), { params: { path }, raw: tar as BodyInit });
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
}
