// 分栏配对（补丁 0108）：「哪些会话正在任一栏里显示」的订阅口。
//
// 宿主（apps/desktop）注册 conversationRailSplitHost 后，侧栏条目除了本节点自己的
// activeConversationId 之外，还要把另一栏里的那条也画成 active（PRD 故事 18）。
// 未注册时返回一个模块级的冻结空集合：身份稳定，不会让 memo 过的条目白白重渲染。
import { useSyncExternalStore } from "react";
import { conversationRailSplitHost } from "../model/conversationRailSplitHost";

const EMPTY_SHOWN_IDS: ReadonlySet<string> = Object.freeze(new Set<string>());

function subscribe(listener: () => void): () => void {
  const host = conversationRailSplitHost();
  // 没有宿主就没有变化源；注册本身不是可观察事件（与 0103 同：宿主在挂载前注册）。
  return host ? host.subscribe(listener) : () => {};
}

function getSnapshot(): ReadonlySet<string> {
  // 直接返回宿主自己那份 Set：useSyncExternalStore 靠引用相等判「没变」，
  // 这里绝不能每次 new 一个 Set，否则会陷入无限重渲染。
  return conversationRailSplitHost()?.getShownSessionIds() ?? EMPTY_SHOWN_IDS;
}

export function useConversationRailSplitShownIds(): ReadonlySet<string> {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
