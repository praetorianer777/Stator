import { Node, mergeAttributes, type Editor } from "@tiptap/core";
import type { Node as PMNode, NodeType } from "@tiptap/pm/model";
import { NodeSelection, type Transaction } from "@tiptap/pm/state";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { t } from "@/i18n";
import { DATE_NODE, DateChip, STATUS_NODE, StatusLabel, isoDay, statusColor, statusLabel, today, type StatusAttrs } from "./InlineValueViews";
import { MATH_BLOCK_NODE, MATH_INLINE_NODE } from "./MathViews";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    inlineValues: {
      /** Puts a status where the caret is and asks for its words. */
      insertStatus: () => ReturnType;
      /** Puts today's date where the caret is and asks for the day. */
      insertDate: () => ReturnType;
    };
  }
}

/** A status, a date or a formula in the document, at the position its node starts, as it was when it was opened. */
export type InlineValueTarget =
  | { kind: typeof STATUS_NODE; pos: number; attrs: StatusAttrs }
  | { kind: typeof DATE_NODE; pos: number; attrs: { date: string } }
  | { kind: typeof MATH_INLINE_NODE | typeof MATH_BLOCK_NODE; pos: number; attrs: { latex: string } };

export interface InlineValueOptions {
  /** Opens the dialog that changes a status or a date; without it they keep what they have. */
  edit: ((target: InlineValueTarget) => void) | undefined;
}

function targetOf(node: PMNode, pos: number): InlineValueTarget | null {
  if (node.type.name === STATUS_NODE) return { kind: STATUS_NODE, pos, attrs: { label: String(node.attrs.label ?? ""), color: statusColor(node.attrs.color) } };
  if (node.type.name === DATE_NODE) return { kind: DATE_NODE, pos, attrs: { date: String(node.attrs.date ?? "") } };
  if (node.type.name === MATH_INLINE_NODE || node.type.name === MATH_BLOCK_NODE)
    return { kind: node.type.name, pos, attrs: { latex: String(node.attrs.latex ?? "") } };
  return null;
}

/** Opens the dialog for the node a view draws. */
export function open(props: NodeViewProps) {
  const pos = props.getPos();
  const target = typeof pos === "number" ? targetOf(props.node, pos) : null;
  if (target) (props.extension.options as InlineValueOptions).edit?.(target);
}

function StatusView(props: NodeViewProps) {
  const label = statusLabel(props.node.attrs.label) ?? "";
  return (
    <NodeViewWrapper as="span" className="doc-inline-value" onClick={() => open(props)}>
      <StatusLabel label={label} color={statusColor(props.node.attrs.color)} />
    </NodeViewWrapper>
  );
}

function DateView(props: NodeViewProps) {
  const day = isoDay(props.node.attrs.date);
  return (
    <NodeViewWrapper as="span" className="doc-inline-value" onClick={() => open(props)}>
      {day && <DateChip day={day} />}
    </NodeViewWrapper>
  );
}

// Enter on a selected status or date opens its dialog, as a click does: the
// arrow keys select an inline atom before they pass it.
export function openSelected(editor: Editor, type: NodeType, options: InlineValueOptions): boolean {
  const { selection } = editor.state;
  if (!(selection instanceof NodeSelection) || selection.node.type !== type || !options.edit) return false;
  const target = targetOf(selection.node, selection.from);
  if (target) options.edit(target);
  return target !== null;
}

// The new node goes where the selection is, and its dialog opens on it.
function insertAndOpen(tr: Transaction, type: NodeType, options: InlineValueOptions, attrs: Record<string, unknown>) {
  const node = type.create(attrs);
  tr.replaceSelectionWith(node, false);
  const pos = tr.selection.from - node.nodeSize;
  const target = tr.doc.nodeAt(pos)?.type === type ? targetOf(node, pos) : null;
  if (target) options.edit?.(target);
}

/**
 * Opens the dialog on the node of this type that the steps from the given
 * one put in, for a node placed by a command that may move it, as a block
 * that replaces an empty line is.
 */
export function openInserted(tr: Transaction, type: NodeType, options: InlineValueOptions, firstStep: number, from: number) {
  const start = tr.mapping.slice(firstStep).map(from, -1);
  let target: InlineValueTarget | null = null;
  tr.doc.nodesBetween(start, Math.min(tr.doc.content.size, start + 2), (node, pos) => {
    if (target) return false;
    if (node.type === type) target = targetOf(node, pos);
    return !target;
  });
  if (target) options.edit?.(target);
}

/** A coloured label in running text, with words of the author's own. */
export const Status = Node.create<InlineValueOptions>({
  name: STATUS_NODE,
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  // Ahead of the list item's Enter, which would split the item over a selected status.
  priority: 1000,
  addOptions() {
    return { edit: undefined };
  },
  addAttributes() {
    return {
      label: {
        default: t.inlineValues.status.defaultLabel,
        parseHTML: (el) => statusLabel(el.getAttribute("data-label")) ?? t.inlineValues.status.defaultLabel,
        renderHTML: (attrs) => ({ "data-label": attrs.label }),
      },
      color: {
        default: "neutral",
        parseHTML: (el) => statusColor(el.getAttribute("data-status-label")),
        renderHTML: (attrs) => ({ "data-status-label": attrs.color }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "span[data-status-label][data-label]", getAttrs: (el) => (statusLabel((el as HTMLElement).getAttribute("data-label")) ? null : false) }];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["span", mergeAttributes(HTMLAttributes, { class: "doc-status" }), String(node.attrs.label ?? "")];
  },
  renderText({ node }) {
    return String(node.attrs.label ?? "");
  },
  addNodeView() {
    return ReactNodeViewRenderer(StatusView, { as: "span" });
  },
  addKeyboardShortcuts() {
    return { Enter: () => openSelected(this.editor, this.type, this.options) };
  },
  addCommands() {
    return {
      insertStatus:
        () =>
        ({ tr, dispatch }) => {
          if (dispatch) insertAndOpen(tr, this.type, this.options, { label: t.inlineValues.status.defaultLabel, color: "neutral" });
          return true;
        },
    };
  },
});

/** A day in running text, stored as YYYY-MM-DD and shown in each reader's own format. */
export const DateNode = Node.create<InlineValueOptions>({
  name: DATE_NODE,
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  // Ahead of the list item's Enter, which would split the item over a selected date.
  priority: 1000,
  addOptions() {
    return { edit: undefined };
  },
  addAttributes() {
    return {
      date: {
        default: null,
        parseHTML: (el) => isoDay(el.getAttribute("data-date")),
        renderHTML: (attrs) => ({ "data-date": attrs.date, datetime: attrs.date }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "time[data-date]", getAttrs: (el) => (isoDay((el as HTMLElement).getAttribute("data-date")) ? null : false) }];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["time", mergeAttributes(HTMLAttributes, { class: "doc-date" }), String(node.attrs.date ?? "")];
  },
  renderText({ node }) {
    return String(node.attrs.date ?? "");
  },
  addNodeView() {
    return ReactNodeViewRenderer(DateView, { as: "span" });
  },
  addKeyboardShortcuts() {
    return { Enter: () => openSelected(this.editor, this.type, this.options) };
  },
  addCommands() {
    return {
      insertDate:
        () =>
        ({ tr, dispatch }) => {
          if (dispatch) insertAndOpen(tr, this.type, this.options, { date: today() });
          return true;
        },
    };
  },
});
