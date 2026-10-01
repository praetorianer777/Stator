import type { Node as PMNode } from "@tiptap/pm/model";
import { closeHistory } from "@tiptap/pm/history";
import type { EditorState, Transaction } from "@tiptap/pm/state";
import { CellSelection } from "@tiptap/pm/tables";
import { ARMATURE_CREATE_MAX_ITEMS, ARMATURE_SUMMARY_MAX_LENGTH } from "@/config";
import { ARMATURE_ISSUE_NODE } from "@/features/armature/issueKeys";

/** One issue a selection makes: its summary, and where its chip goes. */
export interface SelectionItem {
  summary: string;
  /** Selected text the chip takes the place of; for an item or a row, from and to are the place the chip follows. */
  from: number;
  to: number;
}

/** What a selection becomes: one issue for selected text, or one per list item or table row. */
export interface SelectionPlan {
  /** text: the chip replaces the selection; items: each chip follows its item's text. */
  kind: "text" | "items";
  items: SelectionItem[];
  /** How many more items the selection held than one create files. */
  dropped: number;
  /** The document the places belong to. */
  doc: PMNode;
}

const LISTS = new Set(["bulletList", "orderedList", "taskList"]);
const ITEMS = new Set(["listItem", "taskItem"]);
const CELLS = new Set(["tableCell", "tableHeader"]);

/** A summary as Armature takes it: white space made one space, at most its length. */
export function cleanSummary(text: string): string {
  return [...text.replace(/\s+/g, " ").trim()].slice(0, ARMATURE_SUMMARY_MAX_LENGTH).join("").trim();
}

// A chip already in the text reads as its key, and a line break as a space.
const leafText = (node: PMNode) => (node.type.name === ARMATURE_ISSUE_NODE ? String(node.attrs.key ?? "") : " ");

function textOf(node: PMNode): string {
  return node.textBetween(0, node.content.size, " ", leafText);
}

/** The text of a list item without its nested lists. */
function ownText(item: PMNode): string {
  const parts: string[] = [];
  item.forEach((child) => {
    if (!LISTS.has(child.type.name)) parts.push(textOf(child));
  });
  return parts.join(" ");
}

/** The end of the first textblock in node, at pos, that may hold a chip: where a chip follows its text. */
function chipPlace(node: PMNode, pos: number): number | null {
  let place: number | null = null;
  node.descendants((child, offset) => {
    if (place !== null) return false;
    if (child.isTextblock && !child.type.spec.code && !LISTS.has(child.type.name)) {
      place = pos + 1 + offset + child.nodeSize - 1;
      return false;
    }
    return !LISTS.has(child.type.name);
  });
  return place;
}

/** Whether from to reaches a list item's own content, before its nested lists. */
function ownOverlaps(item: PMNode, pos: number, from: number, to: number): boolean {
  let end = pos + item.nodeSize - 1;
  let found = false;
  item.forEach((child, offset) => {
    if (!found && LISTS.has(child.type.name)) {
      end = pos + 1 + offset;
      found = true;
    }
  });
  return pos + 1 < to && end > from;
}

function itemOf(node: PMNode, pos: number): SelectionItem | null {
  const summary = cleanSummary(ownText(node));
  const at = chipPlace(node, pos);
  return summary && at !== null ? { summary, from: at, to: at } : null;
}

/** A row's item: the first cell with text names it, and its chip follows that text. Header rows make none. */
function rowOf(row: PMNode, pos: number): SelectionItem | null {
  let header = true;
  row.forEach((cell) => {
    if (cell.type.name !== "tableHeader") header = false;
  });
  if (header) return null;
  let found: SelectionItem | null = null;
  row.forEach((cell, offset) => {
    if (found) return;
    const summary = cleanSummary(textOf(cell));
    const at = summary ? chipPlace(cell, pos + 1 + offset) : null;
    if (summary && at !== null) found = { summary, from: at, to: at };
  });
  return found;
}

function plan(kind: SelectionPlan["kind"], items: SelectionItem[], doc: PMNode): SelectionPlan | null {
  if (items.length === 0) return null;
  return {
    kind,
    items: items.slice(0, ARMATURE_CREATE_MAX_ITEMS),
    dropped: Math.max(0, items.length - ARMATURE_CREATE_MAX_ITEMS),
    doc,
  };
}

function rowsOfCells(selection: CellSelection, doc: PMNode): SelectionItem[] {
  const rows = new Set<number>();
  selection.forEachCell((_cell, pos) => {
    const $cell = doc.resolve(pos);
    rows.add($cell.before());
  });
  return [...rows]
    .sort((a, b) => a - b)
    .map((pos) => {
      const row = doc.nodeAt(pos);
      return row ? rowOf(row, pos) : null;
    })
    .filter((item): item is SelectionItem => item !== null);
}

/**
 * What the selection becomes: selected text inside one paragraph, heading
 * or cell is one issue; over list items, one per item; over table rows, one
 * per row. Null when it makes none.
 */
export function planSelection(state: EditorState): SelectionPlan | null {
  const { selection, doc } = state;
  if (selection.empty) return null;
  if (selection instanceof CellSelection) return plan("items", rowsOfCells(selection, doc), doc);
  const { $from, $to, from, to } = selection;

  const sameCell = (() => {
    for (let depth = $from.sharedDepth(to); depth > 0; depth--) {
      if (CELLS.has($from.node(depth).type.name)) return true;
    }
    return false;
  })();
  if (($from.sameParent($to) && $from.parent.isTextblock) || sameCell) {
    if ($from.parent.type.spec.code || $to.parent.type.spec.code) return null;
    const summary = cleanSummary(doc.textBetween(from, to, " ", leafText));
    return summary ? plan("text", [{ summary, from, to }], doc) : null;
  }

  const items: SelectionItem[] = [];
  doc.nodesBetween(from, to, (node, pos) => {
    if (ITEMS.has(node.type.name)) {
      // An item that only holds the selected ones in its nested list is not one of them.
      const item = ownOverlaps(node, pos, from, to) ? itemOf(node, pos) : null;
      if (item) items.push(item);
      return true;
    }
    if (node.type.name === "tableRow") {
      const item = rowOf(node, pos);
      if (item) items.push(item);
      return false;
    }
    return !node.isTextblock;
  });
  return plan("items", items, doc);
}

/**
 * Puts the chip of each issue made into the document, in one transaction so
 * one undo takes them all back: keys holds each item's new key, or null for
 * an item that was not made.
 */
export function placeChips(tr: Transaction, chosen: SelectionPlan, keys: readonly (string | null)[]): Transaction {
  const chip = tr.doc.type.schema.nodes[ARMATURE_ISSUE_NODE];
  if (!chip) return tr;
  closeHistory(tr);
  // From the last item back, so each place is still where the plan found it.
  for (let i = chosen.items.length - 1; i >= 0; i--) {
    const key = keys[i];
    const item = chosen.items[i];
    if (!key || !item) continue;
    const node = chip.create({ key });
    if (chosen.kind === "text") {
      tr.replaceWith(item.from, item.to, node);
      continue;
    }
    const before = tr.doc.resolve(item.to).nodeBefore;
    const spaced = before !== null && !(before.isText && /\s$/.test(before.text ?? ""));
    tr.insert(item.to, spaced ? [tr.doc.type.schema.text(" "), node] : node);
  }
  return tr;
}
