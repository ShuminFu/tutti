// 分隔线拖动的几何（票 05 回归）：分隔线画在 `ratio + railPush` 处，但 controller
// 的 `resize()` 收的是**不含推力**的 `ratio`。直接把「指针占比」当 ratio 递进去，
// 两件事会同时发生：
//   1. 第一个 pointermove 就把 ratio 减掉一整个 railPush（会话栏展开时是两三百像素），
//      看着像「一按下去列先弹回去了」；
//   2. 之后分隔线永远画在指针右边 railPush 处 —— 不跟手。
// 所以这里把指针占比还原成 ratio：先扣掉按下时指针在 10px 命中带里的偏移（抓哪儿
// 就从哪儿起算），再扣掉**当前**的 railPush（栏宽会被右栏 320px 下限夹逼，拖动中
// 会变，必须每帧读最新值，不能用按下那一刻的）。
export interface EmbeddedSplitDividerDrag {
  /** 主区左边界（客户端坐标），用来把 clientX 折成占比。 */
  readonly surfaceLeft: number;
  /** 主区宽度；<= 0 时拖动无意义。 */
  readonly surfaceWidth: number;
  /** 按下点相对分隔线的偏移（占比）：抓在线右边 3px 就一直差这 3px。 */
  readonly grabOffsetRatio: number;
}

export function beginEmbeddedSplitDividerDrag(input: {
  readonly clientX: number;
  readonly dividerRatio: number;
  readonly surfaceLeft: number;
  readonly surfaceWidth: number;
}): EmbeddedSplitDividerDrag | null {
  const { clientX, dividerRatio, surfaceLeft, surfaceWidth } = input;
  if (!Number.isFinite(surfaceWidth) || surfaceWidth <= 0) return null;
  if (!Number.isFinite(surfaceLeft) || !Number.isFinite(clientX)) return null;
  const pointerRatio = (clientX - surfaceLeft) / surfaceWidth;
  const grabOffsetRatio = Number.isFinite(dividerRatio)
    ? pointerRatio - dividerRatio
    : 0;
  return { grabOffsetRatio, surfaceLeft, surfaceWidth };
}

/**
 * 拖动中的目标 ratio（未夹逼——每栏 >= 320px 的夹逼在 controller 里做）。
 * `railPushRatio` 传当前快照的值，不是按下那一刻的。
 */
export function embeddedSplitDividerDragRatio(
  drag: EmbeddedSplitDividerDrag,
  clientX: number,
  railPushRatio: number
): number | null {
  if (drag.surfaceWidth <= 0 || !Number.isFinite(clientX)) return null;
  const push = Number.isFinite(railPushRatio) ? railPushRatio : 0;
  return (
    (clientX - drag.surfaceLeft) / drag.surfaceWidth -
    drag.grabOffsetRatio -
    push
  );
}
