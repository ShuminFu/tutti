# 计划：宿主投影只读一次 · 运行时选择收敛为一个模块（复盘候选 #6）

> 状态：**GATE 1 · 待批准**。本文是 grilling 的产物：约束、加深后模块的形状（不含签名）、
> 测试的存活/回归、切片与需要人拍板的决策。批准前不写代码。
> 来源：`docs/plans/2026-09-20-architecture-review-deepening-candidates.md` #6；
> 报告 https://claude.ai/artifact/3scqhk5oUKsJjByDx4GJdv 。

## 0. 一句话

宿主（rndmaster）→ 边车（tuttid）的输入今天是 16 个 env 名 + 2 份文件，在 20 个文件里
`os.Getenv` 现读；把它收成一个 **宿主投影（Host Projection）** 模块：启动时读一次 env 名，
文件内容按需重读，向下只传类型化的值；然后再让「哪条二进制跑 provider X」的优先级只在一处写。

## 1. grilling 摘要：约束（决定形状的事实）

| # | 约束 | 出处 | 对形状的影响 |
|---|---|---|---|
| C1 | **不重启生效**：宿主保存注册表 / 凭据后，边车下一次解析就要看到 | host_runtime_selection.go:16-25、host_model_endpoint.go:17-19、b3127b8 | env **名**（文件路径）可在启动期冻结；文件**内容**必须按需重读。模块要区分「值」与「文件句柄」两类事实 |
| C2 | 独立 DinTalDock（无 rndmaster 宿主）必须照旧：投影全部缺席 → 走各自的 legacy 链 | provider_resolution.go:156-190 注释；agent-extensions.md:411 | 模块要能表达「宿主没说话」(absent) 与「宿主明确说没有」(cleared) 两种缺省，不能把缺席当空值 |
| C3 | 消费方跨两个 Go 模块：`packages/agent/{runtimeprep,daemon/runtime}` 与 `services/tuttid/service/{agentstatus,agent,agentextension}` | 读取点清单 §2 | 新模块必须放在 `packages/agent/` 下才能被两边 import |
| C4 | 现有注入风格是「struct 字段 + Dependencies」：`agentstatus.Service{Environ, LookPath, …}` / `NewService(ServiceDependencies)`；`DefaultPreparer{StateDir,…}` | service.go:252-345；preparer.go:14-45 | 投影以**值**注入到这些 struct，不引入全局单例；wiring 只在 `wiring_daemon_api.go` 一处构造 |
| C5 | 接缝要有两个 adapter 才算真接缝 | codebase-design | 生产 adapter = env+文件；测试 adapter = 字面量文档。被删的 12 条测试正是第二个 adapter 的用户 |
| C6 | 已有 `Service.Environ func() []string` 覆盖 PATH 类读取，但不覆盖 `os.Getenv(名)` 与两份文件 | service.go:253 | 不再加第 N 个 func 字段，而是一个投影值；`Environ` 保留给 PATH/子进程环境 |

## 2. 现状：模块要接管的读取点（非测试）

**值类（启动期读一次即可）**

| env 名 | 含义 | 今天在哪读 |
|---|---|---|
| `RNDMASTER_TUTTI_EMBEDDED` | 嵌入宿主态 | types/defaults.go:203、wiring_agent_extension_startup.go:22、agentextension/manager.go:128、agent_replay_composition.go |
| `RNDMASTER_DOCK_SETUP_BASE` / `_TOKEN` | 宿主 Dock setup 端点 | agentstatus/provider_resolution.go:63,566,711、agent/embedded_dock_setup.go:17,36、wiring_daemon_api.go:658 |
| `TUTTI_CLAUDE_CODE_RUNTIME` (=acp) | claude 走 ACP 还是 SDK sidecar | provider_resolution.go:244 |
| `TUTTI_CLAUDE_SDK_SIDECAR_COMMAND` / `_ENTRY_PATH` | sidecar 覆盖 | provider_resolution.go:253,286 |
| `TUTTI_MANAGED_PROVIDERS` | 打包态 | provider_resolution.go:298、runtimeprep/claude.go:124 |
| `TUTTI_CODEX_MANAGED` / `_APP_SERVER_PATH` / `_GATEWAY_BASE` / `_GATEWAY_KEY` / `_CONFIG_TEMPLATE` / `_FAST_START` | 受管 codex | provider_resolution.go:60,98-100、runtimeprep/codex.go:133,135,186,203、daemon/runtime/codex_appserver_adapter.go:498-499 |
| `TUTTI_AGENT_EXTENSION_{GROK,DEEPSEEK_HARNESS}_{PACKAGE_DIR,RUNTIME_CHANNEL}` | 宿主随包投放的扩展 | types/defaults.go:222-229、agentextension/bundled_runtime.go:14、install_plan.go:256 |
| `CLAUDE_CODE_EXECUTABLE`、`CODEX_HOME`（操作者覆盖，非宿主专属） | 冻结在 spawn 时的旧值 | provider_resolution.go:171、runtimeprep/claude.go:127、codex.go:926、agent/model_config.go:19 |

**文件句柄类（路径冻结、内容按需重读，沿用现有校验：绝对路径 / 普通文件 / 非 symlink / 大小上限 / version=1）**

| 文件 | 内容 | 今天在哪读 |
|---|---|---|
| `TUTTI_HOST_MODEL_ENDPOINTS_FILE`（回落 `_ENDPOINTS` 内联） | routes / providers / modelContext | runtimeprep/host_model_endpoint.go（每次调用 parse 3 次）；消费者 agentstatus ×4、agentextension ×2、agent ×3 |
| `RNDMASTER_CLI_RUNTIME_SELECTION_FILE` | 每 provider 的 binPath | agentstatus/host_runtime_selection.go；消费者 provider_resolution.go:156、codex_runtime_catalog.go:126 |

**明确不纳入**：`TUTTI_STATE_DIR`、`TUTTI_DESKTOP_PARENT_PID`、`TUTTI_APP_VERSION`（进程引导期已在 main/defaults 读，属于进程身份不属于宿主投影）；`TUTTI_TEST_*`、analytics / browser / connector 家族（不同子系统，YAGNI）。

## 3. 加深后的形状（平实描述，不含签名）

**模块 A · 宿主投影（Host Projection）** — `packages/agent/hostprojection`（新，见 D1）

- interface 只有两类事实：**值**（上表第一组，已解析为 bool / 路径 / 枚举）与**文档句柄**（上表第二组，取一次返回已校验的文档，或「缺席」）。
- 三态缺省是 interface 的一部分：absent（宿主没说话）/ cleared（宿主明确说没有）/ set。今天 host_runtime_selection.go 已经有这三态，模型端点没有——统一。
- 两个 adapter：**FromProcess**（启动时读 env 名，文件按需重读）与**Literal**（测试用字面量）。
- 模块私有：env 名、路径校验、大小上限、version 判定、别名归一（claude/claude-code）。**它是全仓唯一可以出现这些 env 名字面量的地方**，由一条边界测试守住（见 §5）。
- 注入点（各一个字段）：`agentstatus.Service`（经 `ServiceDependencies`）、`runtimeprep.DefaultPreparer` 及其 `CodexPreparer` / `ClaudeCodePreparer`、`agentextension.Manager` / `SetupService`、daemon `codex_appserver_adapter` 的 config、`service/agent` 的 provider_auth_watcher / embedded_dock_setup / model_config。`wiring_daemon_api.go` 构造一次 FromProcess 传下去。

**模块 B · 运行时选择（Runtime Selection）** — 第二阶段（见 D4）

- interface：给定 provider（与 cwd / installation），返回**一次**解析好的绑定（可执行路径、启动命令、环境、来源、reason code），setup / status / launch / runtimeprep 都消费同一份，不再各解析一次。
- 实现私有：统一的优先级 **宿主投影 → 显式 / 持久化选择 → 受管 → 发现**，cleared 语义、版本下限、reason code。
- 每 provider 一个 adapter 只提供候选与校验器；今天 claude / codex 之间的「刻意的差别」（cleared 后跳过内联 vs 落回发现）变成 adapter 上的一个声明选项，不再是注释里的知识。
- 今天三条链的落点：claude `provider_resolution.go:156-190`、codex `codex_runtime_catalog.go:119-175`、扩展 `agentextension/manager.go:239-275`；另有 `runtimeprep/claude.go:123-137` 一条从不看投影的旁链要并入。

## 4. 删除测试（deletion test）

- 删掉模块 A：16 个名字重新散回 20 个文件，两份文件的校验逻辑各写两遍（今天就是这样）。→ 集中，而非搬家。
- 删掉模块 B：优先级重新写三遍且顺序不同（今天就是这样）。→ 集中。

## 5. 测试：存活 / 改写 / 回归 / 新增

- **存活**：所有不依赖 `t.Setenv` 的用例原样存活（interface 未变，只是多了一个可注入的值）。
- **改写**（Setenv → Literal 投影）：`services/tuttid/service/agent` 8 个测试文件、`agentstatus` 3 个、`runtimeprep` 1 个。
- **回归**（被删的 12 条，按被删时的断言原样恢复）：
  - 6c259ed 的 `TestHostModelEndpoint*` 3 条 → 直接以字面量文档恢复；
  - 6c259ed 的 `TestDefaultPreparerCodex*` / `*ACPExtension*` / `TestExtensionRuntimePreparer*` 6 条 → 需要 `CODEX_HOME`、`TUTTI_CODEX_MANAGED` 也从投影来（已纳入 §2）；
  - 58ad3e7 `TestDefaultControllerUsesClaudeSDKAdapterByDefault` → 断言改为「投影说 sdk 时用 SDK adapter」；
  - ec742fb 的 2 条 resume 用例 + a9758f5 提到的 22 条「读本机 host-model-endpoints.json 才红」→ `model_plan_binding` / `composer_context_window_options` 经 runtimeprep 的句柄读投影后自然确定性。
- **新增**：
  - 边界测试：扫描 `services/ packages/` 的非测试 Go 源，§2 的名字只允许出现在 `packages/agent/hostprojection` 内（形式同 `tools/scripts/check-agent-*-boundaries.mjs`，用 Go 测试写即可）。先带白名单起步、逐切片清零。
  - 差分验证（本仓被删测试的动机）：受影响的包在 `env -i` 与「注入 TUTTI_*/RNDMASTER_* 的宿主环境」下 `go test` 结果**逐条一致**。

## 6. 切片（每片可独立合并、行为保持）

| 片 | 内容 | 大致范围 |
|---|---|---|
| S1 | 新建 `packages/agent/hostprojection`：类型、FromProcess、Literal、三态、两份文档句柄；边界测试（白名单 = 今天的 20 个文件） | +1 包，≈400 行 + 测试 |
| S2 | `runtimeprep` 接入：host_model_endpoint.go 变薄成句柄消费者；codex.go / claude.go / runtime_instructions 的 env 读改为投影；`DefaultPreparer` 与两个 Preparer 加字段；wiring 传入。恢复 9 条测试 | 5 文件 |
| S3 | `agentstatus` 接入：provider_resolution.go 的 12 个常量与 6 处读取、host_runtime_selection.go 并入模块、dock-setup 三个分支；`ServiceDependencies` 加字段。恢复 1 条，改写 3 文件 | 6 文件 |
| S4 | `service/agent` 接入：provider_auth_watcher / embedded_dock_setup / model_config；`model_plan_binding` 与 `composer_context_window_options` 经句柄读。改写 8 文件，22 条环境相关红变确定 | 5 文件 + 测试 |
| S5 | 其余读取点：`types/defaults.go`、`agentextension`（manager / bundled_runtime / install_plan）、daemon `codex_appserver_adapter`、`wiring_*`、`agent_replay_composition`；边界白名单清零 | 7 文件 |
| S6 | 文档：新页 `docs/architecture/host-projection.md`（列出契约：名字、文档 schema、三态、no-restart 规则）；ADR「宿主输入经单一投影模块进入」；`CONTEXT.md` 加词条 | 3 文件 |
| S7（第二阶段） | 模块 B：先按 design-it-twice 出两份 interface 对比，再单独立计划 | 另开 |

rndmaster 侧**不需要改**：契约的名字与文件不变。只在 S6 的文档里把 rndmaster 写入这些名字的位置列出来（`cliagent-backend/internal/tuttid/supervisor.go`、`tutti_cli_runtime_selection.go`、`managed_providers.go` 等）。

## 7. 验证与 E2E 证据（GATE 3 前必须有）

1. 每片：`go vet` + 受影响包 `go test`，在 `env -i` 与宿主环境两种条件下各跑一次，结果一致。
2. S3 起：`RNDMASTER_CLI_RUNTIME_SELECTION_FILE` 的 no-restart 语义用测试钉住（先 absent → 写文件 → 同一进程再解析 → set；改成空 binPath → cleared）。
3. 端到端（**需要真机，含 rndmaster 宿主**）：嵌入态起 claude / codex / grok 各一条会话；宿主改 CLI 运行时路径后不重启边车，新会话用新路径；独立 DinTalDock（无宿主）三条 provider 照旧可起。这一步我在本环境跑不了，要在 dogfood 机上做并留截图/日志。
4. 不跑全量套件（按 CLAUDE.md）；受影响包名单在每片 PR 里列出。

## 8. 需要拍板的决策（GATE 1）

- **D1 · 模块位置**：新包 `packages/agent/hostprojection`（推荐：runtimeprep、daemon/runtime、services 三方都能 import）；备选：塞进 `runtimeprep`（省一个包，但 agentstatus 要为一个 env 值依赖整个 runtimeprep）。
- **D2 · 未注入时的语义**：字段为 nil 时视为「宿主缺席」（推荐：测试确定；靠边界测试保证没有漏改的调用点）；备选：nil 时回落读环境（零行为风险，但把泄漏留在原地，边界测试也会形同虚设）。
- **D3 · 范围**：§2 的 16 个宿主名 + 2 个操作者覆盖（`CLAUDE_CODE_EXECUTABLE`、`CODEX_HOME`）+ 7 个同函数内的 `TUTTI_CODEX_*` / `TUTTI_CLAUDE_SDK_SIDECAR_*`（推荐一并纳入：它们和宿主名写在同一批函数里，分开改要碰两次）；备选：只收 16 个宿主名。
- **D4 · 模块 B 的时机**：S1–S6 合并后再单独立计划并做 design-it-twice（推荐：三条链的数据类型不同——ProviderSpec / RuntimeBinding / codex candidate——统一它们是设计题，不该搭在 A 的顺风车上）；备选：同一计划内做。
- **D5 · 记录**：写 ADR + 架构页 + `CONTEXT.md` 词条（推荐；今天这条接缝没有任何 ADR，「加法通道、不重启」只在注释里）。拟加词条：**Host Projection**（宿主投影）、**Projection Document**（投影文档：模型端点 / 运行时选择）、**Operator Override**（操作者覆盖：spawn 时冻结的 env）、**Runtime Selection**（运行时选择）。

## 9. 风险与不做的事

- 风险：注入点漏改 → 某条 provider 在嵌入态突然走 legacy 链。缓解：D2 选「缺席」+ 边界测试清零 + §7.3 真机三 provider 冒烟。
- 风险：`runtimeprep` 被 daemon/runtime 引用，包依赖方向要检查 `hostprojection` 不反向依赖任何 service。
- 不做：不改 rndmaster 写入侧；不改文件 schema；不动 `Service.Environ`；不在本计划里碰 `[1m]` 启动记录（候选 #5）。
