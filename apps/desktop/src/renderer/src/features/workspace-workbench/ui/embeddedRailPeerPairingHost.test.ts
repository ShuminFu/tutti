// 结对模式三件套的桥错误归一（peer-pair-mode 评审补充 1）：
// 只有「宿主没注册这个能力」才算 unsupported（分栏层会永久收起单选）；超时是普通失败。
import assert from "node:assert/strict";
import test from "node:test";
import { HostBridgeUnavailableError } from "../../../platform/desktop/web/webHostBridgeClient.ts";
import { normalizePairModeBridgeError } from "./embeddedRailPeerPairingHost.ts";

function bridgeError(code: string, message = `tutti host bridge: ${code}`) {
  return Object.assign(new HostBridgeUnavailableError(message), { code });
}

function thrown(error: unknown): Error {
  try {
    normalizePairModeBridgeError(error);
  } catch (caught) {
    return caught as Error;
  }
  throw new Error("normalizePairModeBridgeError 必须抛出");
}

test("能力未注册 → unsupported", () => {
  assert.equal(thrown(bridgeError("host_capability_unsupported")).message, "unsupported");
});

test("桥超时不是 unsupported：普通错误，带人话", () => {
  const error = thrown(bridgeError("host_request_timeout"));
  assert.notEqual(error.message, "unsupported");
  assert.equal(error.message, "宿主响应超时，请稍后再试");
});

test("后端拒绝原样透传", () => {
  const backend = new Error("409 kickoff_roles_changed");
  assert.equal(thrown(backend), backend);
});
