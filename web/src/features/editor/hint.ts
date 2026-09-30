import { Mark } from "@tiptap/core";
import type { MarkType } from "@tiptap/pm/model";
import { Plugin, PluginKey, TextSelection, type EditorState } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";

export const HINT_MARK = "hint";

interface Range {
  from: number;
  to: number;
}

/** The run of hint text touching a position, within its textblock, or null. */
export function hintAround(state: EditorState, pos: number): Range | null {
  const type = state.schema.marks[HINT_MARK];
  const $pos = state.doc.resolve(pos);
  if (!type || !$pos.parent.isTextblock) return null;
  const start = $pos.start();
  let run: Range | null = null;
  let found: Range | null = null;
  $pos.parent.forEach((child, offset) => {
    const from = start + offset;
    const to = from + child.nodeSize;
    if (child.isText && type.isInSet(child.marks)) {
      run = run && run.to === from ? { from: run.from, to } : { from, to };
      if (run.from <= pos && pos <= run.to) found = run;
    } else {
      run = null;
    }
  });
  return found;
}

/** A range widened over every hint it touches, or null when it touches none. */
function hintSpan(state: EditorState, from: number, to: number): Range | null {
  const before = hintAround(state, from);
  const after = hintAround(state, to);
  if (!before && !after) return null;
  return { from: Math.min(from, before?.from ?? from), to: Math.max(to, after?.to ?? to) };
}

// The range comes from the input rather than the selection: a browser reports
// the first keystroke after a click before the selection has followed it.
function typeOver(view: EditorView, type: MarkType, from: number, to: number, text: string): boolean {
  const { state } = view;
  const span = hintSpan(state, from, to);
  if (!span) return false;
  // What is typed takes the hint's other styles, not the label's before it.
  const first = state.doc.resolve(span.from).nodeAfter;
  const marks = (first && type.isInSet(first.marks) ? first.marks : state.doc.resolve(from).marks()).filter((m) => m.type !== type);
  const tr = state.tr.replaceWith(span.from, span.to, state.schema.text(text, marks));
  tr.setSelection(TextSelection.create(tr.doc, span.from + text.length));
  view.dispatch(tr.scrollIntoView());
  return true;
}

function removeHint(view: EditorView, forward: boolean): boolean {
  const { state } = view;
  if (!state.selection.empty) return false;
  const pos = state.selection.from;
  const hint = hintAround(state, pos);
  // At the hint's edge the key belongs to what lies beyond it.
  if (!hint || (forward ? pos === hint.to : pos === hint.from)) return false;
  const tr = state.tr.delete(hint.from, hint.to);
  view.dispatch(tr.setSelection(TextSelection.create(tr.doc, hint.from)));
  return true;
}

/**
 * A template's placeholder text: drawn muted, and gone as a whole the moment
 * somebody types, pastes or deletes into it. The server strips what is left
 * of it from every published version.
 */
export const Hint = Mark.create({
  name: HINT_MARK,
  // Ahead of the lists' and the core's keys and of the markdown paste, so a
  // hint is always taken out before they act.
  priority: 1000,
  inclusive: false,
  parseHTML() {
    return [{ tag: "span[data-hint]" }];
  },
  renderHTML() {
    return ["span", { "data-hint": "" }, 0];
  },
  addKeyboardShortcuts() {
    return {
      Backspace: ({ editor }) => removeHint(editor.view, false),
      Delete: ({ editor }) => removeHint(editor.view, true),
    };
  },
  addProseMirrorPlugins() {
    const type = this.type;
    return [
      new Plugin({
        key: new PluginKey("hint"),
        props: {
          handleTextInput: (view, from, to, text) => typeOver(view, type, from, to, text),
          // The hint goes first, then whatever handles the paste pastes into its place.
          handlePaste: (view) => {
            const span = hintSpan(view.state, view.state.selection.from, view.state.selection.to);
            if (span) {
              const tr = view.state.tr.delete(span.from, span.to);
              view.dispatch(tr.setSelection(TextSelection.create(tr.doc, span.from)));
            }
            return false;
          },
        },
      }),
    ];
  },
});
