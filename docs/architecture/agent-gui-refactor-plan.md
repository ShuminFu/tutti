# Agent GUI 架构收敛：仍生效的决定

本文原是 2026-07 Agent 基建重构的方案与迁移切片记录。重构已完成，逐文件清单、规模快照和验证流水可从 Git 历史读取；这里只保留**仍约束今天实现**的决定与护栏，其余归入现行文档：

- [Agent GUI Node](./agent-gui-node.md)：界面、会话引擎、宿主边界、child session 与 Fork
- [Agent Extensions](./agent-extensions.md)：扩展目标、运行时安装、setup 生命周期
- `packages/agent/gui/AGENTS.md`：包内编辑路由与硬规则
- [Agent Activity Packages](./agent-activity-packages.md)：活动域实体与投影

代码注释与 `tools/scripts` 中残留的旧小节引用对应关系：`3.3` / `3.5` 指本文件
第 1、2 节的实体边界与状态所有权；`4.1` 指第 2 节的单向数据流与迁移约束；
`5.2` / `5.3` 指第 6 节的静态预算（含 `check:agent-gui-degradation` 的 800 行
业务文件上限与渲染预算）。

## 1. 实体边界

活动域只有三个实体，派生值不得成为第二份事实：

| 实体        | 所有内容                                   | 不应包含              |
| ----------- | ------------------------------------------ | --------------------- |
| Session     | 目标、工作目录、标题、设置、当前 turn 引用 | turn 结果、待处理审批 |
| Turn        | 一次提交的阶段、结果、错误、文件变化       | 后续 turn 的状态      |
| Interaction | 审批、提问、计划确认及处理状态             | session 级展示状态    |

- 展示状态由 turn 与 interaction 推导；提交可用性由 selector 推导
- session 只引用 active turn，不复制其 phase/outcome
- interaction 是否待处理由其自身状态表达，不使用三态 `null` 补丁协议

## 2. 状态所有权

判定问题只有一个：**关闭所有 Agent GUI 面板后，该状态是否仍应存在并继续工作？**

- 是：属于 daemon 或工作区级会话引擎（队列推进、乐观提交、运行时事件对账、当前 session/turn 投影）
- 否：属于组件本地状态（滚动位置、输入焦点、临时展开状态）

单向数据流：`UI --intent--> 引擎 --command--> 桌面传输壳 --HTTP/event--> tuttid`，canonical 活动再经传输回到引擎，UI 只经 selector 读取。daemon 是业务规则与持久状态的权威；引擎是客户端时序、乐观状态与对账的唯一所有者；桌面层只做传输与宿主集成，不构成第二业务核心；React 只读取快照、派发意图、渲染，不用 effect 编排业务生命周期。

## 3. Provider 差异

传输协议不是稳定边界，归一化活动契约才是。provider 身份、运行时策略、能力和目标元数据由 descriptor/target 下发；UI 按 capability 渲染，不按 provider 名称猜行为；标准 ACP、专属协议和 SDK 边车都投影为同一活动契约；新增 provider 不得要求在 Agent GUI 内新增行为分支。

## 4. 类型与协议

OpenAPI 是跨 daemon 边界传输类型的事实源，Go/TypeScript 传输类型从契约生成；内部域类型只能通过显式投影跨层；时间、状态和身份字段使用单一表示；未知枚举必须有显式处理路径，不能用宽字符串绕开检查。

## 5. 关键状态机

```text
Turn:        submitted -> running -> waiting -> running -> settling -> settled
Interaction: pending -> answered | dismissed | superseded
Readiness:   unavailable -> needs_setup -> setting_up -> ready
                                        \-> failed
```

- `pendingIntents` 是提交乐观消息、接受、确认、结果未知、失败与到期的单一事实源；controller 不把命令完成还原为 Promise 工作流
- 激活与普通提交共用同一 prompt envelope；乐观消息准入同时认文本与可渲染的结构化 `content`
- 合成计划决策走 tuttid 语义 API 与 durable `plan_decision` saga；provider 原生 exit-plan 走 durable `interactive_response`
- 进程丢失后的未结算 turn 由 daemon 启动时统一收敛为 `settled/interrupted`
- 每个 interaction 有独立 ID、类型、状态和所属 turn；回答幂等；新 turn 按规则取代失效 interaction；UI 只显示 selector 给出的 pending interaction
- built-in provider 的 managed-environment wizard 只服务其拥有的内建 provider；Agent Extension setup 属于 Agent Target 生命周期，由 daemon 持久安装/setup 状态驱动；provider 名称不得成为 readiness/setup 分支条件

## 6. 静态护栏

保留以下**只降不升**预算：文件长度、React effect/ref 数量、provider 名称分支、view props 面积、组件内 store 创建。预算下降时可更新 baseline；预算上升必须先修源头，不能通过抬 baseline 合并。
