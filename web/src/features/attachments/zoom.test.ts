import { describe, expect, it } from "vitest";
import { LIGHTBOX_MAX_ZOOM } from "@/config";
import { FITTED, clampView, isZoomed, panBy, step, zoomAt, zoomPercent, type Bounds } from "./zoom";

// A room of 800 by 600 holding a picture fitted to 400 by 600.
const room: Bounds = { width: 800, height: 600, imageWidth: 400, imageHeight: 600 };

describe("zooming a picture", () => {
  it("never goes below fitted or above the maximum", () => {
    expect(zoomAt(FITTED, 0.5, { x: 0, y: 0 }, room)).toEqual(FITTED);
    expect(zoomAt(FITTED, 100, { x: 0, y: 0 }, room).scale).toBe(LIGHTBOX_MAX_ZOOM);
  });

  it("keeps the point under the pointer where it was", () => {
    // Twice as large around a point 150px below the middle: the picture moves
    // up by 150 so that point, now 300 below its middle, stays at 150.
    const view = zoomAt(FITTED, 2, { x: 0, y: 150 }, room);
    expect(view).toEqual({ scale: 2, x: 0, y: -150 });
    expect(view.y + view.scale * 150).toBe(150);
  });

  it("pulls no edge of the picture into the room", () => {
    // At 2x the picture is 800 by 1200: it fills the width exactly and
    // overhangs 300 above and below.
    const view = zoomAt(FITTED, 2, { x: 0, y: 0 }, room);
    expect(panBy(view, 500, 0, room)).toEqual({ scale: 2, x: 0, y: 0 });
    expect(panBy(view, 0, 1000, room)).toEqual({ scale: 2, x: 0, y: 300 });
    expect(panBy(view, 0, -1000, room)).toEqual({ scale: 2, x: 0, y: -300 });
  });

  it("comes back to the middle when zoomed out again", () => {
    const view = panBy(zoomAt(FITTED, 4, { x: 0, y: 0 }, room), 300, 300, room);
    expect(isZoomed(view)).toBe(true);
    expect(zoomAt(view, 1 / 4, { x: 0, y: 0 }, room)).toEqual(FITTED);
    expect(clampView({ scale: 0.2, x: 50, y: 50 }, room)).toEqual(FITTED);
  });

  it("reads as a percentage of fitted", () => {
    expect(zoomPercent(FITTED)).toBe(100);
    expect(zoomPercent({ scale: 2.254, x: 0, y: 0 })).toBe(225);
  });
});

describe("stepping through a list", () => {
  it("goes round from either end", () => {
    expect(step(0, 1, 3)).toBe(1);
    expect(step(2, 1, 3)).toBe(0);
    expect(step(0, -1, 3)).toBe(2);
    expect(step(0, 1, 0)).toBe(0);
  });
});
