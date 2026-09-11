import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const directory = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(
  resolve(directory, "WorkspaceChromeActions.tsx"),
  "utf8"
);

test("Windows help menu exposes the shared desktop update check", () => {
  assert.match(source, /useAppUpdateService\(\)/);
  assert.match(source, /void appUpdateService\.checkForUpdates\(\)/);
  assert.match(source, /t\("desktop\.menu\.checkForUpdates"\)/);
  assert.match(
    source,
    /disabled=\{appUpdateState\.isActing\}[\s\S]{0,220}updates\.checkingTitle/
  );
});

const embeddedSettingsHostSource = readFileSync(
  resolve(directory, "EmbeddedWorkspaceSettingsHost.tsx"),
  "utf8"
);

// 嵌入态（DinTalDock）不画那枚齿轮：它浮在转录右上角，是正文里唯一的浮动按钮，
// 既压着内容，也和分栏栏头右侧那排图标撞在一起。设置本身没被砍掉 —— provider 栏
// 底部的 ⋯ 里有一条 Settings，走的是 `useWorkspaceSettingsPanelRequest` 那条
// deep-link 通道，所以组件必须继续挂着（面板和请求桥都在它身上）。
test("the embedded shell paints no floating settings gear", () => {
  assert.match(source, /\{embedded \? null : \(\s*\n\s*<Tooltip>/);
  // 面板与请求桥留在原地：只是按钮不画，不是把整段设置摘掉。
  assert.match(source, /<WorkspaceSettingsPanel/);
  assert.match(source, /useWorkspaceSettingsPanelRequest\(\)/);
  // 原来那层「右上角浮动」的定位壳一并拆掉，别留一个空的 pointer-events 层。
  assert.doesNotMatch(
    embeddedSettingsHostSource,
    /absolute inset-x-0 top-0|data-dintaldock-settings-host/
  );
  assert.match(embeddedSettingsHostSource, /<WorkspaceSettingsTrigger\s/);
});
