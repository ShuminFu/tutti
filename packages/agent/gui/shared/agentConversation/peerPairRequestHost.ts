// 配对请求就地审批（补丁 0116）的宿主端口注册口（形状照 0103 的 conversationRailPeerPairingHost）。
//
// agent 调 `peer_pair_request` 后请求只进研发大师收件箱，用户得切窗口去批。这里让
// DinTalDock 对话里的作曲区上方直接出一张审批卡。iframe 不直连 cliagent-backend：
// apps/desktop 在嵌入 DinTalDock 时注册一份走 tutti-host-request 桥的实现；
// 未注册（普通 web / 老宿主）时整张卡不出现。

export interface PeerPairRequestSide {
  taskId: string;
  sessionId: string;
  provider: string;
  alias: string;
  title: string;
  cwd: string;
  status: string;
}

export interface PeerPairRequestView {
  id: string;
  status: string;
  reason: string;
  createdAt: string;
  /** 发起方（= 当前会话，宿主已按 agentSessionId 过滤）。 */
  from: PeerPairRequestSide;
  /** 想配对的对端。 */
  to: PeerPairRequestSide;
}

export type PeerPairRequestDecision = "approve" | "reject";

export interface PeerPairRequestHost {
  listPendingPeerPairRequests(input: {
    agentSessionId: string;
  }): Promise<{ requests: PeerPairRequestView[] }>;
  decidePeerPairRequest(input: {
    requestId: string;
    decision: PeerPairRequestDecision;
  }): Promise<{ request: PeerPairRequestView }>;
}

let registeredHost: PeerPairRequestHost | null = null;

export function registerPeerPairRequestHost(
  host: PeerPairRequestHost
): () => void {
  registeredHost = host;
  return () => {
    if (registeredHost === host) {
      registeredHost = null;
    }
  };
}

export function peerPairRequestHost(): PeerPairRequestHost | null {
  return registeredHost;
}

// 卡片上「要和谁配对」的名字：配对别名 > 会话标题 > provider · 会话号前 8 位。
export function peerPairRequestSideLabel(side: PeerPairRequestSide): string {
  if (side.alias.trim()) return side.alias.trim();
  if (side.title.trim()) return side.title.trim();
  const short = (side.sessionId || side.taskId).slice(0, 8);
  return [side.provider, short].filter(Boolean).join(" · ") || "peer";
}
