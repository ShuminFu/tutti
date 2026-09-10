// 分栏配对（补丁 0108）：侧栏会话条目的指针拖动手势。
//
// gui 包只负责「拖起 / 拖动 / 放下 / 放弃」四个生命周期的上报；拖影、放下区、
// 两栏本身都由宿主（apps/desktop）在 conversationRailSplitHost 里实现。
// 用指针事件而不是 HTML5 drag：拖影可控、不触发宿主的文件拖放（PRD「拖动手势」）。
import { useEffect, useRef, useState } from "react";
import {
  conversationRailSplitHost,
  type ConversationRailSplitDragSession,
  type ConversationRailSplitPoint
} from "../model/conversationRailSplitHost";

/** 6px 死区：移动不足这个距离的按下/松开仍是普通点击（PRD 故事 1）。 */
export const CONVERSATION_RAIL_SPLIT_DRAG_DEADZONE_PX = 6;

export interface ConversationRailItemDragHandlers {
  onPointerDown: (event: React.PointerEvent<HTMLElement>) => void;
  onPointerMove: (event: React.PointerEvent<HTMLElement>) => void;
  onPointerUp: (event: React.PointerEvent<HTMLElement>) => void;
  onPointerCancel: (event: React.PointerEvent<HTMLElement>) => void;
  onLostPointerCapture: (event: React.PointerEvent<HTMLElement>) => void;
}

export interface ConversationRailItemDrag {
  /** 宿主已注册且该条允许拖：行上挂 data-split-draggable（touch-action:none）。 */
  draggable: boolean;
  /** 已越过死区、正在拖：行上挂 data-split-dragging（原位虚影）。 */
  dragging: boolean;
  handlers: ConversationRailItemDragHandlers;
  /**
   * 放下之后浏览器还会合成一次 click；调用方在 select 的 onClick 里先问这里，
   * 返回 true 就吞掉这一次（只吞一次），免得一次放下同时又在本节点选中该会话。
   */
  consumeSuppressedClick: () => boolean;
}

interface DragTracking {
  pointerId: number;
  startX: number;
  startY: number;
  element: HTMLElement;
}

function pointOf(event: { clientX: number; clientY: number }): ConversationRailSplitPoint {
  return { x: event.clientX, y: event.clientY };
}

export function useConversationRailItemDrag(input: {
  /** 交互锁 / 待删除 等情况下为 false：不拖，事件原样放行。 */
  enabled: boolean;
  /** 越过死区那一刻才取一次，拿的是当时的标题/图标/状态。 */
  getSession: () => ConversationRailSplitDragSession;
}): ConversationRailItemDrag {
  const host = conversationRailSplitHost();
  const draggable = Boolean(host) && input.enabled;
  const [dragging, setDragging] = useState(false);
  // 手势状态放 ref：pointermove 频率高，不能每次都走 setState；
  // dragging 的 state 只用来切 data 属性。
  const trackingRef = useRef<DragTracking | null>(null);
  const draggingRef = useRef(false);
  const suppressClickRef = useRef(false);
  const getSessionRef = useRef(input.getSession);
  getSessionRef.current = input.getSession;
  const enabledRef = useRef(input.enabled);
  enabledRef.current = input.enabled;

  const finish = (cancelled: boolean, point?: ConversationRailSplitPoint): void => {
    const tracking = trackingRef.current;
    const wasDragging = draggingRef.current;
    trackingRef.current = null;
    // 先清标志再释放捕获：releasePointerCapture 会同步触发 lostpointercapture，
    // 那个 handler 看到标志已清就不会把一次正常放下再报成 cancel。
    draggingRef.current = false;
    if (!wasDragging) return;
    setDragging(false);
    if (tracking) {
      try {
        if (tracking.element.hasPointerCapture?.(tracking.pointerId)) {
          tracking.element.releasePointerCapture?.(tracking.pointerId);
        }
      } catch {
        // 指针已经不在（例如窗口失焦），释放失败无所谓。
      }
    }
    const activeHost = conversationRailSplitHost();
    if (!activeHost) return;
    if (cancelled || !point) {
      activeHost.onDragCancel();
    } else {
      activeHost.onDragEnd(point);
    }
  };

  // Esc 与窗口失焦只在拖动期间监听：拖起 → 挂；结束 → 拆。
  useEffect(() => {
    if (!dragging) return;
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        event.preventDefault();
        finish(true);
      }
    };
    const onBlur = (): void => finish(true);
    window.addEventListener("keydown", onKeyDown, true);
    window.addEventListener("blur", onBlur);
    return () => {
      window.removeEventListener("keydown", onKeyDown, true);
      window.removeEventListener("blur", onBlur);
    };
    // finish 是每次渲染新建的闭包，但只读 ref，不需要进依赖。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dragging]);

  // 卸载时若还在拖，向宿主报 cancel，别让宿主的放下区永远亮着。
  useEffect(() => {
    return () => {
      if (draggingRef.current) {
        draggingRef.current = false;
        trackingRef.current = null;
        conversationRailSplitHost()?.onDragCancel();
      }
    };
  }, []);

  const handlers: ConversationRailItemDragHandlers = {
    onPointerDown: (event) => {
      // 每次按下都清掉上次放下留下的「吞一次 click」：放下若没合成 click
      //（指针在别处松开），这个标志不能留到下一次真正的点击。
      suppressClickRef.current = false;
      if (!enabledRef.current || !conversationRailSplitHost()) return;
      if (event.button !== 0 || !event.isPrimary) return;
      // 只记起点，不 setPointerCapture、不 preventDefault：
      // 死区内的按下/松开还是普通 click / dblclick（选中、重命名）要照常工作。
      trackingRef.current = {
        element: event.currentTarget,
        pointerId: event.pointerId,
        startX: event.clientX,
        startY: event.clientY
      };
    },
    onPointerMove: (event) => {
      const tracking = trackingRef.current;
      if (!tracking || tracking.pointerId !== event.pointerId) return;
      if (draggingRef.current) {
        conversationRailSplitHost()?.onDragMove(pointOf(event));
        return;
      }
      const dx = event.clientX - tracking.startX;
      const dy = event.clientY - tracking.startY;
      if (Math.hypot(dx, dy) < CONVERSATION_RAIL_SPLIT_DRAG_DEADZONE_PX) return;
      const activeHost = conversationRailSplitHost();
      if (!activeHost || !enabledRef.current) {
        trackingRef.current = null;
        return;
      }
      // 越过死区：这时才捕获指针（拖出 iframe 边界也能收到 up/cancel），
      // 并向宿主报一次 dragStart。happy-dom 没有 setPointerCapture，可选调用。
      try {
        tracking.element.setPointerCapture?.(event.pointerId);
      } catch {
        // 指针已失效时 setPointerCapture 会抛，忽略。
      }
      draggingRef.current = true;
      setDragging(true);
      activeHost.onDragStart(getSessionRef.current(), pointOf(event));
    },
    onPointerUp: (event) => {
      const tracking = trackingRef.current;
      if (!tracking || tracking.pointerId !== event.pointerId) return;
      if (!draggingRef.current) {
        // 死区内松开：什么都不做，让后面的 click 正常流转。
        trackingRef.current = null;
        return;
      }
      // 放下后紧跟的那次合成 click 要吞掉，否则一次放下会顺带在本节点选中它。
      suppressClickRef.current = true;
      finish(false, pointOf(event));
    },
    onPointerCancel: (event) => {
      const tracking = trackingRef.current;
      if (!tracking || tracking.pointerId !== event.pointerId) return;
      finish(true);
    },
    onLostPointerCapture: (event) => {
      const tracking = trackingRef.current;
      if (!tracking || tracking.pointerId !== event.pointerId) return;
      finish(true);
    }
  };

  return {
    consumeSuppressedClick: () => {
      const suppressed = suppressClickRef.current;
      suppressClickRef.current = false;
      return suppressed;
    },
    draggable,
    dragging,
    handlers
  };
}
