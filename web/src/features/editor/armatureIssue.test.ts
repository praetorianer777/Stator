import { afterEach, describe, expect, it } from "vitest";
import { Editor, type JSONContent } from "@tiptap/core";
import { Slice } from "@tiptap/pm/model";
import type { EditorView } from "@tiptap/pm/view";
import { problems } from "@/test/allowlist";
import type { IssueSource } from "./armatureIssue";
import { editorExtensions } from "./extensions";
import type { DocNode } from "./schema";

const BASE = "https://armature.example.com";

let editor: Editor | undefined;
afterEach(() => editor?.destroy());

function make(source?: IssueSource, content: JSONContent = { type: "doc", content: [{ type: "paragraph" }] }) {
  editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions({ armature: source }), content });
  return editor;
}

const author: IssueSource = { baseUrl: () => BASE, knowsProject: (key) => key === "CP" };
const noToken: IssueSource = { baseUrl: () => BASE, knowsProject: () => false };

// Input rules run on typed text only, so each character goes through the
// editor's own text input handler the way a keystroke does.
function type(e: Editor, text: string) {
  for (const ch of text) {
    const { from, to } = e.state.selection;
    const handled = e.view.someProp("handleTextInput", (f) => f(e.view as EditorView, from, to, ch, () => e.state.tr.insertText(ch, from, to)));
    if (!handled) e.view.dispatch(e.state.tr.insertText(ch, from, to));
  }
}

function paste(e: Editor, text: string): boolean {
  const event = { clipboardData: { types: ["text/plain"], getData: (kind: string) => (kind === "text/plain" ? text : "") } } as unknown as ClipboardEvent;
  return Boolean(e.view.someProp("handlePaste", (f) => f(e.view as EditorView, event, Slice.empty)));
}

const inline = (e: Editor) =>
  ((e.getJSON() as DocNode).content?.[0]?.content ?? []).map((n) => (n.type === "text" ? n.text : `[${String(n.attrs?.key)}]`)).join("");

describe("Armature issue chips in the editor", () => {
  it("turn a typed key into a chip when the author sees its project, and keep what was typed after it", () => {
    const e = make(author);
    type(e, "Fixed in CP-12, see UTF-8 and SEC-1 ");
    expect(inline(e)).toBe("Fixed in [CP-12], see UTF-8 and SEC-1 ");
    expect(problems(e.getJSON() as DocNode)).toEqual([]);
    expect(e.getText()).toContain("CP-12");
  });

  it("leave typed keys alone for an author without a token, and in code", () => {
    const e = make(noToken);
    type(e, "CP-12 ");
    expect(inline(e)).toBe("CP-12 ");

    const f = make(author, { type: "doc", content: [{ type: "codeBlock", attrs: { language: null } }] });
    type(f, "CP-12 ");
    expect(JSON.stringify(f.getJSON())).not.toContain("armatureIssue");

    const g = make(author);
    g.commands.toggleCode();
    type(g, "CP-12 ");
    expect(JSON.stringify(g.getJSON())).not.toContain("armatureIssue");
  });

  it("go back to the text on undo", () => {
    const e = make(author);
    type(e, "CP-7 ");
    expect(inline(e)).toBe("[CP-7] ");
    e.commands.undoInputRule();
    expect(inline(e)).toBe("CP-7 ");
  });

  it("are made from a pasted issue address of the connected Armature, also without a token", () => {
    const e = make(noToken);
    type(e, "See ");
    expect(paste(e, `${BASE}/issues/sec-2/?focus=1`)).toBe(true);
    expect(inline(e)).toBe("See [SEC-2]");

    const f = make(author);
    expect(paste(f, "https://elsewhere.example.com/issues/CP-1")).toBe(false);
    expect(paste(f, `${BASE}/projects/CP`)).toBe(false);

    const g = make({ baseUrl: () => null, knowsProject: () => true });
    expect(paste(g, `${BASE}/issues/CP-1`)).toBe(false);

    const h = make(author, { type: "doc", content: [{ type: "codeBlock", attrs: { language: null } }] });
    expect(paste(h, `${BASE}/issues/CP-1`)).toBe(false);
  });

  it("store the key and nothing else, and read back from their HTML", () => {
    const e = make(author, { type: "doc", content: [{ type: "paragraph", content: [{ type: "armatureIssue", attrs: { key: "CP-3" } }] }] });
    expect(e.getJSON().content?.[0]?.content?.[0]).toEqual({ type: "armatureIssue", attrs: { key: "CP-3" } });
    const html = e.getHTML();
    expect(html).toContain('data-armature-issue="CP-3"');
    const f = make(author);
    f.commands.setContent(html);
    expect(f.getJSON().content?.[0]?.content?.[0]).toEqual({ type: "armatureIssue", attrs: { key: "CP-3" } });
    f.commands.setContent('<p><span data-armature-issue="not a key">x</span></p>');
    expect(JSON.stringify(f.getJSON())).not.toContain("armatureIssue");
  });

  it("are not in a comment's editor", () => {
    editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions({ variant: "comment" }) });
    expect(editor.schema.nodes.armatureIssue).toBeUndefined();
  });
});
