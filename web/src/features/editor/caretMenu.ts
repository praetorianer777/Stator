import type { CSSProperties } from "react";
import { CARET_MENU_EDGE_PX, CARET_MENU_MAX_HEIGHT_PX, CARET_MENU_MIN_ROOM_PX, MENU_GAP_PX } from "@/config";

/**
 * Where a menu drawn at the caret goes, as fixed viewport coordinates: below
 * the caret when there is room for a useful list, above it when there is more
 * room there, and never taller than the room on the side it takes, so it
 * cannot reach past the window. Above, it hangs from the caret by its bottom
 * edge, so its height need not be known to place it.
 */
export function caretMenuPlace(rect: Pick<DOMRect, "left" | "top" | "bottom">, width: number, viewport: { width: number; height: number }): CSSProperties {
  const below = viewport.height - rect.bottom - MENU_GAP_PX - CARET_MENU_EDGE_PX;
  const above = rect.top - MENU_GAP_PX - CARET_MENU_EDGE_PX;
  const left = Math.max(CARET_MENU_EDGE_PX, Math.min(rect.left, viewport.width - width - CARET_MENU_EDGE_PX));
  if (below >= CARET_MENU_MIN_ROOM_PX || below >= above) {
    return { left, top: rect.bottom + MENU_GAP_PX, maxHeight: Math.max(0, Math.min(CARET_MENU_MAX_HEIGHT_PX, below)) };
  }
  return { left, bottom: viewport.height - rect.top + MENU_GAP_PX, maxHeight: Math.max(0, Math.min(CARET_MENU_MAX_HEIGHT_PX, above)) };
}

/** The same, for the window the page is in now. */
export const caretMenuPlaceNow = (rect: Pick<DOMRect, "left" | "top" | "bottom">, width: number): CSSProperties =>
  caretMenuPlace(rect, width, { width: window.innerWidth, height: window.innerHeight });
