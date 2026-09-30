import { useEffect, useState } from "react";

/**
 * Keeps the active option of a clipped (overflow-y-hidden) list in view and
 * lets the wheel and a swipe scroll it. Pass the result as the list's ref.
 */
export function useFollowActive(active: number): (list: HTMLElement | null) => void {
  const [list, setList] = useState<HTMLElement | null>(null);

  // The editor keeps focus and moves the active option with the arrow keys,
  // so a list that follows it needs no focus of its own to be read from the
  // keyboard; a list that scrolled by itself would need a tab stop.
  useEffect(() => {
    list?.children[active]?.scrollIntoView?.({ block: "nearest" });
  }, [list, active]);

  // A clipped list ignores the wheel, so the wheel scrolls it by hand, and
  // the page behind stays put as it would under a list that scrolls.
  useEffect(() => {
    if (!list) return;
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      list.scrollTop += e.deltaY;
    };
    list.addEventListener("wheel", wheel, { passive: false });
    return () => list.removeEventListener("wheel", wheel);
  }, [list]);

  // A clipped list ignores a swipe too, so the finger drags it by hand. Only
  // the move is cancelled: a tap still ends in the mousedown that picks.
  useEffect(() => {
    if (!list) return;
    let lastY: number | undefined;
    const start = (e: TouchEvent) => {
      lastY = e.touches.length === 1 ? e.touches[0]?.clientY : undefined;
    };
    const move = (e: TouchEvent) => {
      const y = e.touches.length === 1 ? e.touches[0]?.clientY : undefined;
      if (lastY === undefined || y === undefined) return;
      e.preventDefault();
      list.scrollTop += lastY - y;
      lastY = y;
    };
    list.addEventListener("touchstart", start, { passive: true });
    list.addEventListener("touchmove", move, { passive: false });
    return () => {
      list.removeEventListener("touchstart", start);
      list.removeEventListener("touchmove", move);
    };
  }, [list]);

  return setList;
}
