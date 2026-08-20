import { test } from "node:test";
import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { SSEParser } from "../src/sse.js";

const encode = (s: string) => new TextEncoder().encode(s);

test("parses a whole event", () => {
  const events = new SSEParser().feed(encode('event: output\ndata: {"offset":10,"stream":"stdout","data":"aGk="}\n\n'));
  assert.equal(events.length, 1);
  assert.equal(events[0][0], "output");
  assert.equal(events[0][1].offset, 10);
});

test("parses an event split across two reads", () => {
  const parser = new SSEParser();
  assert.deepEqual(parser.feed(encode('event: output\ndata: {"offset":1,"stre')), []);
  const events = parser.feed(encode('am":"stdout","data":"aGk="}\n\n'));
  assert.equal(events.length, 1);
  assert.equal(events[0][1].stream, "stdout");
});

test("parses two events in one read", () => {
  const events = new SSEParser().feed(
    encode('event: gap\ndata: {"offset":5}\n\nevent: exit\ndata: {"state":"exited","exit_code":0,"truncated":false}\n\n'),
  );
  assert.deepEqual(
    events.map((e) => e[0]),
    ["gap", "exit"],
  );
  assert.equal(events[1][1].exit_code, 0);
});

test("ignores comment keepalives", () => {
  const events = new SSEParser().feed(encode(': keepalive\n\nevent: gap\ndata: {"offset":2}\n\n'));
  assert.deepEqual(
    events.map((e) => e[0]),
    ["gap"],
  );
});

test("an incomplete trailing event is not emitted", () => {
  assert.deepEqual(new SSEParser().feed(encode('event: output\ndata: {"offset":1}')), []);
});

test("output event data field is base64 and decodes to the original bytes", () => {
  const original = "hello orcal";
  const payload = Buffer.from(original, "utf8").toString("base64");
  const events = new SSEParser().feed(
    encode(`event: output\ndata: {"offset":0,"stream":"stdout","data":"${payload}"}\n\n`),
  );
  assert.equal(events.length, 1);
  const data = events[0][1].data;
  assert.equal(typeof data, "string");
  assert.equal(Buffer.from(data as string, "base64").toString("utf8"), original);
});

test("gap event carries only its offset field", () => {
  const events = new SSEParser().feed(encode('event: gap\ndata: {"offset":42}\n\n'));
  assert.equal(events.length, 1);
  assert.deepEqual(events[0], ["gap", { offset: 42 }]);
});

test("exit is the terminal event carrying state, exit_code, and truncated", () => {
  const events = new SSEParser().feed(encode('event: exit\ndata: {"state":"exited","exit_code":7,"truncated":true}\n\n'));
  assert.equal(events.length, 1);
  assert.deepEqual(events[0], ["exit", { state: "exited", exit_code: 7, truncated: true }]);
});

test("a frame split exactly between the two blank-line newlines is not emitted early", () => {
  const parser = new SSEParser();
  const beforeSecondNewline = parser.feed(encode('event: gap\ndata: {"offset":9}\n'));
  assert.deepEqual(beforeSecondNewline, []);
  const afterSecondNewline = parser.feed(encode("\n"));
  assert.equal(afterSecondNewline.length, 1);
  assert.deepEqual(afterSecondNewline[0], ["gap", { offset: 9 }]);
});

test("a frame split inside the data payload at an arbitrary byte is reassembled", () => {
  const parser = new SSEParser();
  const whole = 'event: output\ndata: {"offset":3,"stream":"stderr","data":"YmFkIG5ld3M="}\n\n';
  const bytes = encode(whole);
  const cut = whole.indexOf('"stream"') + 3;
  const first = parser.feed(bytes.slice(0, cut));
  assert.deepEqual(first, []);
  const events = parser.feed(bytes.slice(cut));
  assert.equal(events.length, 1);
  assert.equal(events[0][1].stream, "stderr");
});

test("a multi-byte utf-8 character split mid-sequence across two feeds decodes correctly", () => {
  const parser = new SSEParser();
  const emoji = "\u{1F389}";
  const prefix = 'event: gap\ndata: {"offset":1,"note":"';
  const text = `${prefix}${emoji}"}\n\n`;
  const bytes = encode(text);
  const cut = encode(prefix).length + 2;
  const first = parser.feed(bytes.slice(0, cut));
  assert.deepEqual(first, []);
  const events = parser.feed(bytes.slice(cut));
  assert.equal(events.length, 1);
  assert.equal(events[0][1].note, emoji);
});

test("consecutive events do not leak fields from one block into the next", () => {
  const parser = new SSEParser();
  const events = [
    ...parser.feed(encode('event: gap\ndata: {"offset":1}\n\n')),
    ...parser.feed(encode('event: output\ndata: {"offset":2,"stream":"stdout","data":"aGk="}\n\n')),
    ...parser.feed(encode('event: gap\ndata: {"offset":3}\n\n')),
  ];
  assert.deepEqual(
    events.map((e) => e[0]),
    ["gap", "output", "gap"],
  );
  assert.deepEqual(events[0][1], { offset: 1 });
  assert.deepEqual(events[1][1], { offset: 2, stream: "stdout", data: "aGk=" });
  assert.deepEqual(events[2][1], { offset: 3 });
});

test("interleaved gap and exit blocks in a single feed do not merge data lines", () => {
  const events = new SSEParser().feed(
    encode(
      'event: gap\ndata: {"offset":1}\n\nevent: exit\ndata: {"state":"exited","exit_code":1,"truncated":false}\n\nevent: gap\ndata: {"offset":2}\n\n',
    ),
  );
  assert.deepEqual(
    events.map((e) => e[0]),
    ["gap", "exit", "gap"],
  );
  assert.deepEqual(events[0][1], { offset: 1 });
  assert.deepEqual(events[1][1], { state: "exited", exit_code: 1, truncated: false });
  assert.deepEqual(events[2][1], { offset: 2 });
});
