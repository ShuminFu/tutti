import { Link2, Link2Off, X } from "lucide-react";
import { resolveAgentGuiSessionProviderFlatIconUrl } from "../../../agentGuiSessionProviderIconUrls";
import {
  conversationRailPeerKnownTitle,
  conversationRailPeerDisplayTitle,
  conversationRailPeerPairLinks,
  isConversationRailPeerClosedStatus,
  isConversationRailPeerPairable
} from "../model/conversationRailPeerPairing";
import type { AgentGUIConversationRailPeerPairingValue } from "./agentGUIConversationRailPeerPairingContext";

export interface ConversationRailPeerPairingMenuLabels {
  peerPairMark: string;
  peerPairUnmanaged: string;
  peerPairUnmark: string;
  peerPairUnpair: (count: number) => string;
  peerPairWith: (title: string) => string;
  peerPairWithRelaunch: (title: string) => string;
}

export interface ConversationRailPeerPairingMenuEntry {
  description?: string;
  disabled?: boolean;
  icon: React.ReactNode;
  id: string;
  label: string;
  submenu?: ConversationRailPeerPairingMenuEntry[];
  /** 条目末尾的图标（解除子菜单的 ✕）；hover 时整条变 danger 色。 */
  trailingIcon?: React.ReactNode;
  onSelect: () => void;
}

interface PeerPairingMenuConversation {
  id: string;
  isImported?: boolean;
  provider: string;
  status: string;
  title: string;
}

// 右键菜单的配对组（四态）。宿主没注册这组能力时返回空数组 —— 整组不显示，
// 老宿主的菜单形状与今天逐字一致。
export function buildConversationRailPeerPairingMenuEntries(input: {
  conversation: PeerPairingMenuConversation;
  labels: ConversationRailPeerPairingMenuLabels;
  pairing: AgentGUIConversationRailPeerPairingValue;
  run: (action: () => void) => void;
}): ConversationRailPeerPairingMenuEntry[] {
  const { conversation, labels, pairing, run } = input;
  if (!pairing.supported) {
    return [];
  }
  const links = conversationRailPeerPairLinks(pairing.index, conversation.id);
  const unpairEntry: ConversationRailPeerPairingMenuEntry = {
    // N=0 置灰，但菜单形状保持稳定（PRD：不用猜有没有这一项）。
    disabled: links.length === 0,
    icon: <Link2Off aria-hidden="true" />,
    id: "peer-unpair",
    label: labels.peerPairUnpair(links.length),
    onSelect: () => {},
    submenu: links.map((link) => ({
      icon: <PeerProviderIcon provider={link.peer.provider} />,
      id: `peer-unpair-${link.pairId}`,
      // 对端标题在真机上是整段 prompt：不剥就把子菜单撑到窗口外（T3）。
      label: conversationRailPeerDisplayTitle(link.peer.title),
      trailingIcon: (
        <X aria-hidden="true" className="agent-gui-node__conversation-peer-menu-remove" />
      ),
      onSelect: () =>
        run(() =>
          pairing.deletePair({
            pairId: link.pairId,
            peerTitle: link.peer.title,
            taskId: link.ownTaskId
          })
        )
    }))
  };

  // 非托管：标记/配对整条置灰，标签直接就是原因说明。
  if (!isConversationRailPeerPairable(conversation)) {
    return [
      {
        disabled: true,
        icon: <Link2 aria-hidden="true" />,
        id: "peer-pair-mark",
        label: labels.peerPairUnmanaged,
        onSelect: () => {}
      },
      unpairEntry
    ];
  }

  const marked = pairing.marked;
  const markSelf = (): void =>
    run(() =>
      pairing.toggleMarked({
        provider: conversation.provider,
        sessionId: conversation.id,
        status: conversation.status,
        title: conversation.title
      })
    );

  if (marked && marked.sessionId === conversation.id) {
    return [
      {
        icon: <Link2 aria-hidden="true" />,
        id: "peer-pair-unmark",
        label: labels.peerPairUnmark,
        onSelect: markSelf
      },
      unpairEntry
    ];
  }

  if (marked) {
    // 后端重开的是**被标记的那一头**（请求里的 to），所以 relaunchClosed 按 marked
    // 的 status 判、文案也点名它。被右键这条自己已结束不拦：后端接受这种配对（T4）。
    const relaunchClosed = isConversationRailPeerClosedStatus(marked.status);
    // 标题优先用后端 peer_title（配对表里认识它时），否则剥侧栏标题的模板。
    const markedTitle = conversationRailPeerDisplayTitle(
      conversationRailPeerKnownTitle(pairing.index, marked.sessionId) ??
        marked.title
    );
    return [
      {
        // 别名由后端按它自己的 provider 名生成（claude / codex），侧栏这边的 provider
        // 是 Tutti 的 id（claude-code），预览出来会对不上，所以副标题只给 provider。
        description: marked.provider,
        icon: <Link2 aria-hidden="true" />,
        id: "peer-pair-with",
        label: relaunchClosed
          ? labels.peerPairWithRelaunch(markedTitle)
          : labels.peerPairWith(markedTitle),
        onSelect: () =>
          run(() =>
            pairing.createPair({
              // from = 当前右键这条（发起方），to = 被标记那条。
              fromSessionId: conversation.id,
              fromTitle: conversation.title,
              relaunchClosed,
              toSessionId: marked.sessionId,
              toTitle: marked.title
            })
          )
      },
      unpairEntry
    ];
  }

  return [
    {
      icon: <Link2 aria-hidden="true" />,
      id: "peer-pair-mark",
      label: labels.peerPairMark,
      onSelect: markSelf
    },
    unpairEntry
  ];
}

function PeerProviderIcon({
  provider
}: {
  provider: string;
}): React.JSX.Element {
  const url = resolveAgentGuiSessionProviderFlatIconUrl(provider);
  if (!url) {
    return <Link2 aria-hidden="true" />;
  }
  return (
    <span
      aria-hidden="true"
      className="agent-gui-node__conversation-peer-menu-icon"
      style={{ WebkitMaskImage: `url("${url}")`, maskImage: `url("${url}")` }}
    />
  );
}
