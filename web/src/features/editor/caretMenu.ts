import type { CSSProperties } from "react";
import { CARET_MENU_EDGE_PX, CARET_MENU_MIN_ROOM_PX, MENU_GAP_PX } from "@/config";

/**
 * Where a menu drawn at the caret goes, as fixed viewport coordinates: below
 * the caret when there is room for a useful list, above it when there is more
 * room there. The room on the side it takes is handed over as the custom
 * property CARET_MENU_ROOM, which the menu's max-height reads next to its own
 * cap, so it cannot reach past the window; a stylesheet rule on the menu still
 * outranks it, as a class would. Above, it hangs from the caret by its bottom
 * edge, so its height need not be known to place it.
 */
export const CARET_MENU_ROOM = "--caret-menu-room";

/** The class that caps a menu's height at its cap or the room it has, whichever is less. */
export const CARET_MENU_HEIGHT_CLASS = "max-h-[min(20rem,var(--caret-menu-room))]";

export function caretMenuPlace(rect: Pick<DOMRect, "left" | "top" | "bottom">, width: number, viewport: { width: number; height: number }): CSSProperties {
  const below = viewport.height - rect.bottom - MENU_GAP_PX - CARET_MENU_EDGE_PX;
  const above = rect.top - MENU_GAP_PX - CARET_MENU_EDGE_PX;
  const left = Math.max(CARET_MENU_EDGE_PX, Math.min(rect.left, viewport.width - width - CARET_MENU_EDGE_PX));
  if (below >= CARET_MENU_MIN_ROOM_PX || below >= above) {
    return { left, top: rect.bottom + MENU_GAP_PX, [CARET_MENU_ROOM]: `${Math.max(0, below)}px` } as CSSProperties;
  }
  return { left, bottom: viewport.height - rect.top + MENU_GAP_PX, [CARET_MENU_ROOM]: `${Math.max(0, above)}px` } as CSSProperties;
}

/** The same, for the window the page is in now. */
export const caretMenuPlaceNow = (rect: Pick<DOMRect, "left" | "top" | "bottom">, width: number): CSSProperties =>
  caretMenuPlace(rect, width, { width: window.innerWidth, height: window.innerHeight });
