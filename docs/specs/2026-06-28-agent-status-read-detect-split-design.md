# Agent Provider Status: 读/检测分离（未完成设计）

状态：**设计待评审，尚未按此实现**。当前 `agentstatus.Service.List` 仍会在每次调用时现场探测（`--version`、registry/proxy/provider HTTP、adapter），只是通过 `detection_cache.go` 的指纹缓存和 `cachedStatusForSpec` 的 TTL 缩短了重复探测。下面的分离方案仍是对该问题的正解，实现前不要假定端点语义已经翻转。

## 问题

桌面端安装中每秒轮询进度，而进度只能从 `GetAgentProviderStatuses`（= `agentstatus.Service.List`）的 `activeAction` 字段拿到；`List` 把状态建模成按需计算的纯函数，于是**读 = 检测**，高频轮询被迫反复探网络，网络不稳时闪红并触发整轨重检测。

## 不同信号温度不同

| 信号                   | 何时变        | 本质                        |
| ---------------------- | ------------- | --------------------------- |
| 已装 CLI/adapter、版本 | 仅安装/卸载时 | 事件驱动（daemon 自己知道） |
| 登录态                 | 仅登录/登出   | 事件驱动                    |
| 网络可达性             | 偶发、外部    | 按需探测、可缓存            |
| 安装进度 activeAction  | 安装中持续    | 流                          |

把四种温度不同的东西塞进一个同步现算调用，是当前设计的代价来源。

## 目标设计

**状态是 daemon 维护的模型（single source of truth），客户端观察它；读是廉价默认，检测是显式命令。**

- **读（`GetStatus`）**：读模型 + 叠加实时 `activeAction`，不探测任何东西；高频、可轮询、不置 `isLoading`。
- **检测（`Detect`）**：探网络并复核已装/登录态，然后**更新模型**；低频，只在打开 / 「重新检测」/ 动作收尾时执行。今天的 `List` 语义原样搬到这里再加一行写模型。
- 网络是模型里被探测的字段，读直接返回上次探到的稳定值；冷模型（尚未 Detect）返回空 `Providers`，由前端 reconcile 保留既有快照。
- 端点应对名称诚实：`GetAgentProviderStatuses` 改为廉价读，新增命令端点（如 `POST /agent-providers/detect`）承担探测，并复用现有响应 schema。

## 已核实的约束

- `agentstatus.Service` 是值类型，会被按值拷贝甚至 new 出空实例，因此状态模型必须放**包级全局**（与 `active_action.go` 同模式），不能挂 `Service` 字段；daemon 长生命周期，包级模型跨请求存活。
- `GetAgentProviderStatuses` / `Probe` 是 desktop↔daemon 本地 API，无外部消费者；当前 `List` 只有前端 `desktopAgentProviderStatusService` 与 daemon 内部 `provider_availability.go` 两个调用方，端点语义翻转可控，内部调用方需重指到 `Detect`。
- 并发读写用 mutex；read 永不得触发 Detect。
- 前端 `desktopAgentProviderStatusService` 需要拆成 detect 路径（置 `isLoading`）与 read 路径（不置 `isLoading`），安装中的进度轮询走 read。

## 未来方向（不在本设计内）

在状态模型上加 `Watch` 订阅、把模型变化推给前端，可彻底取消轮询；本设计的模型即其天然基座。登录态轮询（`loginStatusPoll`）当前仍走检测，待模型铺好后再改。
