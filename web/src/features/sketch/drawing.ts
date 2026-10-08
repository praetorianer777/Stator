import { SKETCH_DRAWING_MAX_LENGTH } from "@/config";

const SVG_NS = "http://www.w3.org/2000/svg";

/**
 * What a sketch's SVG may hold, as the API's document.SketchDrawing; a test
 * holds this copy to api/sketch-drawing-allowlist.json, which the API writes.
 */
export const DRAWING_RULES = {
  elements: [
    "svg",
    "g",
    "path",
    "rect",
    "circle",
    "ellipse",
    "line",
    "polyline",
    "polygon",
    "text",
    "tspan",
    "defs",
    "style",
    "clipPath",
    "mask",
    "title",
    "desc",
  ],
  textElements: ["text", "tspan", "style", "title", "desc"],
  attributes: [
    "version",
    "viewBox",
    "width",
    "height",
    "x",
    "y",
    "x1",
    "y1",
    "x2",
    "y2",
    "cx",
    "cy",
    "r",
    "rx",
    "ry",
    "d",
    "points",
    "fill",
    "fill-opacity",
    "fill-rule",
    "stroke",
    "stroke-width",
    "stroke-linecap",
    "stroke-linejoin",
    "stroke-dasharray",
    "stroke-dashoffset",
    "stroke-miterlimit",
    "stroke-opacity",
    "opacity",
    "transform",
    "font-family",
    "font-size",
    "font-style",
    "font-weight",
    "text-anchor",
    "dominant-baseline",
    "direction",
    "dir",
    "style",
    "id",
    "class",
    "mask",
    "clip-path",
    "clipPathUnits",
    "maskUnits",
    "preserveAspectRatio",
  ],
  reference: String.raw`url\(#[A-Za-z0-9_-]{1,100}\)`,
  forbidden: ["url(", "javascript", "data:", "expression", "\\", "@", "<", ">", "&"],
  fontFace: String.raw`^\s*(?:@font-face \{ font-family: [A-Za-z][A-Za-z0-9 ]{0,40}; src: url\(data:font/woff2;base64,[A-Za-z0-9+/]*={0,2}\); \}\s*)*$`,
} as const;

/** How deeply a drawing's groups may nest, as the API's maxSketchDrawingDepth. */
const MAX_DEPTH = 32;

const ELEMENTS: readonly string[] = DRAWING_RULES.elements;
const TEXT_ELEMENTS: readonly string[] = DRAWING_RULES.textElements;
const ATTRIBUTES: readonly string[] = DRAWING_RULES.attributes;
const REFERENCE = new RegExp(DRAWING_RULES.reference, "g");
const FONT_FACE = new RegExp(DRAWING_RULES.fontFace);

function safeValue(value: string): boolean {
  const bare = value.replace(REFERENCE, "").toLowerCase();
  return !DRAWING_RULES.forbidden.some((word) => bare.includes(word));
}

function cleanAttributes(el: Element, root: boolean) {
  for (const attr of [...el.attributes]) {
    if (root && attr.name === "xmlns") continue;
    if (attr.namespaceURI !== null || !ATTRIBUTES.includes(attr.localName) || !safeValue(attr.value)) el.removeAttributeNode(attr);
  }
}

function cleanChildren(el: Element, depth: number) {
  for (const child of [...el.childNodes]) {
    if (child.nodeType === Node.TEXT_NODE) {
      if (!TEXT_ELEMENTS.includes(el.localName)) child.remove();
      continue;
    }
    if (child.nodeType !== Node.ELEMENT_NODE) {
      child.remove();
      continue;
    }
    const kid = child as Element;
    // A link cannot be followed from a picture, so its shapes stay and it goes.
    if (kid.namespaceURI === SVG_NS && kid.localName === "a") {
      const shapes = [...kid.childNodes];
      kid.replaceWith(...shapes);
      for (const shape of shapes) {
        if (shape.nodeType !== Node.ELEMENT_NODE) shape.remove();
      }
      cleanChildren(el, depth);
      return;
    }
    if (kid.namespaceURI !== SVG_NS || !ELEMENTS.includes(kid.localName) || depth + 1 >= MAX_DEPTH) {
      kid.remove();
      continue;
    }
    if (kid.localName === "style" && !FONT_FACE.test(kid.textContent ?? "")) {
      kid.remove();
      continue;
    }
    cleanAttributes(kid, false);
    cleanChildren(kid, depth + 1);
  }
}

/**
 * An SVG kept to DRAWING_RULES, as the API will check it: what it does not
 * name is taken out, and a link's shapes stay without it. Null for no SVG,
 * or one past SKETCH_DRAWING_MAX_LENGTH once cleaned.
 */
export function cleanDrawing(svg: string): string | null {
  const doc = new DOMParser().parseFromString(svg, "image/svg+xml");
  const root = doc.documentElement;
  if (root.namespaceURI !== SVG_NS || root.localName !== "svg" || doc.getElementsByTagName("parsererror").length > 0) return null;
  cleanAttributes(root, true);
  cleanChildren(root, 0);
  const out = new XMLSerializer().serializeToString(root);
  return [...out].length > SKETCH_DRAWING_MAX_LENGTH ? null : out;
}

/** A drawing as a picture's address: shown as an image, where no script runs and nothing is fetched. */
export function drawingSrc(svg: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}
