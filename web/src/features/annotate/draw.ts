import { ANNOTATION_COLOURS } from "@/config";
import {
  CROP,
  arrowHead,
  edgeFor,
  outputRect,
  shapeBounds,
  strokeWidth,
  textSize,
  type Annotation,
  type Measure,
  type Rect,
  type Shape,
  type Size,
} from "./annotation";

/** The font a label is drawn in; a system face, so the saved picture needs no font loaded. */
export function labelFont(height: number): string {
  return `600 ${height}px system-ui, sans-serif`;
}

// Roughly what a system face's letters take, for a browser with no canvas to measure on.
const AVERAGE_LETTER_WIDTH = 0.6;

/** Measures text as the context draws a label, or roughly without one. */
export function measureWith(context: CanvasRenderingContext2D | null): Measure {
  if (!context) return (text, height) => text.length * height * AVERAGE_LETTER_WIDTH;
  return (text, height) => {
    context.save();
    context.font = labelFont(height);
    const width = context.measureText(text).width;
    context.restore();
    return width;
  };
}

// Each shape is drawn twice, edged in black or white under its colour, so it
// stands out on whatever part of a screenshot it crosses.
function drawShape(context: CanvasRenderingContext2D, shape: Shape, size: Size) {
  const line = strokeWidth(size);
  const colour = ANNOTATION_COLOURS[shape.colour];
  const edge = edgeFor(shape.colour);
  context.lineJoin = "round";
  context.lineCap = "round";
  switch (shape.kind) {
    case "box":
      for (const [style, width] of [
        [edge, line * 2],
        [colour, line],
      ] as const) {
        context.strokeStyle = style;
        context.lineWidth = width;
        context.strokeRect(shape.rect.x, shape.rect.y, shape.rect.width, shape.rect.height);
      }
      break;
    case "arrow": {
      const head = line * 5;
      const [left, right] = arrowHead(shape, head);
      // The shaft stops inside the head, so its round end does not poke through the tip.
      const length = Math.hypot(shape.to.x - shape.from.x, shape.to.y - shape.from.y) || 1;
      const back = Math.min(head * 0.8, length);
      const shaftEnd = { x: shape.to.x - ((shape.to.x - shape.from.x) / length) * back, y: shape.to.y - ((shape.to.y - shape.from.y) / length) * back };
      for (const [style, width] of [
        [edge, line * 2],
        [colour, line],
      ] as const) {
        context.strokeStyle = style;
        context.fillStyle = style;
        context.lineWidth = width;
        context.beginPath();
        context.moveTo(shape.from.x, shape.from.y);
        context.lineTo(shaftEnd.x, shaftEnd.y);
        context.stroke();
        context.beginPath();
        context.moveTo(shape.to.x, shape.to.y);
        context.lineTo(left.x, left.y);
        context.lineTo(right.x, right.y);
        context.closePath();
        if (style === edge) context.stroke();
        context.fill();
      }
      break;
    }
    case "text": {
      const height = textSize(size);
      context.font = labelFont(height);
      context.textBaseline = "top";
      context.lineWidth = Math.max(2, height / 5);
      context.strokeStyle = edge;
      context.strokeText(shape.text, shape.at.x, shape.at.y);
      context.fillStyle = colour;
      context.fillText(shape.text, shape.at.x, shape.at.y);
      break;
    }
  }
}

function drawSelection(context: CanvasRenderingContext2D, rect: Rect, size: Size) {
  const pad = strokeWidth(size) * 2;
  const dash = strokeWidth(size) * 2;
  context.lineWidth = Math.max(1, strokeWidth(size) / 2);
  context.setLineDash([dash, dash]);
  for (const [style, offset] of [
    ["#000000", 0],
    ["#ffffff", dash],
  ] as const) {
    context.strokeStyle = style;
    context.lineDashOffset = offset;
    context.strokeRect(rect.x - pad, rect.y - pad, rect.width + pad * 2, rect.height + pad * 2);
  }
  context.setLineDash([]);
}

/** Draws the picture as it is being edited: everything, the part cropped away dimmed, and the selection outlined. */
export function drawEditing(context: CanvasRenderingContext2D, picture: CanvasImageSource, size: Size, annotation: Annotation, selected: string | null) {
  context.clearRect(0, 0, size.width, size.height);
  context.drawImage(picture, 0, 0, size.width, size.height);
  for (const shape of annotation.shapes) drawShape(context, shape, size);
  const crop = annotation.crop;
  if (crop) {
    context.fillStyle = "rgba(0, 0, 0, 0.55)";
    context.beginPath();
    context.rect(0, 0, size.width, size.height);
    context.rect(crop.x, crop.y, crop.width, crop.height);
    context.fill("evenodd");
  }
  const shape = annotation.shapes.find((s) => s.id === selected);
  if (shape) drawSelection(context, shapeBounds(shape, size, measureWith(context)), size);
  else if (selected === CROP && crop) drawSelection(context, crop, size);
}

/** Draws the picture as it is saved, onto a canvas the size of the part kept. */
export function drawSaved(context: CanvasRenderingContext2D, picture: CanvasImageSource, size: Size, annotation: Annotation) {
  const kept = outputRect(annotation, size);
  context.save();
  context.translate(-kept.x, -kept.y);
  context.drawImage(picture, 0, 0, size.width, size.height);
  for (const shape of annotation.shapes) drawShape(context, shape, size);
  context.restore();
}
