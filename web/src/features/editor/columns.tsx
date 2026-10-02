import type { CSSProperties } from "react";
import { Node, findParentNode, mergeAttributes } from "@tiptap/core";
import { Fragment, type Node as PMNode } from "@tiptap/pm/model";
import { TextSelection, type EditorState } from "@tiptap/pm/state";
import { COLUMN_LAYOUTS, COLUMN_SHARE_MAX, COLUMN_SHARE_MIN } from "@/config";

export type ColumnLayout = (typeof COLUMN_LAYOUTS)[number];
export type ColumnLayoutKey = ColumnLayout["key"];
export type ColumnCount = ColumnLayout["widths"]["length"];

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    columns: {
      /** Puts the selected blocks in the first of that many columns, the others empty. */
      setColumns: (count: ColumnCount) => ReturnType;
      /** Gives the columns around the caret a layout, adding or folding in a column as it needs. */
      setColumnLayout: (key: ColumnLayoutKey) => ReturnType;
      /** Takes the columns away and keeps their blocks, one column after another. */
      unsetColumns: () => ReturnType;
    };
  }
}

/** A stored share of the row, or null for one the server would refuse. */
export function columnShare(value: unknown): number | null {
  return typeof value === "number" && Number.isInteger(value) && value >= COLUMN_SHARE_MIN && value <= COLUMN_SHARE_MAX ? value : null;
}

/** The style that gives a column its share; none leaves it an even one. */
export function columnStyle(width: unknown): CSSProperties | undefined {
  const share = columnShare(width);
  return share === null ? undefined : ({ "--column-share": String(share) } as CSSProperties);
}

/** The named layout these columns have, or undefined when their shares match none. */
export function layoutOf(columns: PMNode): ColumnLayout | undefined {
  const widths: Array<number | null> = [];
  columns.forEach((column) => void widths.push(columnShare(column.attrs.width)));
  return COLUMN_LAYOUTS.find((layout) => layout.widths.length === widths.length && layout.widths.every((w, i) => w === widths[i]));
}

const findColumns = findParentNode((node) => node.type.name === "columns");

/** The column section around the caret, if any. */
export function columnsAround(state: EditorState) {
  return findColumns(state.selection);
}

/** The layout of the columns around the caret: a named one, "custom", or null outside any. */
export function layoutAround(state: EditorState): ColumnLayoutKey | "custom" | null {
  const found = columnsAround(state);
  return found ? (layoutOf(found.node)?.key ?? "custom") : null;
}

/** A section of two or three columns side by side, which stack on a narrow screen. */
export const Columns = Node.create({
  name: "columns",
  group: "block",
  content: "column{2,3}",
  defining: true,
  parseHTML() {
    return [{ tag: "div[data-columns]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-columns": "", class: "doc-columns" }), 0];
  },
  addCommands() {
    return {
      setColumns:
        (count) =>
        ({ state, tr, dispatch }) => {
          const layout = COLUMN_LAYOUTS.find((l) => l.widths.length === count);
          const { $from, $to } = state.selection;
          const range = $from.blockRange($to);
          // Columns inside a column would leave too little room to read.
          if (!layout || !range || columnsAround(state)) return false;
          const { columns, column, paragraph } = state.schema.nodes;
          if (!columns || !column || !paragraph) return false;
          const blocks = state.doc.slice(range.start, range.end).content;
          const made = columns.create(
            null,
            layout.widths.map((width, i) => column.create({ width }, i === 0 ? blocks : paragraph.create())),
          );
          if (!made.firstChild || !range.parent.canReplaceWith(range.startIndex, range.endIndex, columns)) return false;
          if (dispatch) {
            tr.replaceWith(range.start, range.end, made);
            // Inside the first column, where the blocks it took now are.
            const end = range.start + 2 + made.firstChild.content.size;
            tr.setSelection(TextSelection.near(tr.doc.resolve(end), -1)).scrollIntoView();
          }
          return true;
        },
      setColumnLayout:
        (key) =>
        ({ state, tr, dispatch }) => {
          const layout = COLUMN_LAYOUTS.find((l) => l.key === key);
          const found = columnsAround(state);
          const { column, paragraph } = state.schema.nodes;
          if (!layout || !found || !column || !paragraph) return false;
          if (dispatch) {
            const old: PMNode[] = [];
            found.node.forEach((c) => void old.push(c));
            const count = layout.widths.length;
            const kept = old.slice(0, count);
            // A column the layout has no room for joins the last one kept.
            const folded = old.slice(count).flatMap((c) => {
              const blocks: PMNode[] = [];
              c.forEach((b) => void blocks.push(b));
              return blocks;
            });
            const last = kept[kept.length - 1];
            if (folded.length && last) kept[kept.length - 1] = last.copy(last.content.append(Fragment.from(folded)));
            while (kept.length < count) kept.push(column.create(null, paragraph.create()));
            const next = found.node.copy(Fragment.from(kept.map((c, i) => column.create({ ...c.attrs, width: layout.widths[i] }, c.content))));
            const from = state.selection.from;
            tr.replaceWith(found.pos, found.pos + found.node.nodeSize, next);
            // What comes before the caret is unchanged unless its column was folded in.
            tr.setSelection(TextSelection.near(tr.doc.resolve(Math.min(from, found.pos + next.nodeSize - 2)), -1));
          }
          return true;
        },
      unsetColumns:
        () =>
        ({ state, tr, dispatch }) => {
          const found = columnsAround(state);
          if (!found) return false;
          if (dispatch) {
            const from = state.selection.from;
            const blocks: PMNode[] = [];
            let started = 0;
            found.node.forEach((c, offset) => {
              if (found.pos + 1 + offset < from) started++;
              c.forEach((b) => void blocks.push(b));
            });
            tr.replaceWith(found.pos, found.pos + found.node.nodeSize, blocks);
            // The caret's blocks move up by the two tokens of every column begun before it.
            tr.setSelection(TextSelection.near(tr.doc.resolve(Math.max(found.pos, from - 2 * started))));
          }
          return true;
        },
    };
  },
});

/** One column of a section: any blocks, at a share of the row. */
export const Column = Node.create({
  name: "column",
  content: "block+",
  // Backspace at a column's start stays in it rather than joining the one before.
  isolating: true,
  addAttributes() {
    return {
      width: {
        default: null,
        parseHTML: (el) => columnShare(Number(el.getAttribute("data-width"))),
        renderHTML: (attrs) => {
          const share = columnShare(attrs.width);
          return share === null ? {} : { "data-width": share, style: `--column-share: ${share}` };
        },
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-column]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-column": "", class: "doc-column" }), 0];
  },
});
