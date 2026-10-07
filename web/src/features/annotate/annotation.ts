import {
  ANNOTATION_COLOURS,
  ANNOTATION_HISTORY_LIMIT,
  ANNOTATION_KEY_STEP_SHARE,
  ANNOTATION_MIN_CROP_PX,
  ANNOTATION_STROKE_MIN_PX,
  ANNOTATION_STROKE_SHARE,
  ANNOTATION_TEXT_MIN_PX,
  ANNOTATION_TEXT_SHARE,
  ANNOTATION_TYPES,
} from "@/config";

// Everything here is in the picture's own pixels, whatever size it is shown at.

export interface Point {
  x: number;
  y: number;
}

export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface Size {
  width: number;
  height: number;
}

export type Colour = keyof typeof ANNOTATION_COLOURS;

export interface Arrow {
  id: string;
  kind: "arrow";
  colour: Colour;
  from: Point;
  to: Point;
}

export interface Box {
  id: string;
  kind: "box";
  colour: Colour;
  rect: Rect;
}

/** A line of text, placed by its top left corner. */
export interface Label {
  id: string;
  kind: "text";
  colour: Colour;
  at: Point;
  text: string;
}

export type Shape = Arrow | Box | Label;

/** What is drawn on a picture: shapes, last on top, and the part kept, or all of it when crop is null. */
export interface Annotation {
  shapes: Shape[];
  crop: Rect | null;
}

export const EMPTY: Annotation = { shapes: [], crop: null };

/** What selecting the crop is called, beside the shapes' ids. */
export const CROP = "crop";

/** Whether a file of this type can be annotated, as the API judges it. */
export function isAnnotatable(contentType: string): boolean {
  return mediaType(contentType) in ANNOTATION_TYPES;
}

/** The type a picture is saved in: its own. */
export function mediaType(contentType: string): string {
  return contentType.split(";")[0]!.trim().toLowerCase();
}

/** The thickness of a line on a picture of this size. */
export function strokeWidth(size: Size): number {
  return Math.max(ANNOTATION_STROKE_MIN_PX, Math.round(Math.min(size.width, size.height) * ANNOTATION_STROKE_SHARE));
}

/** The height of text on a picture of this size. */
export function textSize(size: Size): number {
  return Math.max(ANNOTATION_TEXT_MIN_PX, Math.round(Math.min(size.width, size.height) * ANNOTATION_TEXT_SHARE));
}

/** How far one arrow key press moves or resizes. */
export function keyStep(size: Size): number {
  return Math.max(1, Math.round(Math.min(size.width, size.height) * ANNOTATION_KEY_STEP_SHARE));
}

/** The rectangle between two corners, whichever way it was dragged. */
export function rectBetween(a: Point, b: Point): Rect {
  return { x: Math.min(a.x, b.x), y: Math.min(a.y, b.y), width: Math.abs(a.x - b.x), height: Math.abs(a.y - b.y) };
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(Math.max(value, low), high);
}

/** A crop within the picture, in whole pixels and no smaller than the least a crop keeps. */
export function fitCrop(rect: Rect, size: Size): Rect {
  const width = clamp(Math.round(rect.width), Math.min(ANNOTATION_MIN_CROP_PX, size.width), size.width);
  const height = clamp(Math.round(rect.height), Math.min(ANNOTATION_MIN_CROP_PX, size.height), size.height);
  return { x: clamp(Math.round(rect.x), 0, size.width - width), y: clamp(Math.round(rect.y), 0, size.height - height), width, height };
}

/** The part of the picture that is saved. */
export function outputRect(annotation: Annotation, size: Size): Rect {
  return annotation.crop ?? { x: 0, y: 0, width: size.width, height: size.height };
}

/** A shape moved by dx and dy. */
export function moveShape(shape: Shape, dx: number, dy: number): Shape {
  switch (shape.kind) {
    case "arrow":
      return { ...shape, from: { x: shape.from.x + dx, y: shape.from.y + dy }, to: { x: shape.to.x + dx, y: shape.to.y + dy } };
    case "box":
      return { ...shape, rect: { ...shape.rect, x: shape.rect.x + dx, y: shape.rect.y + dy } };
    case "text":
      return { ...shape, at: { x: shape.at.x + dx, y: shape.at.y + dy } };
  }
}

/** A shape grown by dx and dy: a box from its far corner, an arrow at its head; text keeps its size. */
export function resizeShape(shape: Shape, dx: number, dy: number): Shape {
  switch (shape.kind) {
    case "arrow":
      return { ...shape, to: { x: shape.to.x + dx, y: shape.to.y + dy } };
    case "box":
      return { ...shape, rect: { ...shape.rect, width: Math.max(1, shape.rect.width + dx), height: Math.max(1, shape.rect.height + dy) } };
    case "text":
      return shape;
  }
}

/** Measures a line of text at a height, as the canvas will draw it. */
export type Measure = (text: string, size: number) => number;

/** The rectangle a shape covers, for picking it and drawing its selection. */
export function shapeBounds(shape: Shape, size: Size, measure: Measure): Rect {
  switch (shape.kind) {
    case "arrow":
      return rectBetween(shape.from, shape.to);
    case "box":
      return shape.rect;
    case "text": {
      const height = textSize(size);
      return { x: shape.at.x, y: shape.at.y, width: measure(shape.text, height), height };
    }
  }
}

function distanceToSegment(p: Point, a: Point, b: Point): number {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const length = dx * dx + dy * dy;
  const t = length === 0 ? 0 : clamp(((p.x - a.x) * dx + (p.y - a.y) * dy) / length, 0, 1);
  return Math.hypot(p.x - (a.x + t * dx), p.y - (a.y + t * dy));
}

function inside(p: Point, r: Rect, slack: number): boolean {
  return p.x >= r.x - slack && p.x <= r.x + r.width + slack && p.y >= r.y - slack && p.y <= r.y + r.height + slack;
}

/**
 * The topmost shape a press at p picks, or null: an arrow or a box by its
 * line, within slack, so the picture inside a box stays free to draw on; text anywhere on it.
 */
export function shapeAt(shapes: readonly Shape[], p: Point, slack: number, size: Size, measure: Measure): Shape | null {
  for (let i = shapes.length - 1; i >= 0; i--) {
    const shape = shapes[i]!;
    if (shape.kind === "arrow" && distanceToSegment(p, shape.from, shape.to) <= slack) return shape;
    if (shape.kind === "text" && inside(p, shapeBounds(shape, size, measure), slack)) return shape;
    if (shape.kind === "box") {
      const r = shape.rect;
      const edges: Array<[Point, Point]> = [
        [
          { x: r.x, y: r.y },
          { x: r.x + r.width, y: r.y },
        ],
        [
          { x: r.x + r.width, y: r.y },
          { x: r.x + r.width, y: r.y + r.height },
        ],
        [
          { x: r.x, y: r.y + r.height },
          { x: r.x + r.width, y: r.y + r.height },
        ],
        [
          { x: r.x, y: r.y },
          { x: r.x, y: r.y + r.height },
        ],
      ];
      if (edges.some(([a, b]) => distanceToSegment(p, a, b) <= slack)) return shape;
    }
  }
  return null;
}

/** The two back corners of an arrow's head, which with its tip make the triangle drawn. */
export function arrowHead(arrow: Pick<Arrow, "from" | "to">, length: number): [Point, Point] {
  const angle = Math.atan2(arrow.to.y - arrow.from.y, arrow.to.x - arrow.from.x);
  const spread = Math.PI / 7;
  return [
    { x: arrow.to.x - length * Math.cos(angle - spread), y: arrow.to.y - length * Math.sin(angle - spread) },
    { x: arrow.to.x - length * Math.cos(angle + spread), y: arrow.to.y - length * Math.sin(angle + spread) },
  ];
}

/** Black or white, whichever stands further from the colour, to edge it with. */
export function edgeFor(colour: Colour): string {
  const hex = ANNOTATION_COLOURS[colour];
  const [r, g, b] = [1, 3, 5].map((at) => {
    const c = Number.parseInt(hex.slice(at, at + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  const luminance = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  // Contrast with black is (L + 0.05) / 0.05 and with white 1.05 / (L + 0.05); they meet near 0.179.
  return luminance > 0.179 ? "#000000" : "#ffffff";
}

/** The steps undo and redo walk, around the annotation as it is now. */
export interface History {
  past: Annotation[];
  present: Annotation;
  future: Annotation[];
}

export function startHistory(present: Annotation = EMPTY): History {
  return { past: [], present, future: [] };
}

/** Makes next the present, a step undo returns from; a step that changes nothing is none. */
export function commit(history: History, next: Annotation): History {
  if (next === history.present) return history;
  return { past: [...history.past, history.present].slice(-ANNOTATION_HISTORY_LIMIT), present: next, future: [] };
}

export function undo(history: History): History {
  const previous = history.past.at(-1);
  if (!previous) return history;
  return { past: history.past.slice(0, -1), present: previous, future: [history.present, ...history.future] };
}

export function redo(history: History): History {
  const [next, ...rest] = history.future;
  if (!next) return history;
  return { past: [...history.past, history.present], present: next, future: rest };
}

/** Whether the picture would be saved any different from how it is. */
export function isChanged(annotation: Annotation, size: Size): boolean {
  const crop = annotation.crop;
  return annotation.shapes.length > 0 || (crop !== null && (crop.x !== 0 || crop.y !== 0 || crop.width !== size.width || crop.height !== size.height));
}

/** The annotation with one shape put in place of its own. */
export function replaceShape(annotation: Annotation, shape: Shape): Annotation {
  return { ...annotation, shapes: annotation.shapes.map((each) => (each.id === shape.id ? shape : each)) };
}

/** The annotation without the shape or the crop selected. */
export function removeSelected(annotation: Annotation, selected: string | null): Annotation {
  if (selected === CROP) return annotation.crop ? { ...annotation, crop: null } : annotation;
  if (!annotation.shapes.some((s) => s.id === selected)) return annotation;
  return { ...annotation, shapes: annotation.shapes.filter((s) => s.id !== selected) };
}

/** The selection moved by dx and dy, or with resize grown: a shape, or the crop held within the picture. */
export function nudge(annotation: Annotation, selected: string | null, dx: number, dy: number, resize: boolean, size: Size): Annotation {
  if (selected === CROP) {
    const crop = annotation.crop;
    if (!crop) return annotation;
    const next = resize ? { ...crop, width: crop.width + dx, height: crop.height + dy } : { ...crop, x: crop.x + dx, y: crop.y + dy };
    return { ...annotation, crop: fitCrop(next, size) };
  }
  const shape = annotation.shapes.find((s) => s.id === selected);
  if (!shape) return annotation;
  return replaceShape(annotation, resize ? resizeShape(shape, dx, dy) : moveShape(shape, dx, dy));
}

/** The shape after the one selected, going round, for picking shapes by keyboard. */
export function nextSelection(annotation: Annotation, selected: string | null): string | null {
  const ids = [...annotation.shapes.map((s) => s.id), ...(annotation.crop ? [CROP] : [])];
  if (ids.length === 0) return null;
  const at = selected === null ? -1 : ids.indexOf(selected);
  return ids[(at + 1) % ids.length]!;
}

/** A shape of the tool's kind drawn where nothing was dragged: in the middle, a quarter of the picture across. */
export function shapeInMiddle(kind: "arrow" | "box", id: string, colour: Colour, size: Size): Shape {
  const middle = { x: size.width / 2, y: size.height / 2 };
  const reach = Math.min(size.width, size.height) / 4;
  return kind === "box"
    ? { id, kind, colour, rect: { x: middle.x - reach, y: middle.y - reach / 2, width: reach * 2, height: reach } }
    : { id, kind, colour, from: { x: middle.x - reach, y: middle.y + reach }, to: middle };
}

/** A crop of the middle of the picture, a tenth in from each side, for cropping by keyboard. */
export function cropInMiddle(size: Size): Rect {
  return fitCrop({ x: size.width / 10, y: size.height / 10, width: (size.width * 8) / 10, height: (size.height * 8) / 10 }, size);
}
