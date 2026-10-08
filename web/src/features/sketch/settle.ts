import type { Editor } from "@tiptap/core";
import type * as Y from "yjs";
import { drawScene } from "./excalidraw";
import { keepShared, sharedIsAhead, sketchMap } from "./live";
import { SKETCH_NODE, readScene } from "./scene";

/**
 * Brings every sketch's scene and drawing in the page up to what is drawn
 * on it together, so a publish carries what is on the canvas even while
 * somebody is still drawing. True when any sketch changed.
 */
export async function settleSketches(editor: Editor, doc: Y.Doc): Promise<boolean> {
  const behind = new Map<string, unknown>();
  editor.state.doc.descendants((node) => {
    const id: unknown = node.attrs.sketchId;
    if (node.type.name === SKETCH_NODE && typeof id === "string" && sharedIsAhead(sketchMap(doc, id), node.attrs.scene)) behind.set(id, node.attrs.scene);
  });
  const drawn = new Map<string, { scene: string; drawing: string | null }>();
  for (const id of behind.keys()) {
    const kept = keepShared(sketchMap(doc, id));
    // The canvas says so to whoever draws on it; the page keeps what it has.
    if (!kept || kept.tooLarge) continue;
    const scene = readScene(kept.scene);
    const drawing = scene && scene.elements.length > 0 ? await drawScene(scene).catch(() => null) : null;
    drawn.set(id, { scene: kept.scene, drawing });
  }
  if (drawn.size === 0 || editor.isDestroyed) return false;
  const tr = editor.state.tr;
  editor.state.doc.descendants((node, pos) => {
    const next = node.type.name === SKETCH_NODE ? drawn.get(node.attrs.sketchId as string) : undefined;
    if (next) tr.setNodeMarkup(pos, undefined, { ...node.attrs, ...next });
  });
  editor.view.dispatch(tr);
  return true;
}
