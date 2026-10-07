import { LIGHTBOX_MAX_ZOOM } from "@/config";

/** How a picture is shown: its zoom over the fitted size, and how far it is moved from the middle. */
export interface View {
  scale: number;
  x: number;
  y: number;
}

/** The picture fitted to the screen, in the middle. */
export const FITTED: View = { scale: 1, x: 0, y: 0 };

/** The room the picture is shown in, and the picture's own size there when fitted. */
export interface Bounds {
  width: number;
  height: number;
  imageWidth: number;
  imageHeight: number;
}

/** A point measured from the middle of the room. */
export interface Point {
  x: number;
  y: number;
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(high, Math.max(low, value));
}

// Half of what overhangs the room on one axis: how far the picture may move
// before an edge comes away from the room's edge. Nothing, when it fits.
function slack(image: number, scale: number, room: number): number {
  return Math.max(0, (image * scale - room) / 2);
}

/** The view held inside its limits: zoom between fitted and the maximum, and no edge pulled into the room. */
export function clampView(view: View, bounds: Bounds): View {
  const scale = clamp(view.scale, 1, LIGHTBOX_MAX_ZOOM);
  const sx = slack(bounds.imageWidth, scale, bounds.width);
  const sy = slack(bounds.imageHeight, scale, bounds.height);
  return { scale, x: clamp(view.x, -sx, sx), y: clamp(view.y, -sy, sy) };
}

/**
 * The view zoomed by a factor around a point, which stays under the pointer
 * or between the fingers that asked, as far as the limits allow.
 */
export function zoomAt(view: View, factor: number, at: Point, bounds: Bounds): View {
  const scale = clamp(view.scale * factor, 1, LIGHTBOX_MAX_ZOOM);
  const ratio = scale / view.scale;
  return clampView({ scale, x: at.x - ratio * (at.x - view.x), y: at.y - ratio * (at.y - view.y) }, bounds);
}

/** The view moved by a distance, as far as the picture's edges allow. */
export function panBy(view: View, dx: number, dy: number, bounds: Bounds): View {
  return clampView({ ...view, x: view.x + dx, y: view.y + dy }, bounds);
}

export function isZoomed(view: View): boolean {
  return view.scale > 1;
}

/** The zoom as a person reads it: 100 for fitted. */
export function zoomPercent(view: View): number {
  return Math.round(view.scale * 100);
}

/** Where the next of a list goes, round from the last back to the first, and the other way. */
export function step(index: number, by: number, count: number): number {
  return count === 0 ? 0 : (((index + by) % count) + count) % count;
}
