# 2026-09-20 架构复盘：近两周提交里的「加深」候选（tutti / DinTalDock 侧）

> 复盘窗口：2026-09-06 → 09-18，本仓 `dintal-dock` 58 个非 merge 提交，与 rndmaster 的 56 个一起看。
> 方法：improve-codebase-architecture（热点 → CONTEXT.md / ADR → 只读探索 + 删除测试 → 可视化报告）。
> 词汇按 codebase-design：module / interface / seam / adapter / depth / locality / leverage。**本轮不提接口签名。**
>
> 完整报告（8 个候选、before/after 图、行号、ADR 冲突）：https://claude.ai/artifact/3scqhk5oUKsJjByDx4GJdv
> rndmaster 侧的记录在 rndmaster 仓 `docs/2026-09-20-architecture-review-deepening-candidates.md`。

## 本仓涉及的候选

### #6（Strong · 首选）宿主投影只读一次；provider 运行时在一个模块里选

- 宿主→边车实际契约：rndmaster 设置 41 个 env 名，本仓 Go 认识 163 个，交集 16 个，在 20 个非测试文件里 `os.Getenv` **现读**，`services/tuttid/cmd` 启动期 0 次读取；没有任何 HostProjection 类型。
- 直接后果：测试只能 `t.Setenv`（agentstatus 42 / runtimeprep 56 / service/agent 160 次），两周内因「随宿主环境翻转」删了 12 条（6c259ed 9 条、58ad3e7 1 条、ec742fb 2 条），另有 22 条已知红。
- 「用哪条二进制」：~40 个 resolve/select/discover 函数散在 5 个包；优先级写了 3 遍且顺序不同——claude（投影 › 内联 env › 受管 › PATH）、codex（投影 › 持久化 › 发现）、扩展（bundled › 发现 › 受管，**不看投影**）；扩展在 setup 与 launch 各解析一次，codex 在 status / ResolveProviderCommand / runtimeprep 三次。11d8d5d 的「两处刻意的差别」只活在注释与提交正文里。
- abb8c83 是在 if 链上再加一个分支（浅），不是把策略收进一处。
- `docs/conventions/troubleshooting/agent-provider-setup.md`：63 节、2581 行、两周 9 次修改，≥8 条是「provider X 起不来，因为解析选了 Y」——知识住在文档里而不在模块的 interface 上。
- 无 ADR 覆盖宿主 env 输入；「加法通道、不必重启」的意图只在注释（host_runtime_selection.go:16-25、host_model_endpoint.go:17-19）与 b3127b8 里。合法——把 env→路径映射冻结一次、文件**内容**按需重读即可同时满足。

### #5（Strong）会话启动设置按类型字段携带；只让 Claude adapter 拼 `[1m]`

- `[1m]` 从 composer 一路以字符串后缀走到 adapter，22 处决定「留 / 剥」（host 8、biz 1、runtimeprep 1、daemon 9、sidecar 1、GUI 1）；唯一的类型化落点是 `ModelEndpointConfig.ContextWindow`（1 个消费者）。
- 四份不完整的启动记录：ComposerSettings / SessionSettings / PrepareInput / sessionRuntimeSnapshot，没有一份带窗口。≈4 个选项 × 5 个 adapter ≈ 20 段手写翻译；思考档位「有哪些档」在 6 处。
- 删除测试：删掉 `contextwindow` 会冒出 11 份 `HasSuffix("[1m]")`——它集中的是**拼写**不是**决定**（interface ≈ implementation，5 个一行函数）。改为类型化窗口后 11 个调用方消失 9 个。
- 文档冲突：`contextwindow.go:1-8`「每个 provider 认同一个载体」已不成立；`deepseek-workers.md:57-60` 把类型化路径写成唯一规则，而三处代码注释说 Claude Code 必须**收到**标记；`HostModelContextWindow`（db283da）零非测试调用。

### #7（Worth exploring）composer「到底发什么」只有一个答案

- 参与者：`agentComposerDraft.ts`（848 行，hasContent + 投影过滤）、`composerDraftAttachmentReadiness.ts`（自注释「必须与投影保持一致」）、`useComposerSlashActions.ts` :469-641、`useAgentGUISubmitInteractionActions.ts` :231、`draftMessageHelpers.ts` :405-445、`useComposerDraftAttachments.ts`、`composerDraftImageUpload.ts`。
- c468a36 之后仍是 ≥3 个独立判定按顺序执行，`!uploading && !uploadError` 手写 6 处；发送按钮仍按 hasContent 点亮，于是「只有一张上传中的图」会亮按钮再 toast「什么都没发」。
- `normalizeDraftAttachmentProgress` 假定结算只翻 `uploading`，真实结算（composerDraftImageUpload.ts:22-46）还会加 attachmentId/url/path 并丢 data → url 结算后仍不清空，属可能的潜在缺陷，与 P2 同因。
- c33b878 之后超时零覆盖（其它 10 个 spec 用了 fake timers，这里没有）。ratchet 已在 HEAD 红 11 条；30d75cd 按行数切（815→726），把粘贴文本与文件准备两条同形结算路径留在原地。
- `agent-gui-node.md` :909-919 与 :2220-2228 对「上传进度是否门禁」说法相反；两份 handoff 文档都没列 draft/submission。

### #3 的边车侧（Strong，主体在 rndmaster）

rndmaster 直读 `tuttid.db` 的 `workspace_agent_*` 表并复刻 `GetLatestTurn` 的排序规则；fcee1ec 为此改了 29 个文件，把 usage 放进 `payload_json`。若 rndmaster 侧收敛为「tuttid facts」模块 + SQLite / HTTP 两个 adapter，本仓存储形状的变化只会碰那一个 adapter。

## 顺手发现

- 两对逐字重复的提交：b24e345 / faffa55、af8e184 / e66cc91。
- 本仓 `CONTEXT.md` 没有 composer / 启动设置 / 运行时选择相关词条（现有词条集中在 Workbench 与 Replay），ADR 0001-0010 也未覆盖这三处接缝。

## 下一步

挑一个候选进入 grilling；命名加深后的模块时把新词写进 `CONTEXT.md`；若否决且理由长期成立，记一条 ADR。
