import { useId } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Textarea } from "@/components/ui";
import { DIAGRAM_DEFAULT_SOURCE, DIAGRAM_MAX_LENGTH, DIAGRAM_PREVIEW_DELAY_MS, DIAGRAM_SOURCE_ROWS } from "@/config";
import { t } from "@/i18n";
import { DIAGRAM_NODE, DiagramDrawing, diagramSource, useDiagram } from "./DiagramViews";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    diagram: {
      /** Puts a diagram with a sketch to start from where the caret is. */
      insertDiagram: () => ReturnType;
    };
  }
}

// The source is edited in place and drawn below it as it is typed: the text
// is what is kept, and the drawing shows whether it says what was meant.
function DiagramNodeView({ node, updateAttributes, editor }: NodeViewProps) {
  const id = useId();
  const source = typeof node.attrs.source === "string" ? node.attrs.source : "";
  const kept = diagramSource(source);
  const drawn = useDiagram(kept ?? "", DIAGRAM_PREVIEW_DELAY_MS);
  return (
    <NodeViewWrapper className="doc-diagram doc-diagram-edit" data-diagram-edit="" contentEditable={false}>
      <label htmlFor={`${id}-source`} className="doc-diagram-label">
        {t.editor.diagram.source}
      </label>
      <Textarea
        id={`${id}-source`}
        value={source}
        rows={DIAGRAM_SOURCE_ROWS}
        maxLength={DIAGRAM_MAX_LENGTH}
        spellCheck={false}
        autoComplete="off"
        readOnly={!editor.isEditable}
        className="font-mono text-sm"
        data-diagram-source=""
        onChange={(event) => updateAttributes({ source: event.target.value })}
      />
      <div className="doc-diagram-preview" data-diagram-preview="" aria-live="polite">
        {kept ? <DiagramDrawing source={kept} drawn={drawn} /> : <p className="doc-diagram-error">{t.editor.diagram.empty}</p>}
      </div>
    </NodeViewWrapper>
  );
}

/** A diagram written as Mermaid text, drawn for each reader. */
export const Diagram = Node.create({
  name: DIAGRAM_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addAttributes() {
    return {
      source: {
        default: DIAGRAM_DEFAULT_SOURCE,
        parseHTML: (el) => diagramSource(el.textContent) ?? DIAGRAM_DEFAULT_SOURCE,
        rendered: false,
      },
    };
  },
  parseHTML() {
    // Ahead of the code block's, which would take any pre.
    return [
      { tag: "pre[data-diagram]", priority: 60, preserveWhitespace: "full", getAttrs: (el) => (diagramSource((el as HTMLElement).textContent) ? null : false) },
    ];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["pre", mergeAttributes(HTMLAttributes, { "data-diagram": "" }), String(node.attrs.source ?? "")];
  },
  renderText({ node }) {
    return String(node.attrs.source ?? "");
  },
  addNodeView() {
    return ReactNodeViewRenderer(DiagramNodeView);
  },
  addCommands() {
    return {
      insertDiagram:
        () =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: { source: DIAGRAM_DEFAULT_SOURCE } }),
    };
  },
});
