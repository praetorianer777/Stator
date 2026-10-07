import { Extension } from "@tiptap/core";
import type { Node as PMNode } from "@tiptap/pm/model";
import { closeHistory } from "@tiptap/pm/history";
import { Plugin, PluginKey, TextSelection, type EditorState, type Transaction } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import { yUndoPluginKey } from "@tiptap/y-tiptap";

/** One place the query was found, as document positions. */
export interface Match {
  from: number;
  to: number;
}

export interface FindState {
  query: string;
  caseSensitive: boolean;
  matches: Match[];
  /** The index of the match the bar is on, or -1 when there is none. */
  current: number;
  decorations: DecorationSet;
}

type FindMeta = { kind: "query"; query: string; caseSensitive: boolean } | { kind: "go"; index: number } | { kind: "after"; pos: number };

export const findKey = new PluginKey<FindState>("findReplace");

// Stands in for an inline node that is not text, such as a chip or a mention,
// so no match runs into or across one; the query never holds it.
const NOT_TEXT = "￼";

// Lower case one code unit at a time, so every index still names the same
// position; a letter whose lower case is longer keeps its own form.
function fold(text: string): string {
  let out = "";
  for (let i = 0; i < text.length; i++) {
    const ch = text.charAt(i);
    const lower = ch.toLowerCase();
    out += lower.length === 1 ? lower : ch;
  }
  return out;
}

/** Every place the query occurs in the text of one text block, across marks but never into an inline node. */
export function findMatches(doc: PMNode, query: string, caseSensitive: boolean): Match[] {
  if (!query || query.includes(NOT_TEXT)) return [];
  const needle = caseSensitive ? query : fold(query);
  const matches: Match[] = [];
  doc.descendants((node, pos) => {
    if (!node.isTextblock) return true;
    let text = "";
    node.forEach((child) => {
      text += child.isText ? (child.text ?? "") : NOT_TEXT.repeat(child.nodeSize);
    });
    const haystack = caseSensitive ? text : fold(text);
    const start = pos + 1;
    for (let i = haystack.indexOf(needle); i >= 0; i = haystack.indexOf(needle, i + needle.length)) {
      matches.push({ from: start + i, to: start + i + needle.length });
    }
    return false;
  });
  return matches;
}

function firstFrom(matches: Match[], pos: number): number {
  if (matches.length === 0) return -1;
  const i = matches.findIndex((m) => m.from >= pos);
  return i < 0 ? 0 : i;
}

function decorate(doc: PMNode, matches: Match[], current: number): DecorationSet {
  if (matches.length === 0) return DecorationSet.empty;
  return DecorationSet.create(
    doc,
    matches.map((m, i) => Decoration.inline(m.from, m.to, { "data-find-match": i === current ? "current" : "" })),
  );
}

const empty: FindState = { query: "", caseSensitive: false, matches: [], current: -1, decorations: DecorationSet.empty };

function next(tr: Transaction, prev: FindState): FindState {
  const meta = tr.getMeta(findKey) as FindMeta | undefined;
  if (!meta && !tr.docChanged) return prev;
  if (meta?.kind === "go") {
    if (meta.index === prev.current || prev.matches.length === 0) return prev;
    return { ...prev, current: meta.index, decorations: decorate(tr.doc, prev.matches, meta.index) };
  }
  const query = meta?.kind === "query" ? meta.query : prev.query;
  const caseSensitive = meta?.kind === "query" ? meta.caseSensitive : prev.caseSensitive;
  if (!query) return { ...empty, caseSensitive };
  const matches = findMatches(tr.doc, query, caseSensitive);
  let current: number;
  if (meta?.kind === "query") current = firstFrom(matches, tr.selection.from);
  else if (meta?.kind === "after") current = firstFrom(matches, meta.pos);
  else {
    const was = prev.matches[prev.current];
    current = firstFrom(matches, was ? tr.mapping.map(was.from, -1) : 0);
  }
  return { query, caseSensitive, matches, current, decorations: decorate(tr.doc, matches, current) };
}

/** Highlights what the find bar looks for; it changes nothing in the document. */
export function findPlugin(): Plugin<FindState> {
  return new Plugin<FindState>({
    key: findKey,
    state: { init: () => empty, apply: next },
    props: { decorations: (state) => findKey.getState(state)?.decorations },
  });
}

/** Looks for the query from the caret on, or stops looking when it is empty. */
export function setFindQuery(state: EditorState, query: string, caseSensitive: boolean): Transaction {
  return state.tr.setMeta(findKey, { kind: "query", query, caseSensitive } satisfies FindMeta);
}

/** Moves to the next match, or the previous one, wrapping around, and selects it. */
export function stepMatch(state: EditorState, direction: 1 | -1): Transaction | null {
  const find = findKey.getState(state);
  if (!find || find.matches.length === 0) return null;
  const count = find.matches.length;
  const index = find.current < 0 ? 0 : (find.current + direction + count) % count;
  return selectMatch(state.tr, find.matches[index]!).setMeta(findKey, { kind: "go", index } satisfies FindMeta);
}

/** Selects the match the bar is on, so leaving the bar leaves the caret there. */
export function selectCurrent(state: EditorState): Transaction | null {
  const find = findKey.getState(state);
  const match = find?.matches[find.current];
  return match ? selectMatch(state.tr, match) : null;
}

function selectMatch(tr: Transaction, match: Match): Transaction {
  return tr.setSelection(TextSelection.create(tr.doc, match.from, match.to));
}

/**
 * Ends the shared draft's undo step, which otherwise takes in every change
 * made within half a second, so a replace undoes on its own as it does alone.
 */
export function endUndoStep(state: EditorState) {
  (yUndoPluginKey.getState(state) as { undoManager?: { stopCapturing: () => void } } | undefined)?.undoManager?.stopCapturing();
}

/** Replaces the match the bar is on and moves to the one after it; the replaced text keeps its marks. */
export function replaceCurrent(state: EditorState, replacement: string): Transaction | null {
  const find = findKey.getState(state);
  const match = find?.matches[find.current];
  if (!match) return null;
  const tr = state.tr;
  closeHistory(tr);
  tr.insertText(replacement, match.from, match.to);
  const after = match.from + replacement.length;
  tr.setSelection(TextSelection.create(tr.doc, after));
  return tr.setMeta(findKey, { kind: "after", pos: after } satisfies FindMeta);
}

/** Replaces every match in one step, which one undo takes back. */
export function replaceAll(state: EditorState, replacement: string): { tr: Transaction; count: number } | null {
  const find = findKey.getState(state);
  if (!find || find.matches.length === 0) return null;
  const tr = state.tr;
  closeHistory(tr);
  // From the last match back, so each one is still where it was found.
  for (let i = find.matches.length - 1; i >= 0; i--) {
    const match = find.matches[i]!;
    tr.insertText(replacement, match.from, match.to);
  }
  return { tr, count: find.matches.length };
}

/** The selected words, when they lie in one text block and could be looked for. */
export function selectedQuery(state: EditorState): string {
  const { from, to, empty: none, $from } = state.selection;
  if (none || !$from.parent.isTextblock || !$from.sameParent(state.selection.$to)) return "";
  const text = state.doc.textBetween(from, to, "", NOT_TEXT);
  return text.includes(NOT_TEXT) ? "" : text;
}

export interface FindReplaceOptions {
  /** Opens the find bar with the selected words; without it Ctrl or Cmd+F is the browser's. */
  open?: (seed: string) => void;
}

/** Ctrl or Cmd+F in the editor opens the find bar; the plugin draws what it finds. */
export const FindReplace = Extension.create<FindReplaceOptions>({
  name: "findReplace",
  addOptions() {
    return { open: undefined };
  },
  addProseMirrorPlugins() {
    return [findPlugin()];
  },
  addKeyboardShortcuts() {
    return {
      "Mod-f": () => {
        if (!this.options.open) return false;
        this.options.open(selectedQuery(this.editor.state));
        return true;
      },
    };
  },
});
