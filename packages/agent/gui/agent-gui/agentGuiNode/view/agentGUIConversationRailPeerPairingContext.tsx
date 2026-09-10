import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import {
  conversationRailPeerDisplayTitle,
  conversationRailPeerPairIndex,
  type ConversationRailPeerPair,
  type ConversationRailPeerPairIndex
} from "../model/conversationRailPeerPairing";
import {
  conversationRailPeerPairingHost,
  type ConversationRailPeerPairingHost
} from "../model/conversationRailPeerPairingHost";

/** 被标记那条的最小身份：菜单副标题要 provider 与别名预览，标题要给人看。 */
export interface AgentGUIConversationRailPeerMark {
  provider: string;
  sessionId: string;
  /**
   * 被标记那条的会话状态。后端重开的是**被标记的这一头**（配对请求里的 to），
   * 所以 relaunchClosed 必须按它判，不是按被右键那条（T4）。
   */
  status: string;
  title: string;
}

export interface AgentGUIConversationRailPeerPairingValue {
  /** iframe 内存态的单个「待配对」标记（刷新即清）。 */
  marked: AgentGUIConversationRailPeerMark | null;
  index: ConversationRailPeerPairIndex;
  pairs: readonly ConversationRailPeerPair[];
  /** 宿主没注册这三个能力时为 false：整组配对菜单项与徽标都不出现。 */
  supported: boolean;
  createPair(input: {
    fromSessionId: string;
    fromTitle: string;
    relaunchClosed: boolean;
    toSessionId: string;
    toTitle: string;
  }): void;
  deletePair(input: {
    pairId: string;
    /** 只用于成功 toast 点名解的是谁；不进桥请求。 */
    peerTitle?: string;
    taskId: string;
  }): void;
  toggleMarked(mark: AgentGUIConversationRailPeerMark): void;
}

const DISABLED_VALUE: AgentGUIConversationRailPeerPairingValue = {
  createPair: () => {},
  deletePair: () => {},
  index: new Map(),
  marked: null,
  pairs: [],
  supported: false,
  toggleMarked: () => {}
};

const AgentGUIConversationRailPeerPairingContext =
  createContext<AgentGUIConversationRailPeerPairingValue>(DISABLED_VALUE);

export function AgentGUIConversationRailPeerPairingProvider({
  children,
  value
}: {
  children: React.ReactNode;
  value: AgentGUIConversationRailPeerPairingValue;
}): React.JSX.Element {
  return (
    <AgentGUIConversationRailPeerPairingContext.Provider value={value}>
      {children}
    </AgentGUIConversationRailPeerPairingContext.Provider>
  );
}

// 默认值是「不支持」：没有 Provider 的既有渲染路径（含旧单测）与今天完全一致。
export function useAgentGUIConversationRailPeerPairing(): AgentGUIConversationRailPeerPairingValue {
  return useContext(AgentGUIConversationRailPeerPairingContext);
}

export interface AgentGUIConversationRailPeerPairingLabels {
  peerPairFailed: string;
  peerPairPaired: (from: string, to: string) => string;
  peerUnpaired: (title: string) => string;
  peerUnpairFailed: string;
}

// 侧栏的配对状态机：拉配对表、记标记、建/解并报 toast。
// 刷新时机：挂载、当前会话变化、建/解成功后；失败**不清缓存**（保留上次的徽标）。
export function useAgentGUIConversationRailPeerPairingState(input: {
  activeConversationId: string | null;
  labels: AgentGUIConversationRailPeerPairingLabels;
  host?: ConversationRailPeerPairingHost | null;
}): AgentGUIConversationRailPeerPairingValue {
  const agentHostApi = useOptionalAgentHostApi();
  const explicitHost = input.host;
  const host = explicitHost === undefined ? conversationRailPeerPairingHost() : explicitHost;
  const [pairs, setPairs] = useState<readonly ConversationRailPeerPair[]>([]);
  const [supported, setSupported] = useState(Boolean(host));
  const [marked, setMarked] = useState<AgentGUIConversationRailPeerMark | null>(
    null
  );
  const hostRef = useRef(host);
  hostRef.current = host;

  const refresh = useCallback(() => {
    const activeHost = hostRef.current;
    if (!activeHost) {
      setSupported(false);
      return;
    }
    void activeHost
      .listPeerPairs()
      .then((result) => {
        setSupported(true);
        setPairs(Array.isArray(result?.pairs) ? result.pairs : []);
      })
      .catch((error: unknown) => {
        // 宿主未注册这组能力（unsupported）→ 整组 UI 隐身，老宿主照旧可用。
        // 其他错误只是这一轮没拿到，缓存留着，不把已画出来的徽标抹掉。
        if (isUnsupportedHostError(error)) {
          setSupported(false);
        }
      });
  }, []);

  // 挂载 + 当前会话变化都重新拉一次（agent / 自动化建的配对也能看到）。
  useEffect(() => {
    refresh();
  }, [input.activeConversationId, refresh]);

  // 单标记：再标一条就替换；点同一条就取消。
  const toggleMarked = useCallback((mark: AgentGUIConversationRailPeerMark) => {
    setMarked((current) =>
      current?.sessionId === mark.sessionId ? null : mark
    );
  }, []);

  const createPair = useCallback(
    (createInput: {
      fromSessionId: string;
      fromTitle: string;
      relaunchClosed: boolean;
      toSessionId: string;
      toTitle: string;
    }) => {
      const activeHost = hostRef.current;
      if (!activeHost) return;
      void activeHost
        .createPeerPair({
          aliasForFrom: "",
          aliasForTo: "",
          from: createInput.fromSessionId,
          relaunchClosed: createInput.relaunchClosed,
          to: createInput.toSessionId
        })
        .then(() => {
          // 成功：清标记 → 重新 listPeerPairs → toast。
          // 带 relaunchedSessionId 时不需要特判：重开出来的新会话就是配对表里的
          // 那一头，下一次 listPeerPairs 一定带上它，徽标与吸附随之出现。若侧栏
          // 的会话列表还没收到这条新会话，允许再下一轮刷新时才吸附。
          setMarked(null);
          refresh();
          agentHostApi?.toast?.success?.(
            input.labels.peerPairPaired(
              conversationRailPeerDisplayTitle(createInput.fromTitle),
              conversationRailPeerDisplayTitle(createInput.toTitle)
            )
          );
        })
        .catch((error: unknown) => {
          // 后端拒绝时把中文文案原样进 toast。
          agentHostApi?.toast?.error?.(
            hostErrorMessage(error) || input.labels.peerPairFailed
          );
        });
    },
    [agentHostApi, input.labels, refresh]
  );

  const deletePair = useCallback(
    (deleteInput: { pairId: string; peerTitle?: string; taskId: string }) => {
      const activeHost = hostRef.current;
      if (!activeHost) return;
      void activeHost
        .deletePeerPair({
          pairId: deleteInput.pairId,
          taskId: deleteInput.taskId
        })
        .then(() => {
          refresh();
          // 解除也要有回执：没有 toast 时唯一的反馈是徽标数字变小，真机上看不出来（T6）。
          agentHostApi?.toast?.success?.(
            input.labels.peerUnpaired(
              conversationRailPeerDisplayTitle(deleteInput.peerTitle)
            )
          );
        })
        .catch((error: unknown) => {
          agentHostApi?.toast?.error?.(
            hostErrorMessage(error) || input.labels.peerUnpairFailed
          );
        });
    },
    [agentHostApi, input.labels, refresh]
  );

  const index = useMemo(() => conversationRailPeerPairIndex(pairs), [pairs]);

  return useMemo(
    () => ({
      createPair,
      deletePair,
      index,
      marked,
      pairs,
      supported: supported && Boolean(host),
      toggleMarked
    }),
    [
      createPair,
      deletePair,
      host,
      index,
      marked,
      pairs,
      supported,
      toggleMarked
    ]
  );
}

function isUnsupportedHostError(error: unknown): boolean {
  return hostErrorMessage(error).trim().toLowerCase() === "unsupported";
}

function hostErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : `${error ?? ""}`;
}
