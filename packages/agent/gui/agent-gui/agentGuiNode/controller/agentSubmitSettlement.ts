import {
  selectEngineHasVisibleQueuedSubmit,
  selectPendingSubmitsForSession,
  type AgentSessionEngine,
  type AgentSessionEngineState
} from "@tutti-os/agent-activity-core";

/** 兜底上限：排队很久的提交也总会被引擎的确认超时收掉，这里只防订阅泄漏。 */
const AGENT_SUBMIT_SETTLEMENT_MAX_WAIT_MS = 10 * 60 * 1000;

type SettlementVerdict = "accepted" | "rejected" | "waiting";

/**
 * 读一拍引擎状态，判这次提交的结局。判据与 AgentGUIEngineSettlementController
 * 恢复草稿那段同源：
 *   - accepted / confirmed → 接受；
 *   - failed 但仍在可见队列里 → 还没定（排队重试中）；
 *   - failed → 拒绝；
 *   - 记录消失：之前见过它就算被撤回 / 丢弃（accepted 之后才会被清理，
 *     那一拍我们一定先看到了 accepted）；从没见过也算拒绝。
 */
function readSettlement(
  state: AgentSessionEngineState,
  agentSessionId: string,
  clientSubmitId: string
): SettlementVerdict {
  const record = selectPendingSubmitsForSession(state, agentSessionId).find(
    (candidate) => candidate.clientSubmitId === clientSubmitId
  );
  if (!record) return "rejected";
  if (record.status === "accepted" || record.status === "confirmed") {
    return "accepted";
  }
  if (record.status === "failed") {
    return selectEngineHasVisibleQueuedSubmit(
      state,
      agentSessionId,
      clientSubmitId
    )
      ? "waiting"
      : "rejected";
  }
  return "waiting";
}

/**
 * 等一次提交被引擎定论：true = 接受，false = 没发出去 / 被拒 / 被撤回。
 *
 * 分栏结对模式（peer-pair-mode 票 05，PRD D4）靠它决定「开工卡能不能给搭档投」：
 * 发送栏那句没被接受，搭档就不该先收到卡。
 */
export function waitForAgentSubmitSettlement(
  engine: Pick<AgentSessionEngine, "getSnapshot" | "subscribe">,
  agentSessionId: string,
  clientSubmitId: string,
  maxWaitMs = AGENT_SUBMIT_SETTLEMENT_MAX_WAIT_MS
): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    let done = false;
    let unsubscribe: (() => void) | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const finish = (accepted: boolean): void => {
      if (done) return;
      done = true;
      unsubscribe?.();
      if (timer !== null) clearTimeout(timer);
      resolve(accepted);
    };
    const check = (state: AgentSessionEngineState): void => {
      const verdict = readSettlement(state, agentSessionId, clientSubmitId);
      if (verdict !== "waiting") finish(verdict === "accepted");
    };
    check(engine.getSnapshot());
    if (done) return;
    unsubscribe = engine.subscribe((state) => check(state));
    timer = setTimeout(() => finish(false), maxWaitMs);
  });
}
