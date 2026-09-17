# Codex 思考档位 + 重启后 context window 缩减 —— 排查与精准修复计划

> 2026-09-17 整理补记：本文是 09-16 的调查与方案快照，包含后文对前文的修正；以第 7 节当时复检为阅读入口。当前主工作区已无代码 WIP，第 7 节“未提交”“正在跑”及进程身份均为历史现场。本次仅保存调查记录，未重新验证其剩余问题或执行修复方案。

日期：2026-09-16 · 仓库：`/Users/jrrc/Projects/dintaldock` @ `dintal-dock`
证据等级标注：**[已证实]** = 已在代码/运行产物中直接读到；**[推断]** = 由已证实事实推导。

---

## 0. 结论速览

| # | 问题 | 根因（一句话） | 状态 |
|---|---|---|---|
| 1 | Codex 无法选择 reasoning effort | Codex **没有静态档位兜底**，全部依赖实时 `codex app-server model/list` 探测；而**任何绑定了 model plan / host endpoint 的会话会跳过该探测**，本应补偿的 overlay 又因 `ReasoningEfforts` 在 DTO 投影时被丢弃而永不生效 | 已定位（含 8 条空路径） |
| 2 | 重启/热重构后 context window 大幅缩减 | `UpdateSessionSettings` 的 SQL 在 model 字符串变化时**删除**持久化的 `usage.contextWindow`；恢复路径只从 `[1m]` 标记重建窗口，普通模型重建为 `0`；DSH 随即回落到自身默认 `128_000` 并回写 | **根因已证实（含运行产物铁证）** |

问题 2 已取得**端到端闭环证据**，见 §3.6 —— 这不是推断，是直接读到的运行产物。

---

## 1. 关于「DingTalk」

**先说清楚一件事**：我在 `dintaldock` 全仓库检索 `dingtalk` / `钉钉`（含全部历史提交、全部分支），**零命中**。

- `apps/desktop/package.json` → `productName: "Tutti"`，`appId: "sh.tutti.desktop"`。
- 截图窗口标题栏显示 **DinTalDock**，会话名为 `rndmaster-dev`。

所以「DingTalk 里使用 Codex」按现有证据**无法对应到本仓库的任何模块**。合理理解有两种，请确认其一：

1. 你指的是 **DinTalDock 桌面端**（即本仓库），「DingTalk」是口误/惯称 → 那么本报告 §2 直接就是答案。
2. 你另有一个**真正的钉钉侧集成**（机器人/小程序/DingTalk 侧的 Agent 面板），代码不在本仓库 → 那我需要该仓库路径才能排查；本仓库只提供**后端协议契约**（§2.3），可作为对端对齐依据。

下面按 (1) 展开——即「在本桌面端里用 Codex 选不了思考档位」。

---

## 2. 问题一：Codex 为什么选不了 reasoning effort

### 2.1 协议总览（后端 → 前端）

**传输方式：请求/响应 RPC，不是事件推送。** [已证实]

`POST /v1/agent-providers/{provider}/composer-options`
→ handler `services/tuttid/api/daemon_agent_sessions.go:81`
→ `AgentSessionService.GetComposerOptions`，实现在 `services/tuttid/service/agent/composer_options.go:145`

推送通道不存在：运行时的 `config_option_update`（`packages/agent/daemon/runtime/acp_live_state.go:152`）只改 daemon 侧 live state，随后被**下一次 RPC 响应**折叠进去。

### 2.2 线上字段名 [已证实]

`packages/clients/tuttid-ts/src/generated/types.gen.ts:1929-1972`：

| 字段 | 含义 |
|---|---|
| `reasoningConfig` | `{configurable, currentValue?, effectiveValue?, defaultValue?, options[]}` —— **扁平的全局**档位选择器 |
| `reasoningOptionsByModel` | `map<model, {defaultValue?, options[]}>` —— **按模型**的档位画像，**Codex 实际填充的是这个** |
| `effectiveSettings.reasoningEffort` | 当前选中档位 |
| `runtimeContext.configOptions[]` | 遗留回显，**非权威** |

> 契约原文（`types.gen.ts:1946-1948`）：*"Typed composer fields are authoritative; new composer capabilities must not be added to this object."*

### 2.3 前端如何展示 [已证实]

- 展示组件：`packages/agent/gui/agent-gui/agentGuiNode/AgentComposerModelReasoningDropdown.tsx`
- 纯逻辑菜单模型：`.../model/composerSettingsMenuModel.ts:274-282` → `reasoning: {show, selectedValue, selectedLabel, options}`
- **显示门控**（`:164-167`）：
  ```ts
  const showReasoning =
    composerSettings.supportsReasoningEffort && reasoningItems.length > 0 && !composerSettings.reasoningUnavailable;
  ```
- 能力位（`model/composerSettingsSupport.ts:69-71`）：
  ```ts
  reasoning: (composerOptions?.reasoningConfigurable ?? false) || hasModelReasoningOptions,
  ```
  （`8c0e68777` 加的 `hasModelReasoningOptions` 或分支，让「只有按模型画像」也能点亮控件）
- 标签解析：`resolveReasoningOptionLabel`（`:644-679`），硬编码 `default/minimal/low/medium/high/xhigh/max/ultra`，兜底 `providerLabel?.trim() || value`。

**优先级**（`agentGuiController.composerHelpers.ts:151-190`）[已证实]：
```
live 运行时回显  >  按模型画像 reasoningOptionsByModel[选中模型]  >  扁平 reasoningConfig.options
```
⚠️ **但优先级第一层在生产中是死代码**：两处调用点（`controller/useAgentGUIComposerPresentation.ts:148-152`、`packages/agent/gui/quickComposerSettings.ts:84-88`）都**只传 3 个参数**，`sessionRuntimeContext` 恒为 `null` ⇒ `liveConfig` 恒为 `null`。实际生效的是 **按模型画像 → 扁平配置 → `options[0]`**。

新建草稿的默认档位是**硬编码字面量 `"high"`**（`useAgentGUIComposerCapabilities.ts:63`）。

### 2.4 根因：Codex 没有静态兜底 [已证实]

`packages/agent/daemon/providerregistry/codex.go:91-93`：
```go
ReasoningEffort:        true,
ReasoningEffortOptions: ReasoningEffortOptionsModelCatalog,   // ← 模型目录制
DefaultReasoningEffort: "high",
```
且 `providerregistry/registry.go:471-475` **强制**模型目录制 provider 不得声明静态值：
```go
case ReasoningEffortOptionsModelCatalog, ReasoningEffortOptionsStrictModelCatalog:
    if descriptor.ComposerProfile.ModelCatalog == "" { ... }
    if len(descriptor.ComposerProfile.ReasoningEffortValues) != 0 {
        return fmt.Errorf("provider %q model-catalog reasoning options cannot declare static values", providerID)
    }
```

⇒ **Codex 的每一个档位值都必须来自活的 `codex app-server` 探测，没有任何静态兜底列表。**

探测链：`composer_options.go:271-279` → `composer_model_catalog.go:38-52` → `codex_model_catalog.go:118` 起 `codex app-server` → `:169-177` 发 `model/list {limit:200}` → `packages/agent/daemon/modelcatalog/codex.go:48` 读 `supportedReasoningEfforts` → `composer_projection.go:22-37` 投影成画像。

**硬门控**（`composer_projection.go:29`）：
```go
if modelID == "" || !model.ReasoningEffortsAdvertised { continue }
```
模型不吐 `supportedReasoningEfforts` ⇒ **该模型连画像都没有** ⇒ `ReasoningOptionsByModel` 保持空 map。

### 2.5 产生「空档位」的全部 8 条路径 [已证实]

| # | 条件 | 位置 | 后果 |
|---|---|---|---|
| E1 | `model/list` 探测失败/超时/无 codex 二进制（8s 超时，`codex_model_catalog.go:21`） | `composer_model_catalog.go:42-52` 返回 `ok=false` | 画像 `{}`，扁平列表 `nil` → **档位选择器空** |
| E2 | Codex 版本 < `0.126.0`（`providerregistry/codex.go:8`） | 老 app-server 无 `model/list` | 同上 |
| E3 | 模型显式返回 `supportedReasoningEfforts: []` | `composer_projection.go:29` 仍建画像但 `Options` 空；`composer_live_model_reasoning.go:72` → `Configurable=false` | 控件不显示 |
| E4 | 模型**完全省略**该字段 | `composer_projection.go:29` `continue` | 该模型无画像 |
| E5 | 选中模型 id 与画像 key 不严格相等 | `composer_live_model_reasoning.go:62`、`composer_reasoning_options.go:108` | 画像有值但绑不上 |
| **E6** | **绑定了 model plan / host endpoint** | `composer_options.go:271`：`if planEndpoint == nil && ...` | **目录探测被整体跳过** |
| E7 | E6 之后的 plan overlay 无法补值 | 见下 | **只剩当前选中值一项** |
| E8 | plan 文档里没有 `reasoningEfforts` | `composer_reasoning_options.go:56` `if len(efforts)==0 { return nil }` | overlay 原样返回，不补 |
| E9 | target 是 `agent_extension` | `composer_options.go:465-479` | 走 ACP 回显而非 Codex 目录 |

### 2.6 E6/E7 —— 最可能的实际病灶

**E6 跳过探测**（`composer_options.go:266-280`）：
```go
planEndpoint := modelPlanResolution.Endpoint
if planEndpoint != nil { settings.Model = planEndpoint.Model }
var catalogLoad <-chan composerModelCatalogLoadResult
if planEndpoint == nil && (composerOptionsProviderUsesModelCatalog(provider) || ...) {
    catalogLoad = startComposerModelCatalogLoad(...)   // ← plan 绑定则永不执行
}
```

**E7 overlay 本应补偿，但对 workspace plan 永不生效**：

`31e8d184b` 新增的补偿 `applyComposerModelPlanReasoningOptions`（`composer_reasoning_options.go:98-130`）读取 `endpoint.Models[].ReasoningEfforts`（经 `:37`）。但**两个 endpoint 构造点都把这个字段丢了** —— `model_plan_binding.go:250-262`：
```go
endpointModels = append(endpointModels, runtimeprep.ModelEndpointModel{
    ID:   model.ID,
    Name: model.Name,
})   // ← ReasoningEfforts 从未赋值
```
（被 `:390` 与 `:629` 调用，即 workspace-plan 与 workspace-agent **两条路径都丢**）

域类型也没有该字段：`biz/modelplan/model.go:136-146` 仅 `{ID, Name, Capabilities, Pricing}`；OpenAPI `ModelPlanModel`（`tuttid.v1.yaml:9343-9358`）只允许 `id/name/capabilities`。

> 而 DTO **是有**这个字段的：`packages/agent/runtimeprep/model_endpoint.go:52`
> `ReasoningEfforts map[string]*string \`json:"reasoningEfforts,omitempty"\``
> ⇒ 断点在**投影时主动丢弃**，而非类型不支持。

**后果链 [已证实]**：`ReasoningEfforts` 为 `nil` → `composerModelPlanReasoningOptionValues` 返回 `nil`（`:56-58`）→ 每个模型被跳过（`:39-40`）→ `len(profiles)==0` → **`applyComposerModelPlanReasoningOptions` 原样返回 options（`:104-105`）**。对已跳过目录探测的 plan 绑定 Codex 目标，`reasoningConfig.Options` **只剩选中值兜底** ⇒ 症状 = 「只有当前档位，选不了」。

**唯一能跑到该 commit 代码的路径是 host endpoint**：`runtimeprep.HostModelEndpoint`（`host_model_endpoint.go:72-107`）整份反序列化 `ModelEndpointConfig`（含 `models[].reasoningEfforts`，`model_endpoint.go:49-53`），由 `TUTTI_HOST_MODEL_ENDPOINTS` / `_FILE` 注入。该 commit 自带测试即证明这点（`model_plan_binding_test.go` 写含 `"off":"none"` 的 host 文件）。

> 📌 `"off" -> "none"` 是**宿主文档的数据约定，不是代码转换** —— 代码里没有这个映射。文档 `agent-provider-setup.md:1028` 的表述属宿主侧要求，容易误读为已实现行为。

### 2.7 验证方法（请在复现现场执行）

```bash
# 1) 看 Codex 是否真的能吐出 supportedReasoningEfforts
codex app-server   # 发 initialize → initialized → model/list {"limit":200}

# 2) 看 daemon 侧有没有拿到目录
#    grep "composer model catalog lookup failed" 日志
#    → 命中即 E1，未命中但档位仍空则看 E3/E4/E5/E6

# 3) 看该会话是否绑了 model plan（决定 E6）
```
判据：日志命中 `composer model catalog lookup failed` → **E1**；无该日志但档位空 → **E6/E7**。

---

## 3. 问题二：重启/热重构后 context window 缩减（根因已闭环）

### 3.0 症状

截图：`Context Usage — Context window 383,367 / 128,000 (100%)`

### 3.1 分母在 DinTalDock 里只有一个产出点 [已证实]

`packages/agent/daemon/contextwindow/contextwindow.go:12-58`：
```go
const OneMillionTokens int64 = 1_000_000
const marker = "[1m]"

// Window returns the token window model asks for, or 0 when it asks for none.
func Window(model string) int64 {
	if _, ok := Split(model); ok {
		return OneMillionTokens
	}
	return 0
}
```

消费契约（`packages/agent/runtimeprep/model_endpoint.go:29-34`）：
```go
// ContextWindow is the token window the session's selected model asked
// for; 0 leaves the runtime its own default. Only runtimes whose session
// catalog carries a window per model (deepseek-harness) read this ...
ContextWindow int64 `json:"contextWindow,omitempty"`
```
`0` ⇒ `omitempty` 丢掉该键 ⇒ **runtime 用自己的默认值**。

**本仓库没有 `128000` 字面量** —— 全量 grep（`.go/.ts/.tsx/.json/.toml`，排除 `node_modules`/`dist`/`out`）唯一命中是 Monaco 编辑器内的 Unicode 区段表（`apps/desktop/out/.../editor.main-*.js:4126`），与语义无关。

### 3.2 故障链（四步，全部已证实）

**Step 1 — 热重构时 SQL 删除持久化窗口**
`packages/agent/store-sqlite/activity_update.go:188-208`：
```sql
UPDATE workspace_agent_sessions
SET model = ?,
    settings_json = ?,
    session_metadata_json = CASE
      WHEN trim(model) <> trim(?) THEN          -- ← 模型字符串一变即触发
        CASE
          WHEN json_array_length(json_extract(session_metadata_json, '$.usage.quotas')) > 0
            THEN json_remove(session_metadata_json, '$.usage.contextWindow')
          ELSE json_remove(session_metadata_json, '$.usage')
        END
      ELSE session_metadata_json
    END,
```
整个 `usage.contextWindow`（`usedTokens` + `totalTokens`）被移除。这是**有意为之**（旧窗口对新模型是谎报），并被测试固化：`activity_update_settings_test.go:37-39` 断言删除后 `ContextWindow == nil`。

**Step 2 — 恢复路径只认 `[1m]` 标记**
`services/tuttid/service/agent/session_runtime_snapshot.go:344`（host-default 分支）与 `:389`（plan-revision 分支）：
```go
contextWindow := contextwindow.Window(effectiveModel)   // 非 [1m] ⇒ 0
```
**没有任何代码把 `Metadata.Usage.ContextWindow.TotalTokens` 读回 endpoint。** 姊妹测试反而固化反向行为（`session_runtime_snapshot_test.go:248`：`plain.ContextWindow != 0` 即失败）。⇒ **非 1M 模型没有恢复机制。**

**Step 3 — 窗口不写进 runtime 配置**
`packages/agent/runtimeprep/extension_runtime_json.go:51-77`：
```go
contextWindow := endpoint.ContextWindow
...
if contextWindow > 0 && id == selectedModelID {
    entry["contextWindow"] = contextWindow      // 0 时不写该键
}
```
断言见 `extension_runtime_context_window_test.go:69-77`。

**Step 4 — DSH 回落到自身默认 128,000 并回写**
`.../deepseek-harness-extension/runtime/darwin-arm64/app/lib/modelGateway.js:4`：
```js
export const GATEWAY_CONTEXT_WINDOW = 128_000;
```
`.../app/lib/acpServer.js:200`：
```js
const contextWindow = live.gateway.models.find(model => model.id === live.selection.current?.model)?.contextWindow ?? GATEWAY_CONTEXT_WINDOW;
```
找不到 per-model 窗口 ⇒ `??` 击中 `128_000`，经 `usage_update` 上报，DinTalDock 持久化为新分母。

### 3.3 铁证：实际运行产物 [已证实]

读取该会话运行目录 `runs/3c61d1b7-.../deepseek-harness/dintal-runtime.json`：

```
total models: 55
count with contextWindow: 0        ← 55 个模型，0 个带 contextWindow

--- deepseek entries ---
{"id": "deepseek-flash",          "name": "deepseek-flash",          "reasoningEfforts": {"high":"high","max":"max","off":"none"}}
{"id": "deepseek-v4-flash-0731",  "name": "deepseek-v4-flash-0731",  "reasoningEfforts": {...}}
{"id": "deepseek-v4-pro",         "name": "deepseek-v4-pro",         "reasoningEfforts": {...}}
{"id": "deepseek-v4-pro-0813",    "name": "deepseek-v4-pro-0813",    "reasoningEfforts": {...}}
```

**`contextWindow` 0/55 —— 与 `acpServer.js:200` 的 `?? GATEWAY_CONTEXT_WINDOW` 严丝合缝。** 这从「推断」升级为**直接观测**：分母 `128,000` 就是 `GATEWAY_CONTEXT_WINDOW`。

> 附带信息：同一份产物显示 `deepseek-flash` 是**通过 gateway 的 host-endpoint 路径**解析的（`reasoningEfforts` 已注入成功）—— 这正好印证 §2.6「overlay 只在 host endpoint 路径生效」。

### 3.4 为什么分子 > 分母

- 分子由 runtime 上报，DinTalDock 只做非负钳制、**不**把 `used` 钳到 `total`（`acp_live_state.go:219-251`）。
- 两个独立原因叠加：① 分母被删后回落（主因，§3.2）；② 分子是**累计量**而分母是**窗口量**（单位不同）。Claude SDK 路径甚至把 4 个计数字段求和（`claude_sdk_live_state.go:583-587`）。
- GUI 用 `Math.min(100, ...)` 钳显（`packages/agent/activity-core/src/usage.ts:43`）⇒ 溢出渲染成正好 `100%`。

### 3.5 已排除项 [已证实]

- ❌ 无「先算窗口后加载元数据」的时序 bug：`session_runtime_snapshot.go:361-388` 先解析 plan revision 再读 model。
- ❌ 无「旧值覆盖新值」的竞态：`mergeACPUsageState` / `mergeClaudeSDKUsageState` 只**填补**缺失窗口，从不覆盖已有值。
- ❌ 没有任何 config-change handler **赋默认值**；窗口消失是靠**删除**实现的。

### 3.6 铁证补齐：触发链已闭环（直接取 demo DB + DSH `sessions.sqlite3` + daemon 日志）

故障会话 `3c61d1b7-4565-5dac-aee3-ea035e3bd9c1`（demo 实例，`acp:deepseek-harness`）完整时间线：

| 时间 (+08:00) | 事件 | 证据 |
|---|---|---|
| 00:38:04 | 创建请求 `model=deepseek-flash[1m]`（带 1M 标记） | 日志 `service.create.model_validated` |
| 00:38:09 | 首次 usage 上报 `raw_size=1000000` | 日志 `acp.usage_update` |
| 00:38:08Z | DSH 首个 `request/context`: `contextWindow:1000000` | DSH events seq 11 |
| 01:16:42 | turn 1 结束（236 step，最后 1M 事件 seq 182687） | 日志 + DSH events |
| 08:41:27 | daemon PID 29047 收 SIGTERM 退出（App 重启） | 日志 `tuttid received signal` |
| 09:24:31 | 新 daemon PID 67253 启动 | 日志 `parent monitor started` |
| 09:30:19 | resume（`api.send`），adapter 进程重启、复用同一 `sessions.sqlite3` | 日志 submit trace 全集 |
| 09:30:28Z | DSH turn 2 首个 `request/context`: `contextWindow:128000` ← **切换点** | DSH events seq 186266 |
| 09:30:36 | 首次 `raw_size=128000 raw_used=383367` → 与截图完全一致 | 日志 `acp.usage_update` |

**结论（修正 §3.2 的触发链）**：本实例**没有**发生 settings 更新——该会话全日志只有 `api.create`（00:38）与 `api.send`（09:30）两条 submit trace，`activity_update.go` 的 SQL 删除（Step 1）**未触发**。真实链路（每步都有代码 + 数据双证据）：

1. **创建**：request 带 `deepseek-flash[1m]` → endpoint `ContextWindow=1000000` → runtime json 写出 `contextWindow:1000000`（DSH seq 11 直接证明）；
2. **入库时出现分裂**：会话行的 `model` 列、`settings_json.model`、`config.model`、`configOptions[0].currentValue` **全部是裸 `deepseek-flash`**（GUI 回显的 composer scope 把标记值钳回了基础目录项），但 **`internal_runtime_context_json.sessionRuntimeSnapshot.model` 忠实保留了 `"deepseek-flash[1m]"`**（`runtimeContextWithSessionRuntimeSnapshot` 在 model_validated 之后快照 input.Model，见 `session_runtime_snapshot.go:104`）——标记唯一存活的副本就在 snapshot 里；
3. **重启 + resume 时 snapshot 被绕过**：`service_resume_helpers.go:118` 只在 `input.Model == ""` 时才回填 snapshot 的模型；GUI 回显的裸模型非空 → `effectiveModel = "deepseek-flash"`；
4. **窗口推导只看标记**：`session_runtime_snapshot.go:344` 只调 `contextwindow.Window(effectiveModel)` → 裸模型 = 0；**既没查宿主的 `modelContext` 表（`host_model_endpoint.go:109` 的 `HostModelContextWindow` 明明有 `deepseek-flash → 1_000_000`，但 session endpoint 解析从不调用它），也没回退持久化窗口（metadata 里仍存的 1M）** → `omitempty` 丢弃 → runtime json 0/55（实况）；
5. **DSH 热载新配置**：`acpServer.js` 的 `?? GATEWAY_CONTEXT_WINDOW` 落到 128k；09:30:36 首条上报 `raw_size=128000 raw_used=383367` 覆盖 metadata → GUI 显示 383,367/128,000；
6. **附随损害**：turn 2 开头的自动压缩直接失败——`compaction/end error: "summarization truncated at the token cap (incomplete checkpoint)"`（压缩预算随窗口缩水，真实上下文 ~383k 已装不进 128k 上限）。

**系统性（DB 横切）**：demo DB 中 93 个会话 metadata 仍为 1M、14 个已缩到 128k。保留 `[1m]` 标记的行全部是 `claude-code`/`codex` 原生路径（9 行）；DSH 路径 84 个 1M 会话**全部**是裸模型行——「GUI 回显钳回裸值、只有 snapshot 记住标记」是 DSH 路径的常态，任何此类会话经历 daemon 重启 + resume 都会复现。

> 根因聚焦点修正：**本次事故的直接凶手是「resume 时窗口推导只有 `[1m]` 标记一个来源」——既绕过了唯一存有标记的 snapshot（`service_resume_helpers.go:118` 的回填条件太窄），又没接上宿主的 `modelContext` 权威窗口表，也没回退已持久化的窗口。** P0-2 的 SQL 删除是另一条独立触发路径，本次未命中但仍是真 bug。共同结构性成因：窗口知识只挂在「模型字符串里的 `[1m]` 标记」这一根易碎绳子上，而这条绳子在 GUI 回显链路里就被钳掉了，只有 immutable snapshot 和宿主 modelContext 表还握着真相。

---

## 4. 精准修复执行计划

> 按依赖排序。P0 为直接止血，P1 补恢复能力，P2 消除结构性缺陷。

### P0-1　恢复非 1M 模型的窗口（最小、最高收益）

**文件**：`services/tuttid/service/agent/session_runtime_snapshot.go:344`（host-default 分支）与 `:389`（plan 分支）；配套 `service_resume_helpers.go:118`

把「只从 `[1m]` 标记推导」改为**三层级联回退**（标记 → 宿主权威窗口表 → 已持久化窗口）：

```go
// 1M 标记优先（它是对 runtime 的显式请求）；其次宿主的 modelContext 表
// （host_model_endpoint.go:109 HostModelContextWindow，宿主的权威逐模型窗口，
// 本次事故里 deepseek-flash→1M 就躺在那里没人问）；最后回退到上次持久化的
// 真实窗口，避免重启后被 runtime 默认值（DSH = 128k）静默缩小。
contextWindow := contextwindow.Window(effectiveModel)
if contextWindow == 0 {
    if w, ok := runtimeprep.HostModelContextWindow(contextwindow.Bare(effectiveModel)); ok { // 仅 host-default 分支
        contextWindow = w
    }
}
if contextWindow == 0 {
    contextWindow = persistedContextWindowTotal(snapshot) // 读 Metadata.Usage.ContextWindow.TotalTokens
}
```

配套一（防 GUI 回显钳掉标记）：`service_resume_helpers.go:118` 的回填条件从「input.Model 为空」放宽为「input.Model 为空，**或** 与 snapshot.Model 去掉 `[1m]` 后同 id」——GUI 每次回显裸值，而 snapshot 保存着用户真实的 1M 选择，resume 时应当让 snapshot 的标记值赢回来。
配套二：新增取数辅助函数（读 `SessionMetadata.Usage.ContextWindow.TotalTokens`，见 `packages/agent/store-sqlite/session_metadata.go:27-35`）。
**验收**：改前/改后 `resume` 单测；新增「裸模型 + 宿主表有值 → `Endpoint.ContextWindow` = 宿主值」与「plan 模型重启后回退持久化值」用例。
**注意**：`session_runtime_snapshot_test.go:248` 断言的是**创建**路径的 `0`，勿误改；只改 resume 分支。

### P0-2　让窗口失效可自愈，而不是单向删除

**文件**：`packages/agent/store-sqlite/activity_update.go:192-199`

两条可选路线，建议 (b)：

- (a) 删除时**保留** `totalTokens` 仅清 `usedTokens`，并让 resume 用 `totalTokens` 重建（依赖 P0-1）。
- (b) **保持删除语义不变**，但确保 P0-1 的回退能拿到值 —— 即把窗口同时存到**不随 model 变化而清空**的位置（如 `InternalRuntimeContext`，见 `service_resume_helpers.go:122`）。

> ⚠️ 注意 `mergeACPUsageState` 只填补不覆盖：若选择「保留旧窗口让 runtime 覆盖」，需同步放开为**允许替换**，否则会一直显示陈旧窗口。二者必须一起改，不可只改一处。

**验收**：单测覆盖「model 变化 → 重启 → 窗口被正确重建为旧真实值」。

### P1-1　修 Codex 档位：不再丢弃 `ReasoningEfforts`

**文件**：`services/tuttid/service/agent/model_plan_binding.go:250-262`

```go
endpointModels = append(endpointModels, runtimeprep.ModelEndpointModel{
    ID:               model.ID,
    Name:             model.Name,
    ReasoningEfforts: model.ReasoningEfforts, // ← 补上，DTO 已支持
})
```
配套：`biz/modelplan/model.go:136-146` 的 `Model` 增 `ReasoningEfforts map[string]*string`；OpenAPI `ModelPlanModel`（`tuttid.v1.yaml:9343-9358`）加同名字段并放开 `additionalProperties: false`；plan 读写与 fingerprint 同步。
**验收**：既有 `TestHostDefaultModelEndpointCarriesCodexReasoningOptions` 扩展出**workspace plan** 对应用例并转绿 —— 当前该测试只覆盖 host endpoint，这正是缺陷藏身处。

### P1-2　plan 绑定不应整体跳过目录探测

**文件**：`services/tuttid/service/agent/composer_options.go:271`

`if planEndpoint == nil && ...` 使 plan 绑定者失去 Codex 目录。建议：当 provider 声明 `ReasoningEffortOptionsModelCatalog` 且 plan 未提供档位时，**仍执行探测**并在 overlay 中**合并**（plan 优先、目录兜底）。
**验收**：新增「plan 绑定 + Codex → 档位列表非空」用例。

### P1-3　补运行时目录兜底（治本）

**文件**：`packages/agent/daemon/modelcatalog/composer_projection.go:29` 附近

`E1`（探测失败）目前直接导致空档位。建议：探测失败时回落一组**保守默认档位**（复用已存在的 `codex_appserver_model_settings.go:186-188` 的 `{"minimal","low","medium","high","xhigh"}`），并标记 `degraded` 供 UI 区分「未知」与「不可用」。
**理由**：该静态表已存在于代码库，只是**没有接到 composer 路径**——属接线缺失而非常缺能力。

### P2-1　消除 `planEndpoint` 双开关

`composer_options.go:271` 一处判断同时控制「目录探测」与「模型来源」，是 E6 的结构性成因。建议拆成两个独立维度（`modelSource` / `reasoningSource`），避免今后再出现「一个开关关掉两件不相干的事」。

### P2-2　契约与文档对齐

- `agent-provider-setup.md:1028` 的 `off -> none` 应**显式标注为宿主文档约定**（代码无此映射），避免误读。
- `ModelEndpointModel.ReasoningEfforts` 建议加注释说明「plan 路径必须透传」，防止再次被投影丢弃。

### 4.4 两条路径覆盖矩阵（自己账号 / 公司网关，均须正确拉出档位与窗口）

| 路径 | reasoningEfforts 现状与修复 | contextWindow 现状与修复 |
|---|---|---|
| **公司网关**（host endpoint，`acp:deepseek-harness`/`grok`/`hermes`/`codex`/`claude-code`/`opencode` 共 6 条路由） | 只有 4 个 deepseek 模型在宿主目录里声明了档位，其余 50+ 模型（含 codex 路由）为空 → 靠 **宿主数据补齐**（`host-model-endpoints.json` 逐模型 `reasoningEfforts`）+ 本仓 P1-2 探测兜底 | **本次事故**：宿主目录模型条目 0 个 `contextWindow`，但顶层 `modelContext` 表有 8 个模型的真实窗口（deepseek-flash→1M），session endpoint 解析却从不读它 → **本仓 P0-1 三层回退**（标记→`HostModelContextWindow`→持久化值），不需要宿主改数据 |
| **自己账号**（Codex/Claude 原生凭据，无 plan 绑定） | Codex 走 `app-server model/list` 探测，正常可用；探测失败时为空 → P1-3 回落保守默认档位 | runtime 原生自管窗口（DTO 该字段对原生 runtime inert），DinTalDock 只需**不注入错误值、不删除已上报值**（P0-2 的删除语义即为此） |
| **自己账号 + workspace plan 绑定** | E6（plan 绑定跳过探测）+ E7（投影丢字段）→ 空 → **P1-1 + P1-2** | 同原生路径；plan 分支的重启回退由 P0-1 的持久化值兜底 |

> 两条路径的窗口知识来源不同，修复动作因此不同：**公司网关的窗口必须由注入供给**（DSH 不自知模型窗口，只会落 128k 默认），注入源应当首选宿主的 `modelContext` 权威表；**自己账号原生 runtime 的窗口由 runtime 自己上报**，DinTalDock 侧的义务是别破坏它（P0-2 删除语义 + P0-1 回退）。

---

## 5. 验证与人工验收点

| 项 | 方式 |
|---|---|
| P0 修复 | `go test ./packages/agent/store-sqlite/... ./services/tuttid/service/agent/...`（仅相关用例，不跑全量） |
| P0 现场复核 | 会话重启后 `dintal-runtime.json` 中选中模型须带 `contextWindow`，且 GUI 分母等于真实窗口 |
| P1 修复 | `go test ./services/tuttid/service/agent/...` + 手工打开 Codex composer 确认档位可列可选 |
| P1 现场复核 | Codex 档位日志无 `composer model catalog lookup failed`；plan 绑定会话亦能列出档位 |

**遵 AGENTS.md**：默认不跑全量测试/lint/build，不执行打包与真实宿主验收；上表为建议项，是否执行由你决定。

---

## 6. 待你确认

1. ~~「DingTalk」到底指什么~~ —— 已确认就是本仓库的 DinTalDock/Tutti 桌面端（全仓 grep 零命中「DingTalk」）。
2. ~~§3.6 的 model 字符串比对~~ —— 已闭环：直接读取 demo DB + DSH `sessions.sqlite3` + 双 daemon 日志，见 §3.6（含 snapshot 唯一保留标记、resume 绕过 snapshot、宿主 modelContext 表无人查证三个决定性证据）。
3. ~~修复范围~~ —— 已确认并落地：P0（窗口）+ P1（Codex 档位）均已实现，见 §7 落地核对。
4. **公司网关档位数据的宿主侧补齐** —— 窗口修复不需要宿主改数据（`modelContext` 表现成可读）；但 codex 路由等非 deepseek 模型的**档位**仍需宿主在 `host-model-endpoints.json` 逐模型声明 `reasoningEfforts`（见 §7 剩余项 S3）。

---

## 7. 修复落地核对（2026-09-16 复检）

### 已落地 ✅

| 项 | 落点 | 证据 |
|---|---|---|
| 窗口根因（create 侧） | merge `08eb19eba`（09-15 12:02）内含 `97445ca03`：`modelPlanResolution.SessionModel` 新增，`resolveCreateSessionModelForPlanOrProvider` 不再用裸 `Endpoint.Model` 覆盖 `input.Model`，会话行/settings_json 保留 `[1m]` 拼写；`db283da20`：1M composer 行严格门控在宿主 `modelContext` 表；`7bc3b7b44`：claude ACP 通道带标记 | 代码 diff + 注释明示「旧行为正是把每个 X[1m] 会话静默降级到 200k 的元凶」 |
| Codex 档位（plan/host overlay） | commit `31e8d184b`（09-16 01:00）：新增 `composer_reasoning_options.go`，`applyResolvedModelPlanComposerOverlay` 接上 `applyComposerModelPlanReasoningOptions`，把 endpoint 模型级 `reasoningEfforts` 投进 composer profile 并带到 1M variant | 含 `model_plan_binding_test.go` +58 用例 |
| 静态档位兜底（defaults 路径，进行中） | 工作树未提交：`ExtensionComposerProfile.ReasoningEffortOptions/DefaultReasoningEffort` + `applyExtensionStaticReasoningConfig` + `composer_target_runtime_evidence.go`（ACP 配置证据缓存） | `git diff` 未提交 |
| 宿主数据（部分） | demo `host-model-endpoints.json`：6 条路由各自给 4 个 deepseek 模型声明了档位（codex 路由的 `deepseek-flash` 有 `high/max/off`） | 直接读文件 |

### 还需补充 ⚠️（按优先级）

- **S1 存量裸会话的恢复路径仍然没有**：`session_runtime_snapshot.go:344/389` 至今仍只调 `contextwindow.Window(effectiveModel)`，`service_resume_helpers.go:118` 仍只在 `input.Model==""` 时回填 snapshot。SessionModel 修复只保护**新**会话；demo DB 里 84 个存量裸模型 1M 会话 + 14 个已缩到 128k 的会话，下次 daemon 重启 + resume 依旧缩水。建议补两道兜底（原 P0-1）：(a) `service_resume_helpers.go:118` 放宽为「input 为空，或与 `snapshot.Model` 去标记后同 id → 用 snapshot 的标记值赢回」——这能让 84 个存量会话自愈（它们的 snapshot 都存着 `[1m]`）；(b) `session_runtime_snapshot.go:344` 加 `runtimeprep.HostModelContextWindow(bare(effectiveModel))` 权威回退——这能自愈 14 个已缩会话（宿主表有 `deepseek-flash→1M`），无需数据迁移。
- **S2 P0-2 未动**：`activity_update.go:192-199` 仍会在 model 字符串变化时 `json_remove($.usage)`，属另一条独立触发路径（本次事故未命中，仍是真 bug）。
- **S3 宿主数据缺口**：仅 4 个 deepseek 模型有档位，codex 路由的 `gpt-6-astra` 等 50+ 模型仍无 `reasoningEfforts`（用户 codex 会话 9/10 用 deepseek-flash 已覆盖，1 个 gpt-6-astra 仍空）；`modelContext` 只声明了 8 个 1M 模型，**非 1M 模型的真实窗口（如 200k）宿主没给**，DSH 只能落 128k 兜底——GUI 分母对这类模型依旧不准。
- **S4 部署刷新**：demo daemon（PID 67253，09:24 启动）运行的是 09-15 22:45 构建的 `cliagent-backend/bin/tuttid-darwin-arm64`——含窗口 SessionModel 修复，**不含** 09-16 01:00 的 P1（Codex 档位）与未提交工作。需重新构建 + 重启 daemon 才能让 P1 生效；现场复核见下。
- **S5 存量受损会话修复**：14 个已缩会话即使加了 S1(b) 也需一次 resume 才能自愈；若不补 S1，需一次性数据修复（按宿主 `modelContext` 重写 metadata usage）。
- **S6 工作树收尾**：未提交改动（`composer_defaults_validation`、`composer_runtime_context`、`session_types`、`eventstream/*`、`preferences`、`agentextension/profiles` 等）属同一波「档位回显」收尾工作，需按功能边界原子 commit（遵 AGENTS.md）；当前用户侧 `go test ./services/tuttid/service/agent/... ./services/tuttid/service/preferences/... ./services/tuttid/service/eventstream/...` 正在跑，跑完再动。

### 现场验收点（S4 部署后）

1. 新建 DSH 会话选 `deepseek-flash[1m]` → 重启 daemon → resume → GUI 分母仍为 1,000,000（旧行为会变 128,000）。
2. 存量 14 个已缩会话 resume 一次，观察 S1 落地后是否回 1M。
3. Codex 路由选 `deepseek-flash` → composer 档位出现 high/max/off；`gpt-6-astra` 仍为空时即 S3 宿主数据缺口，走宿主侧补齐。
