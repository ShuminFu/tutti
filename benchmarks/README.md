# 性能测试 / Benchmarks

此目录是仓库性能测试的统一入口。新增跨模块性能场景与负载夹具放在这里；现有同包 Go benchmark 通过本目录运行，保留其私有接口和测试辅助函数依赖。

```sh
bash benchmarks/run.sh --list
bash benchmarks/run.sh <suite>
```

必须显式选择套件；默认只列目录，不启动服务或运行测试。Go 套件使用 `-run '^$'` 跳过普通测试，以 `-benchmem -count=5` 输出耗时、分配与用例自定义指标；命令失败保留非零退出码。底层测试的前置条件、平台限制和 skip 仍有效，跳过不代表性能通过。

## 场景交付要求

每个性能场景写清：业务路径、输入规模 / 并发、准备与清理是否计时、吞吐 / 延迟 / 内存等指标、依赖和运行条件。对照同一机器、工具链和负载下的基线重复采样；没有基线或预算时只报告数据，不声明达标。正确性断言不可省略，避免把少做工作或空结果误认为加速。

原始输出、环境信息和对比结果可保存在本目录被忽略的 `results/` 中，长期约定与稳定阈值随场景源码维护。不要提交真实账号数据或一次运行报告。

## 现有实现

| 源码                                                                                                                                  | Benchmark                                                     |
| ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| [packages/agent/store-sqlite/activity_sections_benchmark_test.go](../packages/agent/store-sqlite/activity_sections_benchmark_test.go) | `BenchmarkStoreListSessionSectionsLargeRemovedProjectHistory` |
| [packages/agent/store-sqlite/generated_files_bench_test.go](../packages/agent/store-sqlite/generated_files_bench_test.go)             | `BenchmarkStoreListWorkspaceGeneratedFileTurns`               |
| [packages/agent/store-sqlite/migrations_session_titles_test.go](../packages/agent/store-sqlite/migrations_session_titles_test.go)     | `BenchmarkSessionTitleMigrationBackfillsLargeHistory`         |

GUI 的负载、指标、隔离行为与阈值策略见 [Testing](../docs/conventions/testing.md#local-performance-reports)。`agent-gui` 使用现有性能 runner，不属于稳定的跨设备 benchmark，也不会自动纳入 CI。

AgentGUI 性能 runner、场景与专属夹具位于 [`agent-gui/`](agent-gui/)，`pnpm perf:agent-gui` 直接指向这里；trace 采集、分析和启动等通用工具仍由 `tools/scripts/` 维护。工具自身的单元测试保留在原工具测试发现范围。
