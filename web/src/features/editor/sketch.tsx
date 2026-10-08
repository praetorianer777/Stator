import { useEffect, useId, useRef, useState } from "react";
import type { Node as PMNode } from "@tiptap/pm/model";
import { Node } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button, Input } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SKETCH_TITLE_MAX_LENGTH } from "@/config";
import { cleanDrawing } from "@/features/sketch/drawing";
import type { LiveSketches } from "@/features/sketch/live";
import { SketchDrawers, useSketchDrawers } from "@/features/sketch/presence";
import { SketchDialog } from "@/features/sketch/SketchDialog";
import { SketchPending, SketchPicture, useSketchDrawing } from "@/features/sketch/SketchViews";
import { SKETCH_NODE, emptyScene, readScene, sketchTitle } from "@/features/sketch/scene";
import { t } from "@/i18n";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    sketch: {
      /** Puts an empty sketch where the caret is and opens it to draw in. */
      insertSketch: () => ReturnType;
    };
  }
  interface Storage {
    sketch: { openNext: boolean };
  }
}

export interface SketchOptions {
  /** The shared draft, when the page is edited together; sketches are then drawn together too. */
  live: LiveSketches | null;
}

/** Whether another sketch in the document carries the same id, as a pasted copy does. */
function idTakenElsewhere(doc: PMNode, id: string, pos: number | undefined): boolean {
  let taken = false;
  doc.descendants((node, at) => {
    if (taken) return false;
    if (node.type.name === SKETCH_NODE && node.attrs.sketchId === id && at !== pos) taken = true;
    return !taken;
  });
  return taken;
}

// The drawing shows in the page as readers will see it; the canvas opens
// over the window, since a whiteboard needs more room than a column.
function SketchNodeView({ node, updateAttributes, editor, extension, getPos }: NodeViewProps) {
  const s = t.editor.sketch;
  const id = useId();
  const live = (extension.options as SketchOptions).live;
  const sketchId = typeof node.attrs.sketchId === "string" ? node.attrs.sketchId : null;
  const nodeRef = useRef(node);
  nodeRef.current = node;
  const drawers = useSketchDrawers(live?.awareness ?? null, sketchId);
  const state = useSketchDrawing(node.attrs.scene, node.attrs.drawing);
  const title = typeof node.attrs.title === "string" ? node.attrs.title : "";
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const storage = editor.storage.sketch;
    if (!storage.openNext || !editor.isEditable) return;
    storage.openNext = false;
    setOpen(true);
  }, [editor]);
  const editable = editor.isEditable;
  function openCanvas() {
    // A sketch is drawn together under an id of its own, given when it is
    // first opened so that sketches nobody opens never change.
    if (live && (!sketchId || idTakenElsewhere(editor.state.doc, sketchId, getPos()))) updateAttributes({ sketchId: crypto.randomUUID() });
    setOpen(true);
  }
  return (
    <NodeViewWrapper className="doc-sketch doc-sketch-edit" data-sketch-edit="" contentEditable={false}>
      <div className="doc-sketch-preview">
        {state.kind === "drawn" ? (
          <SketchPicture svg={state.svg} title={sketchTitle(title)} />
        ) : state.kind === "empty" ? (
          <p className="doc-sketch-note">{s.empty}</p>
        ) : (
          <SketchPending state={state} />
        )}
        <SketchDrawers drawers={drawers} />
      </div>
      <div className="doc-sketch-tools">
        <label htmlFor={`${id}-title`} className="doc-sketch-label">
          {s.title}
        </label>
        <Input
          id={`${id}-title`}
          value={title}
          maxLength={SKETCH_TITLE_MAX_LENGTH}
          readOnly={!editable}
          controlSize="sm"
          aria-describedby={`${id}-hint`}
          data-sketch-title=""
          onChange={(event) => updateAttributes({ title: event.target.value || null })}
        />
        {editable && (
          <Button type="button" variant="secondary" size="sm" onClick={openCanvas} data-action="edit-sketch">
            <Icon.Sketch />
            {s.edit}
          </Button>
        )}
        <p id={`${id}-hint`} className="doc-sketch-hint">
          {s.titleHint}
        </p>
      </div>
      {open && (!live || sketchId) && (
        <SketchDialog
          scene={typeof node.attrs.scene === "string" && readScene(node.attrs.scene) ? node.attrs.scene : emptyScene()}
          live={live && sketchId ? { doc: live.doc, awareness: live.awareness, sketchId, saved: () => nodeRef.current.attrs.scene } : null}
          onDone={(scene, drawing) => updateAttributes({ scene, drawing })}
          onClose={() => setOpen(false)}
        />
      )}
    </NodeViewWrapper>
  );
}

/** A drawing made on an Excalidraw canvas: its scene, the SVG drawn from it, and its title. */
export const Sketch = Node.create<SketchOptions>({
  name: SKETCH_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { live: null };
  },
  addStorage() {
    return { openNext: false };
  },
  addAttributes() {
    return {
      scene: { default: emptyScene(), rendered: false },
      drawing: { default: null, rendered: false },
      title: { default: null, rendered: false },
      sketchId: { default: null, rendered: false },
    };
  },
  parseHTML() {
    return [
      {
        tag: "figure[data-sketch]",
        getAttrs: (el) => {
          const scene = (el as HTMLElement).getAttribute("data-scene");
          const drawing = (el as HTMLElement).getAttribute("data-drawing");
          return scene && readScene(scene)
            ? { scene, drawing: drawing ? cleanDrawing(drawing) : null, title: sketchTitle((el as HTMLElement).getAttribute("data-title")) }
            : false;
        },
      },
    ];
  },
  renderHTML({ node }) {
    return [
      "figure",
      {
        "data-sketch": "",
        "data-scene": String(node.attrs.scene ?? ""),
        "data-drawing": String(node.attrs.drawing ?? ""),
        "data-title": String(node.attrs.title ?? ""),
      },
    ];
  },
  renderText({ node }) {
    return String(node.attrs.title ?? "");
  },
  addNodeView() {
    return ReactNodeViewRenderer(SketchNodeView);
  },
  addCommands() {
    return {
      insertSketch:
        () =>
        ({ commands, dispatch }) => {
          const sketchId = this.options.live ? crypto.randomUUID() : null;
          const done = commands.insertContent({ type: this.name, attrs: { scene: emptyScene(), drawing: null, title: null, sketchId } });
          if (done && dispatch) this.storage.openNext = true;
          return done;
        },
    };
  },
});
