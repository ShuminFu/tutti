# 行为 / 集成测试

此目录是仓库行为和集成场景的统一入口。新增跨模块黑盒场景、可复用夹具和运行说明放在这里；单元测试与实现相邻。依赖同包私有接口或模块环境的现有测试保留实现位置，由此目录提供执行入口。

```sh
bash integration-tests/run.sh --list
bash integration-tests/run.sh <suite>
```

必须显式选择套件；默认只列目录。套件的前置条件、平台限制与 skip 仍有效，退出码由底层 runner 返回。真实宿主 / GUI、外部服务和本机数据相关场景仅在明确选择对应套件后运行。DinTalDock daemon 套件的 builtin assets 准备遵循现有 daemon 指南，本入口不自动构建。

用例应描述业务行为，断言可观察结果，覆盖正常流程及关键失败 / 恢复路径；数据隔离与清理由夹具负责。不得只有调用或截图而没有结果断言。运行输出放入被忽略的 `results/`，不以一次验收记录替代测试源码。

性能场景与度量入口见 [benchmarks](../benchmarks/README.md)。

## 套件

| 名称                | 工作目录                               | 行为与前置条件                                                           |
| ------------------- | -------------------------------------- | ------------------------------------------------------------------------ |
| `activity-eventhub` | `integration-tests`                    | SQLite 活动投影、事件发布与恢复集成；先按 daemon 指南准备 builtin assets |
| `agent-recovery`    | `services/tuttid`                      | 会话初始化、持久化与重启恢复；先准备 builtin assets                      |
| `device-authority`  | `packages/clients/device-authority-go` | 设备授权与响应丢失后的重试                                               |
| `agent-gui`         | `.`                                    | AgentGUI replay；真实 GUI 环境，仅显式选择运行                           |

`tuttid/` 是独立 Go workspace 测试模块，在根 `go.work` 注册，现有 `pnpm test:go` / `pnpm test:go:prepared` 会自动发现。它通过模块公开 API 测试跨层行为，不属于任何生产模块的包内实现。

## 保留包内的现有集成实现

以下套件按源文件中已有测试名称选择执行，不扩展为整包单元测试。涉及真实进程、服务、账号或平台能力的前置条件仍以源文件为准；skip 不视为验收通过。

| 实现                                                                                                                                                                        | 根目录套件名                              |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------- |
| [packages/clients/device-authority-go/client_integration_test.go](../packages/clients/device-authority-go/client_integration_test.go)                                       | `go-packages-clients-device-authority-go` |
| [services/tuttid/service/agent/canonical_session_initialization_integration_test.go](../services/tuttid/service/agent/canonical_session_initialization_integration_test.go) | `go-services-tuttid-service-agent`        |
| [services/tuttid/service/agent/claude_session_fork_integration_test.go](../services/tuttid/service/agent/claude_session_fork_integration_test.go)                           | `go-services-tuttid-service-agent`        |
| [services/tuttid/service/agent/commit_replay_integration_test.go](../services/tuttid/service/agent/commit_replay_integration_test.go)                                       | `go-services-tuttid-service-agent`        |
| [services/tuttid/service/agent/host_goal_recovery_integration_test.go](../services/tuttid/service/agent/host_goal_recovery_integration_test.go)                             | `go-services-tuttid-service-agent`        |
| [services/tuttid/service/computer/adapter_integration_test.go](../services/tuttid/service/computer/adapter_integration_test.go)                                             | `go-services-tuttid-service-computer`     |
| [services/tuttid/service/computer/windows_integration_test.go](../services/tuttid/service/computer/windows_integration_test.go)                                             | `go-services-tuttid-service-computer`     |
| [services/tuttid/service/mcpapp/integration_test.go](../services/tuttid/service/mcpapp/integration_test.go)                                                                 | `go-services-tuttid-service-mcpapp`       |
