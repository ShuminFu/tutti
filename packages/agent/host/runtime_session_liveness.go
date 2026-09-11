package agenthost

// 补丁 0125：把「这条会话现在有没有活的 ACP 进程」这条判据公开出去。
//
// 这条判据本来只在 Host 内部用（Goal 代次围栏、运行操作收尾），底下是
// hostadapter.RuntimeController.RuntimeSessionLive → daemon runtime
// Controller.HasLiveSession。它是**当下这一刻**的观察：空闲回收
// （ReleaseIdleLiveSessions）放掉 provider 进程时既不改会话状态、也不发事件，
// 所以除了主动来问，外面没有别的办法知道进程已经没了。
//
// Runtime 没实现 RuntimeSessionLiveness 时一律回 false（与内部判据同语义）。
func (h *Host) RuntimeSessionLive(workspaceID, agentSessionID string) bool {
	return h.runtimeSessionLive(workspaceID, agentSessionID)
}
