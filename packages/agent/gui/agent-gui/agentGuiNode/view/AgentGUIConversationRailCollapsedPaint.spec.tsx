import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

// 折叠的会话栏必须整块 visibility:hidden。只靠「宽 0 + overflow + 子项
// opacity/filter」时，WebKit 会把折叠期间重绘的会话行画到错误的合成层上，
// 行文字漏出来压在对话流上（DinTalDock 分栏窄窗里复现）。
const agentActivityStyles = readFileSync(
  resolve(process.cwd(), "app/renderer/agentactivity.css"),
  "utf8"
);

afterEach(() => {
  document.head.innerHTML = "";
  document.body.innerHTML = "";
});

function mountRailPanel(collapsed: boolean): HTMLElement {
  const style = document.createElement("style");
  style.textContent = agentActivityStyles;
  document.head.append(style);
  const panel = document.createElement("aside");
  panel.className = collapsed
    ? "agent-gui-node__rail-panel agent-gui-node__rail-panel--collapsed"
    : "agent-gui-node__rail-panel";
  const rail = document.createElement("aside");
  rail.className = "agent-gui-node__rail";
  const row = document.createElement("div");
  row.className = "agent-gui-node__conversation-item";
  rail.append(row);
  panel.append(rail);
  document.body.append(panel);
  return row;
}

describe("collapsed conversation rail paint", () => {
  it("hides every rail row from painting while collapsed", () => {
    const row = mountRailPanel(true);
    expect(getComputedStyle(row).visibility).toBe("hidden");
  });

  it("keeps rail rows visible while expanded", () => {
    const row = mountRailPanel(false);
    expect(getComputedStyle(row).visibility).not.toBe("hidden");
  });
});
