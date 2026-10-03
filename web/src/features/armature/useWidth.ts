import { useEffect, useRef, useState } from "react";

/**
 * An element's width in pixels, so a drawing is one unit to the pixel and its
 * words keep their size on a phone rather than shrinking with the drawing.
 */
export function useWidth(fallback: number) {
  const box = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(fallback);
  useEffect(() => {
    const el = box.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const seen = new ResizeObserver(([entry]) => {
      const w = Math.round(entry?.contentRect.width ?? 0);
      if (w > 0) setWidth(w);
    });
    seen.observe(el);
    return () => seen.disconnect();
  }, []);
  return { box, width };
}
