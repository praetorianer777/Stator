import { afterEach, describe, expect, it } from "vitest";
import { Editor, type JSONContent } from "@tiptap/core";
import { Slice } from "@tiptap/pm/model";
import type { EditorView } from "@tiptap/pm/view";
import { editorExtensions } from "./extensions";
import { hintAround } from "./hint";
import type { DocNode } from "./schema";

let editor: Editor | undefined;
afterEach(() => editor?.destroy());

const hint = (text: string): JSONContent => ({ type: "text", text, marks: [{ type: "hint" }] });

// "Owner: " in bold, then a hint; a paragraph of hint alone; plain words.
function make() {
  editor = new Editor({
    element: document.createElement("div"),
    extensions: editorExtensions(),
    content: {
      type: "doc",
      content: [
        { type: "paragraph", content: [{ type: "text", text: "Owner: ", marks: [{ type: "bold" }] }, hint("Name the owner")] },
        { type: "paragraph", content: [hint("Describe the goal")] },
        { type: "paragraph", content: [{ type: "text", text: "Plain words" }] },
      ],
    },
  });
  return editor;
}

// Where each paragraph's text starts: 1, then after each paragraph's size.
const OWNER_HINT = 1 + "Owner: ".length;
const OWNER_END = OWNER_HINT + "Name the owner".length;
const GOAL = OWNER_END + 2;
const PLAIN = GOAL + "Describe the goal".length + 2;

function type(e: Editor, text: string) {
  const { from, to } = e.state.selection;
  const handled = e.view.someProp("handleTextInput", (f) => f(e.view as EditorView, from, to, text, () => e.state.tr.insertText(text, from, to)));
  if (!handled) e.view.dispatch(e.state.tr.insertText(text, from, to));
}

function posOf(e: Editor, text: string): number {
  let at = -1;
  e.state.doc.descendants((node, pos) => {
    if (at < 0 && node.isText && node.text?.includes(text)) at = pos + node.text.indexOf(text);
  });
  return at;
}

function press(e: Editor, key: string) {
  const event = new KeyboardEvent("keydown", { key, bubbles: true });
  return Boolean(e.view.someProp("handleKeyDown", (f) => f(e.view as EditorView, event)));
}

const texts = (e: Editor) =>
  ((e.getJSON() as DocNode).content ?? []).map((p) =>
    (p.content ?? []).map((n) => `${n.text}${n.marks?.some((m) => m.type === "hint") ? "[hint]" : ""}`).join(""),
  );

describe("hints", () => {
  it("finds the run of hint text around a position, and only there", () => {
    const e = make();
    expect(hintAround(e.state, OWNER_HINT + 3)).toEqual({ from: OWNER_HINT, to: OWNER_END });
    expect(hintAround(e.state, OWNER_END)).toEqual({ from: OWNER_HINT, to: OWNER_END });
    expect(hintAround(e.state, 2)).toBeNull();
    expect(hintAround(e.state, PLAIN + 2)).toBeNull();
  });

  it("go as a whole on the first keystroke, wherever in them it lands", () => {
    const e = make();
    e.commands.setTextSelection(OWNER_HINT + 4);
    type(e, "A");
    type(e, "da");
    e.commands.setTextSelection(posOf(e, "the goal") + 2);
    type(e, "Ship it");
    expect(texts(e)).toEqual(["Owner: Ada", "Ship it", "Plain words"]);
    const owner = e.getJSON().content?.[0]?.content?.[1];
    // What is typed after a bold label is not bold: it takes the hint's styles.
    expect(owner).toEqual({ type: "text", text: "Ada" });
  });

  it("go where the keystroke lands, even before the selection has followed the click", () => {
    const e = make();
    e.commands.setTextSelection(PLAIN + 2);
    const at = OWNER_HINT + 3;
    e.view.someProp("handleTextInput", (f) => f(e.view as EditorView, at, at, "W", () => e.state.tr.insertText("W", at, at)));
    expect(texts(e)).toEqual(["Owner: W", "Describe the goal[hint]", "Plain words"]);
  });

  it("are replaced when typed over from their edge, and a selection over them goes too", () => {
    const e = make();
    e.commands.setTextSelection(OWNER_END);
    type(e, "Bo");
    expect(texts(e)[0]).toBe("Owner: Bo");
    const f = make();
    f.commands.setTextSelection({ from: 3, to: OWNER_HINT + 2 });
    type(f, "x");
    expect(texts(f)[0]).toBe("Owx");
  });

  it("go whole with Backspace or Delete from inside, and let the key through at their edge", () => {
    const e = make();
    e.commands.setTextSelection(OWNER_HINT + 5);
    expect(press(e, "Backspace")).toBe(true);
    expect(texts(e)[0]).toBe("Owner: ");
    expect(e.state.selection.from).toBe(OWNER_HINT);
    const f = make();
    f.commands.setTextSelection(GOAL);
    expect(press(f, "Delete")).toBe(true);
    expect(texts(f)[1]).toBe("");
    const g = make();
    g.commands.setTextSelection(GOAL);
    press(g, "Backspace");
    expect(texts(g).join("|")).toContain("Describe the goal[hint]");
  });

  it("make way for a paste", () => {
    const e = make();
    e.commands.setTextSelection(GOAL + 2);
    const event = new Event("paste") as ClipboardEvent;
    e.view.someProp("handlePaste", (f) => f(e.view as EditorView, event, Slice.empty));
    expect(texts(e)[1]).toBe("");
    expect(e.state.selection.from).toBe(GOAL);
  });

  it("leave plain text to type as usual", () => {
    const e = make();
    e.commands.setTextSelection(PLAIN + 5);
    type(e, "!");
    expect(texts(e)).toEqual(["Owner: Name the owner[hint]", "Describe the goal[hint]", "Plain! words"]);
  });

  it("are drawn as hints", () => {
    const e = make();
    expect([...e.view.dom.querySelectorAll("[data-hint]")].map((el) => el.textContent)).toEqual(["Name the owner", "Describe the goal"]);
  });
});
