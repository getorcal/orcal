import type { Transport } from "./transport.js";

export async function* paginate<T, R>(
  transport: Transport,
  path: string,
  params: Record<string, string | number | undefined>,
  build: (item: T) => R,
): AsyncGenerator<R> {
  const query: Record<string, string | number | undefined> = { ...params };
  let sentCursor = query.cursor;
  for (;;) {
    const body = (await (await transport.request("GET", path, { params: query })).json()) as {
      items?: T[];
      next_cursor?: string;
    };
    for (const item of body.items ?? []) yield build(item);
    const cursor = body.next_cursor;
    if (!cursor || cursor === sentCursor) return;
    sentCursor = cursor;
    query.cursor = cursor;
  }
}
