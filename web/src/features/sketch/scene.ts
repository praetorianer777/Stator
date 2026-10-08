import {
  SKETCH_APP_STATE_KEYS,
  SKETCH_BACKGROUND,
  SKETCH_ELEMENT_TYPES,
  SKETCH_MAX_ELEMENTS,
  SKETCH_SCENE_MAX_LENGTH,
  SKETCH_TITLE_MAX_LENGTH,
} from "@/config";
import { safeHref } from "@/features/editor/schema";

export const SKETCH_NODE = "sketch";

/** One Excalidraw element as a scene stores it; Excalidraw owns its other fields. */
export interface SketchElement {
  id: string;
  type: string;
  isDeleted?: boolean;
  link?: string | null;
  [field: string]: unknown;
}

/** What a sketch keeps of Excalidraw's scene: its elements and its background. */
export interface SketchScene {
  elements: SketchElement[];
  appState: { viewBackgroundColor: string };
}

/** A scene with nothing drawn yet, as a new sketch starts. */
export function emptyScene(): string {
  return JSON.stringify({ elements: [], appState: { viewBackgroundColor: SKETCH_BACKGROUND } } satisfies SketchScene);
}

function isElement(value: unknown): value is SketchElement {
  return typeof value === "object" && value !== null && typeof (value as SketchElement).id === "string" && typeof (value as SketchElement).type === "string";
}

/** A stored scene read back, or null when it is no scene. */
export function readScene(value: unknown): SketchScene | null {
  if (typeof value !== "string") return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) return null;
  const { elements, appState } = parsed as { elements?: unknown; appState?: { viewBackgroundColor?: unknown } };
  if (!Array.isArray(elements) || !elements.every(isElement)) return null;
  const background = typeof appState?.viewBackgroundColor === "string" ? appState.viewBackgroundColor : SKETCH_BACKGROUND;
  return { elements, appState: { viewBackgroundColor: background } };
}

/** A title as the API keeps it: trimmed, cut at its limit, null when empty. */
export function sketchTitle(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  return trimmed ? [...trimmed].slice(0, SKETCH_TITLE_MAX_LENGTH).join("") : null;
}

const KEPT_TYPES: readonly string[] = SKETCH_ELEMENT_TYPES;

/** A scene to keep, and whether anything Excalidraw drew had to be left out of it. */
export interface KeptScene {
  scene: string;
  leftOut: boolean;
  tooLarge: boolean;
}

/** One element as a sketch keeps it, without data of other programs or links it does not take; null for a kind it does not take. */
export function keepElement(element: SketchElement): SketchElement | null {
  if (!KEPT_TYPES.includes(element.type) || (element.fileId !== undefined && element.fileId !== null)) return null;
  const { customData: _customData, ...rest } = element;
  const link = typeof rest.link === "string" && safeHref(rest.link) ? rest.link : null;
  return { ...rest, link };
}

/**
 * What a sketch keeps of the canvas: the elements still on it, of the kinds
 * the API takes, with links only to addresses it takes and no data of other
 * programs, and the state named in SKETCH_APP_STATE_KEYS.
 */
export function keepScene(elements: readonly SketchElement[], appState: Record<string, unknown>): KeptScene {
  let leftOut = false;
  const kept: SketchElement[] = [];
  for (const element of elements) {
    if (element.isDeleted) continue;
    const one = keepElement(element);
    if (one) kept.push(one);
    else leftOut = true;
  }
  const state: Record<string, unknown> = {};
  for (const key of SKETCH_APP_STATE_KEYS) if (typeof appState[key] === "string") state[key] = appState[key];
  const scene = JSON.stringify({ elements: kept, appState: state });
  return { scene, leftOut, tooLarge: kept.length > SKETCH_MAX_ELEMENTS || [...scene].length > SKETCH_SCENE_MAX_LENGTH };
}

/** An element's version, as Excalidraw reads one a scene left out. */
export function versionOf(element: SketchElement): number {
  return typeof element.version === "number" && element.version > 0 ? element.version : 1;
}

/** An element's version nonce, which settles two copies of one version. */
export function versionNonceOf(element: SketchElement): number {
  return typeof element.versionNonce === "number" ? element.versionNonce : 0;
}

/**
 * Whether two scenes draw the same: the same background and the same
 * version of every shape still on them. Excalidraw gives a shape a new
 * version with every change, so this is what changed means.
 */
export function sameDrawing(a: SketchScene | null, b: SketchScene | null): boolean {
  const mark = (scene: SketchScene | null) =>
    scene
      ? [
          scene.appState.viewBackgroundColor,
          ...scene.elements
            .filter((element) => !element.isDeleted)
            .map((element) => `${element.id} ${versionOf(element)} ${versionNonceOf(element)}`)
            .sort(),
        ].join("\n")
      : "";
  return mark(a) === mark(b);
}

/** Whether a scene has anything drawn in it. */
export function hasDrawing(scene: SketchScene | null): boolean {
  return !!scene && scene.elements.some((element) => !element.isDeleted);
}
