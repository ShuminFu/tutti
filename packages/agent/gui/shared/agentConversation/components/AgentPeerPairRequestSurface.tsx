import { useEffect, useState, type JSX } from "react";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { translate } from "../../../i18n/index";
import {
  peerPairRequestHost,
  peerPairRequestSideLabel,
  type PeerPairRequestDecision,
  type PeerPairRequestView
} from "../peerPairRequestHost";
import {
  InteractiveOptionSpinner,
  interactivePromptCardClassName,
  interactivePromptClassName
} from "./interactivePromptPresentation";

// RNDMASTER_PEER_PAIR_REQUEST_INLINE_APPROVAL：回退看红时改 false，保留本符号。
const peerPairRequestInlineApproval = true;

// 请求是 agent 在本会话里发的，用户多半正盯着这条对话，4s 一拍够快；页面不可见时不拍。
const POLL_INTERVAL_MS = 4000;

export interface AgentPeerPairRequestSurfaceProps {
  agentSessionId: string | null | undefined;
  /** 作曲区上方已有 agent 的交互提示时让位（同一块浮层位置）。 */
  suppressed?: boolean;
  embedded?: boolean;
  edgeGlow?: boolean;
}

/**
 * 配对请求就地审批卡（补丁 0116）：列出**本会话发起**的 pending 配对请求，
 * 就地批准 / 拒绝。与研发大师收件箱是同一条后端记录，谁先批都行。
 */
export function AgentPeerPairRequestSurface({
  agentSessionId,
  suppressed = false,
  embedded = true,
  edgeGlow = true
}: AgentPeerPairRequestSurfaceProps): JSX.Element | null {
  "use memo";
  const host = peerPairRequestHost();
  const sessionId = agentSessionId?.trim() ?? "";
  const enabled = peerPairRequestInlineApproval && host !== null && sessionId !== "";
  const [requests, setRequests] = useState<PeerPairRequestView[]>([]);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshTick, setRefreshTick] = useState(0);

  useEffect(() => {
    if (!enabled || !host) {
      setRequests([]);
      return;
    }
    let cancelled = false;
    let unsupported = false;
    const tick = async (): Promise<void> => {
      if (cancelled || unsupported) return;
      try {
        const result = await host.listPendingPeerPairRequests({
          agentSessionId: sessionId
        });
        if (!cancelled) setRequests(result.requests);
      } catch (cause) {
        // 宿主没这组能力（老宿主）：停拍，卡片保持不出现。
        if (cause instanceof Error && cause.message === "unsupported") {
          unsupported = true;
        }
      }
    };
    void tick();
    const timer = window.setInterval(() => {
      if (document.visibilityState !== "hidden") void tick();
    }, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [enabled, host, sessionId, refreshTick]);

  if (!enabled || !host || suppressed || requests.length === 0) return null;

  const decide = async (
    requestId: string,
    decision: PeerPairRequestDecision
  ): Promise<void> => {
    if (busyId !== null) return;
    setBusyId(requestId);
    setError(null);
    try {
      await host.decidePeerPairRequest({ requestId, decision });
      setRequests((previous) =>
        previous.filter((request) => request.id !== requestId)
      );
    } catch (cause) {
      // 收件箱那边先批过（后端 409 request_decided）也会落到这里：给出原因，并
      // 立刻重拉一遍，已决的那条会从列表里消失。
      setError(cause instanceof Error ? cause.message : String(cause));
      setRefreshTick((value) => value + 1);
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div
      className="flex min-w-0 flex-col gap-2"
      data-testid="agent-peer-pair-request-surface"
    >
      {requests.map((request) => {
        const busy = busyId === request.id;
        return (
          <section
            key={request.id}
            className={interactivePromptClassName(embedded)}
            data-agent-interaction-kind="peer-pair-request"
            data-testid={`agent-peer-pair-request-${request.id}`}
          >
            <div className={interactivePromptCardClassName(edgeGlow)}>
              <div className={styles.interactivePromptLead}>
                {translate("agentHost.agentGui.peerPairRequestLead", {
                  peer: peerPairRequestSideLabel(request.to)
                })}
              </div>
              {request.reason.trim() ? (
                <div
                  className={styles.interactivePromptQuestion}
                  data-testid="agent-peer-pair-request-reason"
                >
                  {request.reason.trim()}
                </div>
              ) : null}
              <div className={styles.interactivePromptOptions}>
                <button
                  type="button"
                  className={styles.interactiveOptionButton}
                  data-testid={`agent-peer-pair-request-${request.id}-approve`}
                  disabled={busyId !== null}
                  onClick={() => void decide(request.id, "approve")}
                >
                  <span className={styles.interactiveOptionContent}>
                    <span className={styles.interactiveOptionTitle}>
                      {translate("agentHost.agentGui.peerPairRequestApprove")}
                    </span>
                    <span className={styles.interactiveOptionDescription}>
                      {translate(
                        "agentHost.agentGui.peerPairRequestApproveHint"
                      )}
                    </span>
                  </span>
                  {busy ? <InteractiveOptionSpinner /> : null}
                </button>
                <button
                  type="button"
                  className={styles.interactiveOptionButton}
                  data-testid={`agent-peer-pair-request-${request.id}-reject`}
                  disabled={busyId !== null}
                  onClick={() => void decide(request.id, "reject")}
                >
                  <span className={styles.interactiveOptionContent}>
                    <span className={styles.interactiveOptionTitle}>
                      {translate("agentHost.agentGui.peerPairRequestReject")}
                    </span>
                    <span className={styles.interactiveOptionDescription}>
                      {translate(
                        "agentHost.agentGui.peerPairRequestRejectHint"
                      )}
                    </span>
                  </span>
                </button>
              </div>
              {error ? (
                <div
                  className="px-1 pt-1 text-[12px] leading-4 text-[var(--text-danger,#d64545)]"
                  data-testid="agent-peer-pair-request-error"
                >
                  {error}
                </div>
              ) : null}
            </div>
          </section>
        );
      })}
    </div>
  );
}
