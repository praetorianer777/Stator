import { Node, mergeAttributes, type CommandProps } from "@tiptap/core";
import type { NodeType } from "@tiptap/pm/model";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { MATH_DEFAULT_LATEX } from "@/config";
import { t } from "@/i18n";
import { open, openInserted, openSelected, type InlineValueOptions } from "./inlineValues";
import { MATH_BLOCK_NODE, MATH_INLINE_NODE, MathFormula, mathSource } from "./MathViews";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    math: {
      /** Puts a formula where the caret is and asks for its source. */
      insertMathInline: () => ReturnType;
      /** Puts a formula on a line of its own and asks for its source. */
      insertMathBlock: () => ReturnType;
    };
  }
}

function MathView(props: NodeViewProps) {
  const display = props.node.type.name === MATH_BLOCK_NODE;
  const latex = mathSource(props.node.attrs.latex) ?? "";
  return (
    <NodeViewWrapper
      as={display ? "div" : "span"}
      className="doc-math-edit"
      data-math-edit={display ? "block" : "inline"}
      title={t.editor.math.edit}
      contentEditable={false}
      onClick={() => open(props)}
    >
      <MathFormula latex={latex} display={display} />
    </NodeViewWrapper>
  );
}

// insertContent finds the formula a place, a block replacing an empty line,
// and the dialog then opens on wherever it went.
function insertFormula({ tr, commands, dispatch }: CommandProps, type: NodeType, options: InlineValueOptions): boolean {
  const firstStep = tr.steps.length;
  const from = tr.selection.from;
  if (!commands.insertContent({ type: type.name, attrs: { latex: MATH_DEFAULT_LATEX } })) return false;
  if (dispatch) openInserted(tr, type, options, firstStep, from);
  return true;
}

const latexAttribute = {
  latex: {
    default: MATH_DEFAULT_LATEX,
    parseHTML: (el: HTMLElement) => mathSource(el.getAttribute("data-latex")) ?? MATH_DEFAULT_LATEX,
    renderHTML: (attrs: Record<string, unknown>) => ({ "data-latex": attrs.latex }),
  },
};

const hasSource = (el: HTMLElement | string) => (typeof el !== "string" && mathSource(el.getAttribute("data-latex")) ? null : false);

/** A formula in running text, stored as its TeX source and typeset for each reader. */
export const MathInline = Node.create<InlineValueOptions>({
  name: MATH_INLINE_NODE,
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  // Ahead of the list item's Enter, which would split the item over a selected formula.
  priority: 1000,
  addOptions() {
    return { edit: undefined };
  },
  addAttributes() {
    return latexAttribute;
  },
  parseHTML() {
    return [{ tag: "span[data-math-inline][data-latex]", getAttrs: hasSource }];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["span", mergeAttributes(HTMLAttributes, { "data-math-inline": "" }), String(node.attrs.latex ?? "")];
  },
  renderText({ node }) {
    return `$${String(node.attrs.latex ?? "")}$`;
  },
  addNodeView() {
    return ReactNodeViewRenderer(MathView, { as: "span" });
  },
  addKeyboardShortcuts() {
    return { Enter: () => openSelected(this.editor, this.type, this.options) };
  },
  addCommands() {
    return {
      insertMathInline: () => (props) => insertFormula(props, this.type, this.options),
    };
  },
});

/** A formula on a line of its own, centred and typeset larger. */
export const MathBlock = Node.create<InlineValueOptions>({
  name: MATH_BLOCK_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { edit: undefined };
  },
  addAttributes() {
    return latexAttribute;
  },
  parseHTML() {
    return [{ tag: "div[data-math-block][data-latex]", getAttrs: hasSource }];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-math-block": "" }), String(node.attrs.latex ?? "")];
  },
  renderText({ node }) {
    return `$$${String(node.attrs.latex ?? "")}$$`;
  },
  addNodeView() {
    return ReactNodeViewRenderer(MathView);
  },
  addKeyboardShortcuts() {
    return { Enter: () => openSelected(this.editor, this.type, this.options) };
  },
  addCommands() {
    return {
      insertMathBlock: () => (props) => insertFormula(props, this.type, this.options),
    };
  },
});
