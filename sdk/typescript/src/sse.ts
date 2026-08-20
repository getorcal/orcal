export type SSEEvent = [string, Record<string, unknown>];

export class SSEParser {
  private buffer = "";
  private readonly decoder = new TextDecoder();

  feed(chunk: Uint8Array): SSEEvent[] {
    this.buffer += this.decoder.decode(chunk, { stream: true });
    const events: SSEEvent[] = [];
    let index = this.buffer.indexOf("\n\n");
    while (index !== -1) {
      const block = this.buffer.slice(0, index);
      this.buffer = this.buffer.slice(index + 2);
      const parsed = SSEParser.parseBlock(block);
      if (parsed) events.push(parsed);
      index = this.buffer.indexOf("\n\n");
    }
    return events;
  }

  private static parseBlock(block: string): SSEEvent | null {
    let name: string | null = null;
    const data: string[] = [];
    for (const line of block.split("\n")) {
      if (!line || line.startsWith(":")) continue;
      const separator = line.indexOf(":");
      const field = separator === -1 ? line : line.slice(0, separator);
      let value = separator === -1 ? "" : line.slice(separator + 1);
      if (value.startsWith(" ")) value = value.slice(1);
      if (field === "event") name = value;
      else if (field === "data") data.push(value);
    }
    if (name === null || data.length === 0) return null;
    try {
      return [name, JSON.parse(data.join("\n")) as Record<string, unknown>];
    } catch {
      return null;
    }
  }
}
