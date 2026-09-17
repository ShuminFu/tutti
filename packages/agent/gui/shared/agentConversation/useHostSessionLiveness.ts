import { useEffect, useMemo, useRef, useState } from "react";
import {
  readHostSessionLivenessCache,
  writeHostSessionLivenessCache
} from "./hostSessionLivenessCache";
import {
  sessionLivenessHost,
  SESSION_LIVENESS_MAX_IDS,
  type HostSessionLivenessState
} from "./sessionLivenessHost";

// RNDMASTER_RAIL_HOST_LIVENESS：回退看红时改 false，保留本符号。
const railHostLiveness = true;

// 会话栏一直在眼前，4s 一拍与 0117 的审批卡同频；页面不可见时不拍。
export const HOST_SESSION_LIVENESS_POLL_INTERVAL_MS = 4000;

/**
 * 宿主对一条会话的两句话（补丁 0128 起是两句，之前只有 `state` 一句）：
 *
 * - `state`：tuttid 里这条会话有没有活的 ACP 进程 —— **圆点只看它**。
 * - `attached`：rndmaster 那边有没有非终态任务行盯着 —— **补挂只看它**。
 *
 * 两者正交，别互相推断：用户在聊天时 `state` 必然 `"live"`，可宿主的账早就被
 * 后端重启打断了。
 *
 * `state === "pending"` 是客户端自己的第三档：宿主已启用，但这条还没进缓存。
 * 宿主契约里没有 pending。`attached` 此时是 `undefined`，补挂那条路必须当成
 * 「第一拍还没回来」，不能当成 `false` 去写账。
 */
export interface HostSessionLivenessRecord {
  state: HostSessionLivenessState | "pending";
  attached: boolean | undefined;
}

export type HostSessionLivenessMap = ReadonlyMap<
  string,
  HostSessionLivenessRecord
>;

const EMPTY_LIVENESS: HostSessionLivenessMap = new Map();

const PENDING_RECORD: HostSessionLivenessRecord = Object.freeze({
  state: "pending",
  attached: undefined
});

function projectVisibleLiveness(
  workspaceId: string,
  ids: readonly string[]
): HostSessionLivenessMap {
  if (ids.length === 0) return EMPTY_LIVENESS;
  const next = new Map<string, HostSessionLivenessRecord>();
  for (const id of ids) {
    next.set(
      id,
      readHostSessionLivenessCache(workspaceId, id) ?? PENDING_RECORD
    );
  }
  return next;
}

/**
 * 轮询宿主，问「会话栏里现在画着的这些会话，宿主那边还活着吗」。
 *
 * 判定顺序上的几件事，都是踩过的坑：
 *
 * 1. **宿主没这组能力就永久停拍**：桥把「未嵌入 / 老宿主 / 超时」统一归一成
 *    `Error("unsupported")`，收到就再也不发请求，否则老宿主上每 4s 一次白等。
 * 2. **不叠请求**：上一拍还没回来时这一拍直接跳过。会话栏行数可以到 200，
 *    宿主那边是一次 SQL 查询，慢一拍比堆积一串在途请求好。
 * 3. **页面不可见不拍**：后台标签页里 WebKit 本来就会把定时器钳到每分钟一拍，
 *    与其拍出一串迟到结果不如不拍。
 * 4. **不整表替换**：切 Agent 会换一批可见 id。若每次用宿主回包另起一张表，
 *    切回来时原会话状态丢失，presence 把空值画成绿点，等 `closed` 回来再摘掉
 *    （闪一下）。按 workspaceId + sessionId 留有界缓存，权威回包只更新对应条目。
 * 5. **未加载 ≠ 空闲**：可见 id 还没进缓存时标 `pending`，presence 不画绿点。
 *    宿主没这组能力仍回空表，presence 把空当 0119 回退。
 *
 * 返回的 Map 只在**内容真的变了**时换新对象，让会话栏那几层 memo 还能挡住
 * 无谓重渲染。
 */
export function useHostSessionLiveness(
  visibleSessionIds: readonly string[],
  workspaceId = ""
): HostSessionLivenessMap {
  const host = sessionLivenessHost();
  const enabled = railHostLiveness && host !== null;
  // 去重 + 截断 + 排序：排序后 join 出来的 key 才是稳定的依赖项，
  // 会话栏重排（新消息上浮）不该触发一次额外请求。
  const requestedIds = useMemo(() => {
    if (!enabled) return [] as string[];
    const unique = Array.from(
      new Set(
        visibleSessionIds.filter(
          (id): id is string => typeof id === "string" && id.trim() !== ""
        )
      )
    );
    unique.sort();
    return unique.slice(0, SESSION_LIVENESS_MAX_IDS);
  }, [enabled, visibleSessionIds]);
  const requestKey = requestedIds.join(",");
  const [revision, setRevision] = useState(0);
  // 「宿主不支持」跨依赖变化也要记住：ids 一变 effect 会重跑，
  // 用局部变量记会被重置成「再试一次」。state 用来把视图从 pending 退回空表，
  // presence 才能走 0119 回退，而不是永远不画绿点。
  const unsupportedRef = useRef(false);
  const [unsupported, setUnsupported] = useState(false);

  const liveness = useMemo(() => {
    if (!enabled || unsupported || requestKey === "") return EMPTY_LIVENESS;
    return projectVisibleLiveness(workspaceId, requestKey.split(","));
  }, [enabled, unsupported, workspaceId, requestKey, revision]);

  useEffect(() => {
    if (!enabled || !host || requestKey === "") {
      return;
    }
    let cancelled = false;
    let inFlight = false;
    const ids = requestKey.split(",");
    const tick = async (): Promise<void> => {
      if (cancelled || unsupportedRef.current || inFlight) return;
      inFlight = true;
      try {
        const result = await host.querySessionLiveness({
          agentSessionIds: ids
        });
        // 旧请求取消保护：cancelled 之后既不写缓存也不 bump，避免过期回包
        // 把切走之后的权威状态盖掉。
        if (cancelled) return;
        const authoritative: Array<
          readonly [
            string,
            { state: HostSessionLivenessState; attached: boolean }
          ]
        > = [];
        for (const [id, entry] of Object.entries(result?.sessions ?? {})) {
          if (entry && typeof entry.state === "string") {
            // attached 缺字段兜 false：老宿主只回 state，当「没在盯」处理，
            // 补挂那条路自己会去把账挂上（补丁 0128）。
            authoritative.push([
              id,
              {
                state: entry.state,
                attached: entry.attached === true
              }
            ]);
          }
        }
        const before = projectVisibleLiveness(workspaceId, ids);
        writeHostSessionLivenessCache(workspaceId, authoritative, new Set(ids));
        const after = projectVisibleLiveness(workspaceId, ids);
        if (!livenessMapsEqual(before, after)) {
          setRevision((current) => current + 1);
        }
      } catch (cause) {
        // 桥超时也会归一成 unsupported。被取消的那一拍不能据此停拍，
        // 否则切 Agent 时迟到的超时会把当前视图清成空表、绿点再闪出来。
        if (cancelled) return;
        // 老宿主：停拍，会话栏退回 0119 的纯 Tutti 判据。
        if (cause instanceof Error && cause.message === "unsupported") {
          unsupportedRef.current = true;
          setUnsupported(true);
        }
        // 其余错误（宿主临时忙 / 后端重启）沉默重试：圆点不是关键路径，
        // 报错弹条只会打扰用户。
      } finally {
        inFlight = false;
      }
    };
    void tick();
    const timer = window.setInterval(() => {
      if (document.visibilityState !== "hidden") void tick();
    }, HOST_SESSION_LIVENESS_POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [enabled, host, requestKey, workspaceId]);

  return enabled && !unsupported ? liveness : EMPTY_LIVENESS;
}

function livenessMapsEqual(
  left: HostSessionLivenessMap,
  right: HostSessionLivenessMap
): boolean {
  if (left.size !== right.size) return false;
  for (const [id, record] of right) {
    // 值变成小对象后不能再用 !== 比：每一拍都是新对象，那样恒不相等，
    // 会话栏那几层 memo 就全废了（补丁 0128）。
    const previous = left.get(id);
    if (!previous) return false;
    if (previous.state !== record.state) return false;
    if (previous.attached !== record.attached) return false;
  }
  return true;
}
