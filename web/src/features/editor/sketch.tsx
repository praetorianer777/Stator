import { useEffect, useId, useState } from "react";
import { Node } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button, Input } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SKETCH_TITLE_MAX_LENGTH } from "@/config";
import { cleanDrawing } from "@/features/sketch/drawing";
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

// The drawing shows in the page as readers will see it; the canvas opens
// over the window, since a whiteboard needs more room than a column.
function SketchNodeView({ node, updateAttributes, editor }: NodeViewProps) {
  const s = t.editor.sketch;
  const id = useId();
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
          <Button type="button" variant="secondary" size="sm" onClick={() => setOpen(true)} data-action="edit-sketch">
            <Icon.Sketch />
            {s.edit}
          </Button>
        )}
        <p id={`${id}-hint`} className="doc-sketch-hint">
          {s.titleHint}
        </p>
      </div>
      {open && (
        <SketchDialog
          scene={typeof node.attrs.scene === "string" && readScene(node.attrs.scene) ? node.attrs.scene : emptyScene()}
          onDone={(scene, drawing) => updateAttributes({ scene, drawing })}
          onClose={() => setOpen(false)}
        />
      )}
    </NodeViewWrapper>
  );
}

/** A drawing made on an Excalidraw canvas: its scene, the SVG drawn from it, and its title. */
export const Sketch = Node.create({
  name: SKETCH_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addStorage() {
    return { openNext: false };
  },
  addAttributes() {
    return {
      scene: { default: emptyScene(), rendered: false },
      drawing: { default: null, rendered: false },
      title: { default: null, rendered: false },
    };
  },
  parseHTML() {
    return [
      {
        tag: "figure[data-sketch]",
        getAttrs: (el) => {
          const scene = (el as HTMLElement).getAttribute("data-scene");
          const drawing = (el as HTMLElement).getAttribute("data-drawing");
          return scene && readScene(scene) ? { scene, drawing: drawing ? cleanDrawing(drawing) : null, title: sketchTitle((el as HTMLElement).getAttribute("data-title")) } : false;
        },
      },
    ];
  },
  renderHTML({ node }) {
    return ["figure", { "data-sketch": "", "data-scene": String(node.attrs.scene ?? ""), "data-drawing": String(node.attrs.drawing ?? ""), "data-title": String(node.attrs.title ?? "") }];
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
          const done = commands.insertContent({ type: this.name, attrs: { scene: emptyScene(), drawing: null, title: null } });
          if (done && dispatch) this.storage.openNext = true;
          return done;
        },
    };
  },
});
