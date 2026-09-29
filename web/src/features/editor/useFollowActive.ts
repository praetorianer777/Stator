import { useEffect, useState } from "react";

/**
 * Keeps the active option of a list under the caret in view. Pass the result
 * as the list's ref; give the list overflow-y-hidden so it clips, not scrolls.
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

  return setList;
}
