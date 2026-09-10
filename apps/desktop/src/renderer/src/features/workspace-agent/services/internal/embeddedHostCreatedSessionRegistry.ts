// 嵌入态由宿主代建会话（补丁 0099）时，宿主铸的 agentSessionId 与本客户端发起
// 激活时用的 id 不是同一个。会话栏的补刷计划靠「待激活记录转正」认新会话
// （`previousPendingIds.has(id)`），id 一换就认不出来，会被当成「用户选中后拉
// 详情」而跳过，那条会话所在的分组页从此再也不重取。
//
// 这里只记「刚才是我建的」这一个事实，交给 rail 的 isLocallyCreatedSession 用。

// 只需覆盖「建完到分组页刷完」这一小段，留最近若干条即可；FIFO 有界，避免长
// 会话里无限增长。
const RECENT_LIMIT = 32;

const recentHostCreatedSessionIds: string[] = [];
const recentHostCreatedSessionIdSet = new Set<string>();

// 回退看红：保留符号，设为 false 后不再登记，「宿主代建的会话进 Chats 分组」必须转红。
export const recordEmbeddedHostCreatedSessions = true;

export function noteEmbeddedHostCreatedSession(agentSessionId: string): void {
  if (!recordEmbeddedHostCreatedSessions) return;
  const id = agentSessionId.trim();
  if (!id || recentHostCreatedSessionIdSet.has(id)) return;
  recentHostCreatedSessionIds.push(id);
  recentHostCreatedSessionIdSet.add(id);
  while (recentHostCreatedSessionIds.length > RECENT_LIMIT) {
    const evicted = recentHostCreatedSessionIds.shift();
    if (evicted !== undefined) recentHostCreatedSessionIdSet.delete(evicted);
  }
}

export function isEmbeddedHostCreatedSession(agentSessionId: string): boolean {
  return recentHostCreatedSessionIdSet.has(agentSessionId.trim());
}

export function resetEmbeddedHostCreatedSessionsForTest(): void {
  recentHostCreatedSessionIds.length = 0;
  recentHostCreatedSessionIdSet.clear();
}
