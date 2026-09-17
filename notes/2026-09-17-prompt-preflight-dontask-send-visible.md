# 2026-09-17 发送被弹回：图片预检 + dontAsk + 失败可见

针对会话 `23de012f` 当天两条快速拒绝路径：

1. 活会话被 live reaper 释放后，带图发送在 `ValidatePromptContent` 被拒，错误是 `agent prompt image input is unsupported`。预检原先只看 adapter 内存里的活实例；声明了 `imageInput` 的 Claude Code 因此在 resume 之前就被挡掉。
2. 作曲区能选 `dontAsk`，但 claude-agent-acp 0.78.0 没有这个档位。`failOnSetModeError: true` 让 `session/set_mode` 失败直接 abort。

## 这次怎么改

- 图片预检与 `promptImage` 初始化同源：先看 composer 声明能力，没有活会话也不再误判。
- 从 claude-code 作曲档位摘掉 `dontAsk`。存量值映射到 `default`（逐条问）。未知档位在 ACP 上仍然直接失败，不降级成 WARN。
- 当前会话发送失败时，除了把草稿填回作曲区，还会写会话详情错误。图片不支持和权限档位不可用走专用文案。

## 验收

- 单测覆盖：无活会话的声明图片预检、dontAsk 映射、未知档位在 Start/ApplyPermissionMode 拦截、失败可见（含 `invalid_request` + `reason` 信封）。
- 同行评审后已收回：`set_mode` 超时不再标成权限档位错误；提交记录带上协议 `reason`。
- 真机仍需用含本提交的 Dock/tuttid：对一条已被回收的 Claude 会话发带图消息应能 resume；选不到 dontAsk；未知档位应看到错误而不是静默回填。
