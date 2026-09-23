// 分栏布局 + 「这一对上次怎么摆」的耐久存储（rndmaster 票 03/04）。
//
// 为什么不能只写 localStorage：嵌入 DinTalDock 的 iframe origin 是**随机回环端口**，
// 宿主每次重启都换一个，浏览器侧存储跟着 origin 一起丢 —— 用户看到的就是「退出
// 重进分栏没了」。形状照 embeddedComposerModelHistoryHost：宿主按 target 落盘，
// 第一次读时把当前 origin 里的老值播种过去；宿主没这个能力（普通 web / 老宿主）
// 时整体退回 localStorage，行为与补丁前逐字一致。
import {
  loadSplitLayout,
  saveSplitLayout,
  type SplitLayoutState
} from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  HostBridgeUnavailableError,
  requestHostSplitLayout,
  requestHostUpdateSplitLayout,
  type HostSplitLayoutPanes,
  type HostSplitPairOrderEntry
} from "../../../platform/desktop/web/webHostBridgeClient.ts";

export type SplitPairOrderRecord = Record<string, HostSplitPairOrderEntry>;

export interface EmbeddedSplitLayoutSnapshot {
  layout: SplitLayoutState | null;
  pairOrder: SplitPairOrderRecord;
}

export interface EmbeddedSplitLayoutStore {
  read(): Promise<EmbeddedSplitLayoutSnapshot>;
  /** 即发即忘：布局写失败不该挡住界面，下一次操作会再写一遍。 */
  write(snapshot: EmbeddedSplitLayoutSnapshot): void;
}

/** 两条会话号排序后连起来，作为「这一对」的 key。左右顺序存在值里，不在 key 里。 */
export function splitPairOrderKey(a: string, b: string): string {
  return [a.trim(), b.trim()].sort().join("|");
}

/** 记多了没意义（都是很久以前那一对），按 usedAt 只留最近这些条。 */
const SPLIT_PAIR_ORDER_MAX_ENTRIES = 50;

export function trimSplitPairOrder(
  record: SplitPairOrderRecord
): SplitPairOrderRecord {
  const entries = Object.entries(record);
  if (entries.length <= SPLIT_PAIR_ORDER_MAX_ENTRIES) return record;
  entries.sort((a, b) => (b[1]?.usedAt ?? 0) - (a[1]?.usedAt ?? 0));
  return Object.fromEntries(entries.slice(0, SPLIT_PAIR_ORDER_MAX_ENTRIES));
}

function toHostPanes(state: SplitLayoutState): HostSplitLayoutPanes {
  return {
    left: state.panes.left,
    right: state.panes.right,
    focus: state.focus,
    ratio: state.ratio
  };
}

function fromHostPanes(
  panes: HostSplitLayoutPanes | null
): SplitLayoutState | null {
  if (!panes) return null;
  return {
    panes: { left: panes.left, right: panes.right },
    focus: panes.focus,
    ratio: panes.ratio
  };
}

function isUnsupported(error: unknown): boolean {
  if (!(error instanceof HostBridgeUnavailableError)) return false;
  const code = (error as { code?: unknown }).code;
  return (
    code === "host_capability_unsupported" || code === "host_bridge_unavailable"
  );
}

function pairOrderStorageKey(scopeKey: string): string {
  return `${scopeKey}:pair-order`;
}

function readLocalPairOrder(
  storage: Pick<Storage, "getItem"> | null,
  scopeKey: string
): SplitPairOrderRecord {
  if (!storage) return {};
  try {
    const raw = storage.getItem(pairOrderStorageKey(scopeKey));
    if (!raw) return {};
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {};
    }
    return parsed as SplitPairOrderRecord;
  } catch {
    return {};
  }
}

function writeLocalPairOrder(
  storage: Pick<Storage, "setItem"> | null,
  scopeKey: string,
  record: SplitPairOrderRecord
): void {
  if (!storage) return;
  try {
    storage.setItem(pairOrderStorageKey(scopeKey), JSON.stringify(record));
  } catch {
    // 私密模式 / 配额：布局是尽力而为，丢了只是下次退回默认。
  }
}

export interface EmbeddedSplitLayoutStoreInput {
  scopeKey: string;
  storage: Pick<Storage, "getItem" | "setItem"> | null;
  targetId: string;
  /** 只给测试换掉宿主写入口（默认走宿主桥）。 */
  hostWrite?: typeof requestHostUpdateSplitLayout;
}

/**
 * 宿主优先、localStorage 兜底的存储。
 *
 * 「宿主没有这个能力」与「这次调用没成功」分开：前者一次判定后永久退回本地
 * （老宿主不会中途长出能力），后者只是这一次失败，下次还试 —— 否则一次 5s 超时
 * 就会让整个会话都不再落盘。
 */
export function createEmbeddedSplitLayoutStore(
  input: EmbeddedSplitLayoutStoreInput
): EmbeddedSplitLayoutStore {
  const { scopeKey, storage, targetId } = input;
  const hostWrite = input.hostWrite ?? requestHostUpdateSplitLayout;
  let hostUnsupported = false;

  function localSnapshot(): EmbeddedSplitLayoutSnapshot {
    return {
      layout: storage ? loadSplitLayout(storage, scopeKey) : null,
      pairOrder: readLocalPairOrder(storage, scopeKey)
    };
  }

  // 发往宿主的写一次只放一个在途，后来的只留最新那份。拖分界线每个 pointermove
  // 都会 write：并发的 fetch 到宿主时先后不定，存下来的可能是拖到一半的比例，
  // 还会白白重写几十次磁盘。串行 + 只留最新 = 最后落盘的一定是最后一次 write。
  let queuedHostWrite:
    | Parameters<typeof requestHostUpdateSplitLayout>[0]
    | null = null;
  let hostWriteInFlight = false;

  async function flushHostWrites(): Promise<void> {
    hostWriteInFlight = true;
    try {
      while (queuedHostWrite && !hostUnsupported) {
        const next = queuedHostWrite;
        queuedHostWrite = null;
        try {
          await hostWrite(next);
        } catch (error) {
          if (isUnsupported(error)) hostUnsupported = true;
        }
      }
    } finally {
      hostWriteInFlight = false;
    }
  }

  return {
    async read() {
      const local = localSnapshot();
      if (hostUnsupported) return local;
      try {
        const remote = await requestHostSplitLayout({
          legacyLayout: local.layout ? toHostPanes(local.layout) : null,
          scopeKey,
          targetId
        });
        return {
          layout: fromHostPanes(remote.layout),
          pairOrder: remote.pairOrder
        };
      } catch (error) {
        if (isUnsupported(error)) hostUnsupported = true;
        // 宿主临时出错时也要给出本地那一份：总比开机就丢分栏好。
        return local;
      }
    },
    write(snapshot) {
      // 本地那份照写不误：它既是老宿主的唯一存处，也是宿主第一次读时的种子。
      if (storage) {
        saveSplitLayout(
          storage,
          scopeKey,
          snapshot.layout ?? {
            panes: { left: null, right: null },
            focus: "left",
            ratio: 0.5
          }
        );
        writeLocalPairOrder(storage, scopeKey, snapshot.pairOrder);
      }
      if (hostUnsupported || !snapshot.layout) return;
      queuedHostWrite = {
        layout: toHostPanes(snapshot.layout),
        pairOrder: trimSplitPairOrder(snapshot.pairOrder),
        scopeKey,
        targetId
      };
      if (!hostWriteInFlight) void flushHostWrites();
    }
  };
}
