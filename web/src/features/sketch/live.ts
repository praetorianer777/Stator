import type * as Y from "yjs";
import type { Awareness } from "y-protocols/awareness";
import { SKETCH_BACKGROUND, SKETCH_SYNC_INTERVAL_MS } from "@/config";
import { keepElement, keepScene, readScene, sameDrawing, versionNonceOf, versionOf, type KeptScene, type SketchElement, type SketchScene } from "./scene";

// A sketch drawn together keeps its shapes in the page's shared draft, one
// top-level map per sketch, so they share the room, its storage and its
// permissions with the body. A top-level map rather than one nested in a
// map of sketches: two browsers making the same nested map at once would
// each make their own, and one of them would lose every shape in it.
const MAP_PREFIX = "sketch:";
const ELEMENT_PREFIX = "e:";
const BACKGROUND = "background";
/** The awareness field that says which sketch a person has open, and where their pointer is. */
export const PRESENCE_FIELD = "sketch";

/** A sketch's shapes in a shared draft, named by the node's sketchId. */
export function sketchMap(doc: Y.Doc, sketchId: string): Y.Map<unknown> {
  return doc.getMap(MAP_PREFIX + sketchId);
}

function isElement(value: unknown): value is SketchElement {
  return typeof value === "object" && value !== null && typeof (value as SketchElement).id === "string" && typeof (value as SketchElement).type === "string";
}

/**
 * Whether one copy of a shape replaces another, as Excalidraw settles them:
 * the later version, and of two equal versions the one with the lower nonce.
 */
export function supersedes(next: SketchElement, was: SketchElement | undefined): boolean {
  if (!was) return true;
  const a = versionOf(next);
  const b = versionOf(was);
  return a > b || (a === b && versionNonceOf(next) < versionNonceOf(was));
}

function byIndex(a: SketchElement, b: SketchElement): number {
  const ai = typeof a.index === "string" ? a.index : "";
  const bi = typeof b.index === "string" ? b.index : "";
  if (ai !== bi) return ai < bi ? -1 : 1;
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

/** Every shape the shared draft holds for a sketch, deleted ones included, back to front. */
export function sharedElements(map: Y.Map<unknown>): SketchElement[] {
  const elements: SketchElement[] = [];
  for (const [key, value] of map) if (key.startsWith(ELEMENT_PREFIX) && isElement(value)) elements.push(value);
  return elements.sort(byIndex);
}

function sharedBackground(map: Y.Map<unknown>): string | undefined {
  const background = map.get(BACKGROUND);
  return typeof background === "string" ? background : undefined;
}

/** The scene the shared draft holds for a sketch, or null when nobody has drawn on it together. */
export function sharedScene(map: Y.Map<unknown>): SketchScene | null {
  const elements = sharedElements(map);
  const background = sharedBackground(map);
  if (elements.length === 0 && background === undefined) return null;
  return { elements, appState: { viewBackgroundColor: background ?? SKETCH_BACKGROUND } };
}

/** The shared scene as a sketch's attribute keeps it. */
export function keepShared(map: Y.Map<unknown>): KeptScene | null {
  const scene = sharedScene(map);
  return scene ? keepScene(scene.elements, scene.appState) : null;
}

/** Whether the shared draft holds shapes a sketch's saved scene lacks, so its drawing is behind. */
export function sharedIsAhead(map: Y.Map<unknown>, savedScene: unknown): boolean {
  const kept = keepShared(map);
  if (!kept) return false;
  const saved = readScene(savedScene);
  return !saved || !sameDrawing(readScene(kept.scene), saved);
}

/**
 * Writes what the canvas holds into the shared draft: every shape of a kind
 * a sketch takes that is newer than the shared copy, deleted ones included
 * so the others delete them too. Returns how many it wrote.
 */
export function writeShared(map: Y.Map<unknown>, elements: readonly SketchElement[], background: string | undefined, origin: unknown): number {
  let written = 0;
  const write = () => {
    for (const element of elements) {
      const kept = keepElement(element);
      if (!kept) continue;
      const key = ELEMENT_PREFIX + kept.id;
      const was = map.get(key);
      if (!supersedes(kept, isElement(was) ? was : undefined)) continue;
      // A copy: Excalidraw changes its own elements in place, and the shared
      // value must not change with them unseen.
      map.set(key, JSON.parse(JSON.stringify(kept)) as SketchElement);
      written++;
    }
    if (background !== undefined && sharedBackground(map) !== background) {
      map.set(BACKGROUND, background);
      written++;
    }
  };
  if (map.doc) map.doc.transact(write, origin);
  else write();
  return written;
}

/** What a link needs of a canvas: what is on it, and a way to take the others' shapes in. */
export interface SketchCanvas {
  /** Every element on the canvas, deleted ones included. */
  elements: () => readonly SketchElement[];
  background: () => string;
  /** Takes the others' shapes and background in, without making them this person's to undo. */
  apply: (elements: SketchElement[], background: string | null) => void;
}

/**
 * Keeps one canvas and a sketch's shapes in the shared draft in step: the
 * others' changes are taken in as they arrive, and this person's go out a
 * moment after they are made. A shape both changed goes to whichever copy
 * Excalidraw would keep, so every canvas ends with the same one.
 */
export class SketchLink {
  readonly map: Y.Map<unknown>;
  private canvas: SketchCanvas;
  private background: string | undefined;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private interval: number;

  constructor(doc: Y.Doc, sketchId: string, canvas: SketchCanvas, interval = SKETCH_SYNC_INTERVAL_MS) {
    this.map = sketchMap(doc, sketchId);
    this.canvas = canvas;
    this.interval = interval;
    this.background = sharedBackground(this.map);
    this.map.observe(this.observe);
    const elements = sharedElements(this.map);
    if (elements.length > 0 || this.background !== undefined) canvas.apply(elements.map(copy), this.background ?? null);
    // The first person on a sketch brings its saved shapes in this way.
    this.push();
  }

  /** The canvas changed; its shapes go out once the interval has passed. */
  changed() {
    this.timer ??= setTimeout(() => {
      this.timer = undefined;
      this.push();
    }, this.interval);
  }

  /** Sends what the canvas holds now. */
  push() {
    clearTimeout(this.timer);
    this.timer = undefined;
    const background = this.canvas.background();
    writeShared(this.map, this.canvas.elements(), background === this.background ? undefined : background, this);
    this.background = sharedBackground(this.map) ?? this.background;
  }

  private observe = (event: Y.YMapEvent<unknown>) => {
    if (event.transaction.origin === this) return;
    const changed: SketchElement[] = [];
    let background: string | null = null;
    for (const key of event.keysChanged) {
      if (key === BACKGROUND) {
        background = sharedBackground(this.map) ?? null;
        if (background !== null) this.background = background;
        continue;
      }
      const value = this.map.get(key);
      if (key.startsWith(ELEMENT_PREFIX) && isElement(value)) changed.push(copy(value));
    }
    if (changed.length === 0 && background === null) return;
    this.canvas.apply(changed.sort(byIndex), background);
    // A shape this canvas holds a newer copy of, as one being drawn, goes out again.
    this.push();
  };

  destroy() {
    clearTimeout(this.timer);
    this.map.unobserve(this.observe);
  }
}

function copy(element: SketchElement): SketchElement {
  return structuredClone(element);
}

/** Where a person is on a shared sketch, as their awareness says. */
export interface SketchPresence {
  id: string;
  pointer: { x: number; y: number; tool: "pointer" | "laser" } | null;
  button: "up" | "down";
  selected: string[];
}

/** Somebody else on a sketch, in the shape Excalidraw draws collaborators in. */
export interface SketchCollaborator {
  id: string;
  username: string;
  color: { background: string; stroke: string };
  pointer?: { x: number; y: number; tool: "pointer" | "laser" };
  button: "up" | "down";
  selectedElementIds: Record<string, true>;
}

function presenceOf(state: Record<string, unknown> | undefined): SketchPresence | null {
  const presence = state?.[PRESENCE_FIELD] as Partial<SketchPresence> | null | undefined;
  return presence && typeof presence.id === "string" ? (presence as SketchPresence) : null;
}

/** Everybody else who has a sketch open, keyed by their browser's awareness id. */
export function sketchCollaborators(states: Map<number, Record<string, unknown>>, selfClient: number, sketchId: string): Map<string, SketchCollaborator> {
  const found = new Map<string, SketchCollaborator>();
  for (const [client, state] of states) {
    const presence = presenceOf(state);
    if (client === selfClient || presence?.id !== sketchId) continue;
    const user = (state.user ?? {}) as { id?: string; name?: string; color?: string };
    const color = String(user.color ?? "");
    found.set(String(client), {
      id: String(user.id ?? client),
      username: String(user.name ?? ""),
      color: { background: color, stroke: color },
      ...(presence.pointer ? { pointer: presence.pointer } : {}),
      button: presence.button === "down" ? "down" : "up",
      selectedElementIds: Object.fromEntries((Array.isArray(presence.selected) ? presence.selected : []).map((id) => [String(id), true as const])),
    });
  }
  return found;
}

/** Says which sketch this person has open, or none. */
export function setSketchPresence(awareness: Awareness, presence: SketchPresence | null) {
  awareness.setLocalStateField(PRESENCE_FIELD, presence);
}

/** Who else has a sketch open, once each and by name, as the page lists the people editing it. */
export function sketchDrawers(
  states: Map<number, Record<string, unknown>>,
  selfClient: number,
  sketchId: string,
): Array<{ id: string; name: string; color: string }> {
  const self = (states.get(selfClient)?.user as { id?: string } | undefined)?.id;
  const seen = new Map<string, { id: string; name: string; color: string }>();
  for (const [client, state] of states) {
    if (client === selfClient || presenceOf(state)?.id !== sketchId) continue;
    const user = (state.user ?? {}) as { id?: string; name?: string; color?: string };
    if (!user.id || user.id === self || seen.has(user.id)) continue;
    seen.set(user.id, { id: user.id, name: String(user.name ?? ""), color: String(user.color ?? "") });
  }
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
}

/** The shared draft and this person in it, as a sketch drawn together needs them. */
export interface LiveSketches {
  doc: Y.Doc;
  awareness: Awareness;
}
