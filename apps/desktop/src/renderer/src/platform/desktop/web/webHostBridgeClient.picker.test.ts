import assert from "node:assert/strict";
import test from "node:test";
import { requestHostCapability } from "./webHostBridgeClient.ts";

async function withHost(run: (host: ReturnType<typeof fakeHost>) => Promise<void>) {
  const previous = globalThis.window;
  const host = fakeHost();
  Object.defineProperty(globalThis, "window", { configurable: true, value: host.window });
  try { await run(host); }
  finally { Object.defineProperty(globalThis, "window", { configurable: true, value: previous }); }
}

function fakeHost() {
  const listeners = new Map<string, Set<(event: any) => void>>();
  const timers = new Map<number, { callback: () => void; due: number }>();
  const requests: Array<{ id: string }> = [];
  let clock = 0;
  let nextTimer = 0;
  const parent = { postMessage(message: { id: string }) { requests.push(message); } };
  const window = {
    parent,
    location: { search: "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost" },
    addEventListener(type: string, listener: (event: any) => void) {
      if (!listeners.has(type)) listeners.set(type, new Set());
      listeners.get(type)!.add(listener);
    },
    removeEventListener(type: string, listener: (event: any) => void) { listeners.get(type)?.delete(listener); },
    setTimeout(callback: () => void, delay: number) {
      const id = ++nextTimer;
      timers.set(id, { callback, due: clock + delay });
      return id;
    },
    clearTimeout(id: number) { timers.delete(id); }
  };
  return {
    window, listeners, timers,
    advance(ms: number) {
      clock += ms;
      for (const [id, timer] of timers) if (timer.due <= clock) { timers.delete(id); timer.callback(); }
    },
    reply(type: string, fields = {}, envelope = {}) {
      const event = { source: parent, origin: "http://wails.localhost", ...envelope,
        data: { type, id: requests.at(-1)!.id, nonce: "nonce-1", ...fields } };
      for (const listener of listeners.get("message") ?? []) listener(event);
    },
    hide() { for (const listener of listeners.get("pagehide") ?? []) listener({}); }
  };
}

for (const capability of ["selectUploadFiles", "selectDirectory"]) {
  test(`${capability}: acceptance keeps a slow user selection alive and cleans listeners`, () => withHost(async host => {
    const request = requestHostCapability(capability);
    host.reply("tutti-host-request-accepted");
    host.reply("tutti-host-request-accepted");
    host.advance(61_000);
    const result = capability === "selectUploadFiles" ? ["C:\\项目 A\\说明.txt"] : { path: "C:\\项目 A" };
    host.reply("tutti-host-response", { result });
    assert.deepEqual(await request, result);
    assert.equal(host.listeners.get("message")?.size, 0);
    assert.equal(host.listeners.get("pagehide")?.size, 0);
    assert.equal(host.timers.size, 0);
  }));
}

test("picker: a final cancellation result does not require ACK", () => withHost(async host => {
  const request = requestHostCapability("selectUploadFiles");
  host.reply("tutti-host-response", { result: [] });
  assert.deepEqual(await request, []);
  assert.equal(host.timers.size, 0);
}));

test("picker: untrusted ACKs cannot suppress the acceptance deadline", () => withHost(async host => {
  const request = requestHostCapability("selectUploadFiles");
  const rejected = assert.rejects(request, { code: "host_request_timeout" });
  host.reply("tutti-host-request-accepted", { nonce: "wrong" });
  host.reply("tutti-host-request-accepted", {}, { source: {} });
  host.reply("tutti-host-request-accepted", {}, { origin: "http://wrong" });
  host.reply("tutti-host-request-accepted", { id: "old" });
  host.advance(5_000);
  await rejected;
}));

test("picker: pagehide cancels an accepted request and drops late results", () => withHost(async host => {
  const request = requestHostCapability("selectUploadFiles");
  const rejected = assert.rejects(request, { code: "host_request_cancelled" });
  host.reply("tutti-host-request-accepted");
  host.hide();
  host.reply("tutti-host-response", { result: ["late"] });
  await rejected;
  assert.equal(host.listeners.get("message")?.size, 0);
  assert.equal(host.timers.size, 0);
}));

test("picker: unsupported retains its stable cause", () => withHost(async host => {
  const request = requestHostCapability("selectUploadFiles");
  host.reply("tutti-host-response", { error: "unsupported" });
  await assert.rejects(request, { code: "host_capability_unsupported" });
}));

test("non-picker: ACK cannot extend ordinary or explicit session budgets", () => withHost(async host => {
  for (const [capability, budget] of [["archiveAgentPromptFile", 5_000], ["createAgentSession", 150_000]] as const) {
    const request = requestHostCapability(capability, [], budget);
    const rejected = assert.rejects(request, { code: "host_request_timeout" });
    host.reply("tutti-host-request-accepted");
    host.advance(budget - 1);
    assert.equal(host.timers.size, 1);
    host.advance(1);
    await rejected;
  }
}));
