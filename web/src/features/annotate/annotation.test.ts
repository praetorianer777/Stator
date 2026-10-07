import { describe, expect, it } from "vitest";
import { ANNOTATION_HISTORY_LIMIT, ANNOTATION_MIN_CROP_PX, ANNOTATION_STROKE_MIN_PX, ANNOTATION_TEXT_MIN_PX } from "@/config";
import {
  CROP,
  EMPTY,
  arrowHead,
  commit,
  cropInMiddle,
  edgeFor,
  fitCrop,
  isAnnotatable,
  isChanged,
  keyStep,
  nextSelection,
  nudge,
  outputRect,
  rectBetween,
  redo,
  removeSelected,
  shapeAt,
  shapeBounds,
  shapeInMiddle,
  startHistory,
  strokeWidth,
  textSize,
  undo,
  type Annotation,
  type Arrow,
  type Box,
  type Label,
  type Measure,
} from "./annotation";

const size = { width: 800, height: 600 };
// Each letter is half as wide as the text is tall.
const measure: Measure = (text, height) => (text.length * height) / 2;
const box: Box = { id: "b", kind: "box", colour: "red", rect: { x: 100, y: 100, width: 200, height: 100 } };
const arrow: Arrow = { id: "a", kind: "arrow", colour: "blue", from: { x: 0, y: 0 }, to: { x: 100, y: 100 } };
const label: Label = { id: "t", kind: "text", colour: "yellow", at: { x: 400, y: 400 }, text: "Here" };

describe("which pictures can be annotated", () => {
  it("are the PNGs, JPEGs and WebPs the browser writes back in their own type", () => {
    expect(["image/png", "image/jpeg", "IMAGE/WEBP; q=1"].map(isAnnotatable)).toEqual([true, true, true]);
    expect(["image/gif", "image/svg+xml", "application/pdf", ""].map(isAnnotatable)).toEqual([false, false, false, false]);
  });
});

describe("sizes on a picture", () => {
  it("grow with the picture and keep a floor on a small one", () => {
    expect(strokeWidth({ width: 4000, height: 3000 })).toBe(18);
    expect(strokeWidth({ width: 40, height: 30 })).toBe(ANNOTATION_STROKE_MIN_PX);
    expect(textSize({ width: 4000, height: 3000 })).toBe(135);
    expect(textSize({ width: 40, height: 30 })).toBe(ANNOTATION_TEXT_MIN_PX);
    expect(keyStep(size)).toBe(12);
    expect(keyStep({ width: 10, height: 10 })).toBe(1);
  });
});

describe("a crop", () => {
  it("is the rectangle dragged, whichever way", () => {
    expect(rectBetween({ x: 50, y: 80 }, { x: 10, y: 20 })).toEqual({ x: 10, y: 20, width: 40, height: 60 });
  });

  it("stays within the picture in whole pixels, and keeps a least size", () => {
    expect(fitCrop({ x: -20, y: 590.4, width: 100.6, height: 50 }, size)).toEqual({ x: 0, y: 550, width: 101, height: 50 });
    expect(fitCrop({ x: 10, y: 10, width: 1, height: 2 }, size)).toEqual({ x: 10, y: 10, width: ANNOTATION_MIN_CROP_PX, height: ANNOTATION_MIN_CROP_PX });
    expect(fitCrop({ x: 0, y: 0, width: 5000, height: 5000 }, size)).toEqual({ x: 0, y: 0, ...size });
    expect(fitCrop({ x: 0, y: 0, width: 1, height: 1 }, { width: 4, height: 4 })).toEqual({ x: 0, y: 0, width: 4, height: 4 });
  });

  it("is what is saved, or the whole picture without one", () => {
    expect(outputRect(EMPTY, size)).toEqual({ x: 0, y: 0, ...size });
    const crop = { x: 1, y: 2, width: 3, height: 4 };
    expect(outputRect({ shapes: [], crop }, size)).toBe(crop);
    expect(cropInMiddle(size)).toEqual({ x: 80, y: 60, width: 640, height: 480 });
  });
});

describe("picking a shape", () => {
  const shapes = [box, arrow, label];

  it("takes a box or an arrow by its line, and text anywhere on it", () => {
    expect(shapeAt(shapes, { x: 102, y: 150 }, 5, size, measure)?.id).toBe("b");
    expect(shapeAt(shapes, { x: 200, y: 150 }, 5, size, measure)).toBeNull();
    expect(shapeAt(shapes, { x: 52, y: 48 }, 5, size, measure)?.id).toBe("a");
    expect(shapeAt(shapes, { x: 120, y: 120 }, 5, size, measure)).toBeNull();
    expect(shapeAt(shapes, { x: 410, y: 410 }, 0, size, measure)?.id).toBe("t");
  });

  it("takes the topmost where shapes cross", () => {
    const over: Box = { ...box, id: "over" };
    expect(shapeAt([box, over], { x: 100, y: 150 }, 5, size, measure)?.id).toBe("over");
  });

  it("measures text at the picture's text size", () => {
    expect(shapeBounds(label, size, measure)).toEqual({ x: 400, y: 400, width: 2 * textSize(size), height: textSize(size) });
    expect(shapeBounds(arrow, size, measure)).toEqual({ x: 0, y: 0, width: 100, height: 100 });
  });
});

describe("changing a shape", () => {
  const drawn: Annotation = { shapes: [box, arrow, label], crop: { x: 0, y: 0, width: 100, height: 100 } };

  it("moves the one selected, or with resize grows a box's far corner and an arrow's head", () => {
    expect(nudge(drawn, "b", 10, -5, false, size).shapes[0]).toMatchObject({ rect: { x: 110, y: 95, width: 200, height: 100 } });
    expect(nudge(drawn, "b", 10, -5, true, size).shapes[0]).toMatchObject({ rect: { x: 100, y: 100, width: 210, height: 95 } });
    expect(nudge(drawn, "a", 10, 0, false, size).shapes[1]).toMatchObject({ from: { x: 10, y: 0 }, to: { x: 110, y: 100 } });
    expect(nudge(drawn, "a", 10, 0, true, size).shapes[1]).toMatchObject({ from: { x: 0, y: 0 }, to: { x: 110, y: 100 } });
    expect(nudge(drawn, "t", 0, 3, true, size).shapes[2]).toBe(label);
    expect(nudge(drawn, "nothing", 1, 1, false, size)).toBe(drawn);
  });

  it("moves the crop within the picture", () => {
    expect(nudge(drawn, CROP, -10, 20, false, size).crop).toEqual({ x: 0, y: 20, width: 100, height: 100 });
    expect(nudge(drawn, CROP, 5000, 0, true, size).crop).toEqual({ x: 0, y: 0, width: 800, height: 100 });
  });

  it("removes the selection, a shape or the crop", () => {
    expect(removeSelected(drawn, "a").shapes.map((s) => s.id)).toEqual(["b", "t"]);
    expect(removeSelected(drawn, CROP).crop).toBeNull();
    expect(removeSelected(drawn, null)).toBe(drawn);
  });

  it("is picked by keyboard one after another, the crop last, going round", () => {
    expect(nextSelection(drawn, null)).toBe("b");
    expect(nextSelection(drawn, "t")).toBe(CROP);
    expect(nextSelection(drawn, CROP)).toBe("b");
    expect(nextSelection(EMPTY, null)).toBeNull();
  });

  it("goes in the middle when placed by keyboard", () => {
    expect(shapeInMiddle("box", "n", "green", size)).toEqual({ id: "n", kind: "box", colour: "green", rect: { x: 250, y: 225, width: 300, height: 150 } });
    expect(shapeInMiddle("arrow", "n", "green", size)).toMatchObject({ from: { x: 250, y: 450 }, to: { x: 400, y: 300 } });
  });
});

describe("an arrow's head", () => {
  it("points back along the arrow from its tip", () => {
    const [left, right] = arrowHead({ from: { x: 0, y: 0 }, to: { x: 100, y: 0 } }, 10);
    expect(left.x).toBeCloseTo(100 - 10 * Math.cos(Math.PI / 7));
    expect(left.y).toBeCloseTo(10 * Math.sin(Math.PI / 7));
    expect(right.y).toBeCloseTo(-left.y);
  });
});

describe("the edge of a colour", () => {
  it("is black for the light ones and white for the dark ones", () => {
    expect(["yellow", "white", "green"].map((c) => edgeFor(c as "yellow"))).toEqual(["#000000", "#000000", "#000000"]);
    expect(["red", "blue", "black"].map((c) => edgeFor(c as "red"))).toEqual(["#ffffff", "#ffffff", "#ffffff"]);
  });
});

describe("undo and redo", () => {
  const one: Annotation = { shapes: [box], crop: null };
  const two: Annotation = { shapes: [box, arrow], crop: null };

  it("walk back and forth, and a new step forgets what was undone", () => {
    let h = commit(commit(startHistory(), one), two);
    expect(h.present).toBe(two);
    h = undo(h);
    expect(h.present).toBe(one);
    h = undo(undo(h));
    expect(h.present).toBe(EMPTY);
    h = redo(h);
    expect(h.present).toBe(one);
    expect(h.future).toEqual([two]);
    h = commit(h, { shapes: [label], crop: null });
    expect(h.future).toEqual([]);
    expect(redo(h)).toBe(h);
  });

  it("count no step that changes nothing, and keep a limited past", () => {
    const h = startHistory(one);
    expect(commit(h, one)).toBe(h);
    let long = startHistory();
    for (let i = 0; i <= ANNOTATION_HISTORY_LIMIT + 5; i++) long = commit(long, { shapes: [], crop: { x: i, y: 0, width: 10, height: 10 } });
    expect(long.past).toHaveLength(ANNOTATION_HISTORY_LIMIT);
  });

  it("tell whether there is anything to save", () => {
    expect(isChanged(EMPTY, size)).toBe(false);
    expect(isChanged({ shapes: [], crop: { x: 0, y: 0, ...size } }, size)).toBe(false);
    expect(isChanged({ shapes: [], crop: { x: 1, y: 0, width: 10, height: 10 } }, size)).toBe(true);
    expect(isChanged({ shapes: [label], crop: null }, size)).toBe(true);
  });
});
