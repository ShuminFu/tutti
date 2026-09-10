package agentruntime

// RNDMASTER_MANAGED_CONTRACT_MCP 是「合同 mcpServers 覆盖到全部 standard-acp 目标」的
// 托管源码标记。此前只有 cursor 与 extension:* 目标会读 RnDMaster 运行时合同，
// local:claude-code / local:codex 这类托管目标拿到的是空合同，于是 session/new 的
// mcpServers 永远只有 DinTalDock 自己的 connector，后端那台 stdio MCP
// （peer_send / peer_list / workflow_report）在会话里根本不存在。
const RNDMASTER_MANAGED_CONTRACT_MCP = "RNDMASTER_MANAGED_CONTRACT_MCP"

func rndmasterManagedContractMCP() bool {
	return RNDMASTER_MANAGED_CONTRACT_MCP != ""
}

// rndmasterContractMCPOnlyACPSession 只把合同里的 mcpServers 取出来交给
// mergeFilteredContractMCP，session 本身原样返回。
//
// 刻意不叠 env / cwd / model / systemPrompt：托管 claude 的登录凭据（宿主托管令牌）、
// 模型网关与提示词都由 rndmaster 后端在别处注入，这里再覆盖一层会把它们顶掉
// （合同里的 ANTHROPIC_AUTH_TOKEN 会把 claude.ai 登录换成中转站）。返回值只带
// MCPConfig，让「只合并 MCP」这件事在类型上就成立，而不是靠调用方自觉。
func rndmasterContractMCPOnlyACPSession(session Session) (Session, rndmasterRuntimeContract, error) {
	if !rndmasterManagedContractMCP() {
		return session, rndmasterRuntimeContract{}, nil
	}
	contract, err := rndmasterContractFromSession(session)
	if err != nil {
		return Session{}, rndmasterRuntimeContract{}, err
	}
	return session, rndmasterRuntimeContract{MCPConfig: contract.MCPConfig}, nil
}
