import { useId } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { SelectInput, Textarea } from "@/components/ui";
import { useDiagramLanguages } from "@/api/meta";
import { DIAGRAM_DEFAULT_SOURCE, DIAGRAM_MAX_LENGTH, DIAGRAM_PREVIEW_DELAY_MS, DIAGRAM_SOURCE_ROWS, DIAGRAM_STARTERS } from "@/config";
import { t } from "@/i18n";
import { DIAGRAM_NODE, DiagramDrawing, KrokiPreviewDrawing, diagramSource, krokiLanguage, useDiagram, useKrokiPreview } from "./DiagramViews";

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
  const kroki = krokiLanguage(node.attrs.language);
  const language = kroki ?? "mermaid";
  const served = useDiagramLanguages();
  // A diagram keeps a language this server does not draw, so it is offered too.
  const offered = served.includes(language) ? served : [...served, language];
  const name = languageName(language);
  const pick = (next: string) => {
    // A starter nobody has changed follows the language; anything written is kept.
    const untouched = !source.trim() || source === DIAGRAM_STARTERS[language];
    updateAttributes(untouched ? { language: next, source: DIAGRAM_STARTERS[next] ?? source } : { language: next });
  };
  return (
    <NodeViewWrapper className="doc-diagram doc-diagram-edit" data-diagram-edit="" data-diagram-language={language} contentEditable={false}>
      <div className="doc-diagram-head">
        <label htmlFor={`${id}-source`} className="doc-diagram-label">
          {t.editor.diagram.source(name)}
        </label>
        {offered.length > 1 && (
          <SelectInput
            aria-label={t.editor.diagram.language}
            controlSize="sm"
            value={language}
            disabled={!editor.isEditable}
            data-diagram-language-picker=""
            onChange={(event) => pick(event.target.value)}
          >
            {offered.map((each) => (
              <option key={each} value={each}>
                {languageName(each)}
              </option>
            ))}
          </SelectInput>
        )}
      </div>
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
        {!kept ? (
          <p className="doc-diagram-error">{kroki ? t.editor.diagram.emptyIn(name) : t.editor.diagram.empty}</p>
        ) : kroki ? (
          <KrokiBlockPreview language={kroki} source={kept} />
        ) : (
          <MermaidBlockPreview source={kept} />
        )}
      </div>
    </NodeViewWrapper>
  );
}

function languageName(language: string): string {
  return t.editor.diagram.languages[language] ?? language;
}

function MermaidBlockPreview({ source }: { source: string }) {
  return <DiagramDrawing source={source} drawn={useDiagram(source, DIAGRAM_PREVIEW_DELAY_MS)} />;
}

function KrokiBlockPreview({ language, source }: { language: string; source: string }) {
  return <KrokiPreviewDrawing source={source} preview={useKrokiPreview(language, source, DIAGRAM_PREVIEW_DELAY_MS)} />;
}

/** A diagram written as text in Mermaid, drawn for each reader, or in a language the installation's Kroki draws. */
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
      // Kept as it came, so a diagram Kroki draws stays in its language when
      // the page is edited; a new diagram is Mermaid.
      language: {
        default: "mermaid",
        parseHTML: (el) => krokiLanguage(el.getAttribute("data-language")) ?? "mermaid",
        renderHTML: (attrs) => (krokiLanguage(attrs.language) ? { "data-language": attrs.language } : {}),
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
