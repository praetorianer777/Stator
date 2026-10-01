import { afterEach, describe, expect, it } from "vitest";
import { Editor, type JSONContent } from "@tiptap/core";
import { Slice } from "@tiptap/pm/model";
import type { EditorView } from "@tiptap/pm/view";
import { editorExtensions, type ExtensionOptions } from "./extensions";
import type { DocNode } from "./schema";
import { SLASH_ITEMS } from "./slashItems";

let editor: Editor | undefined;
afterEach(() => editor?.destroy());

// The editor announces itself created on the next tick, which is when it
// anchors the headings it was given.
async function make(content: JSONContent = { type: "doc", content: [{ type: "paragraph" }] }, options: ExtensionOptions = {}) {
  editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions(options), content });
  await new Promise((resolve) => setTimeout(resolve));
  return editor;
}

function find(doc: DocNode, type: string): DocNode[] {
  const out: DocNode[] = doc.type === type ? [doc] : [];
  for (const child of doc.content ?? []) out.push(...find(child, type));
  return out;
}

const heading = (text: string, id?: string, level = 2): JSONContent => ({
  type: "heading",
  attrs: { level, id: id ?? null },
  content: [{ type: "text", text }],
});

describe("heading anchors", () => {
  it("gives every heading a slug and numbers the repeats", async () => {
    const e = await make({ type: "doc", content: [heading("Getting started"), heading("Setup"), heading("Setup"), heading("!!!")] });
    expect(find(e.getJSON() as DocNode, "heading").map((h) => h.attrs?.id)).toEqual(["getting-started", "setup", "setup-2", "section"]);
  });

  it("keeps an existing anchor when a same-named heading is added above it", async () => {
    const e = await make({ type: "doc", content: [{ type: "paragraph" }, heading("Plan", "plan")] });
    e.chain().setTextSelection(1).setHeading({ level: 2 }).insertContent("Plan").run();
    expect(find(e.getJSON() as DocNode, "heading").map((h) => h.attrs?.id)).toEqual(["plan-2", "plan"]);
  });

  it("follows the text of the heading as it is typed", async () => {
    const e = await make();
    e.chain().setHeading({ level: 1 }).insertContent("Rel").insertContent("ease notes").run();
    expect(find(e.getJSON() as DocNode, "heading")[0]?.attrs?.id).toBe("release-notes");
  });

  it("replaces an anchor that does not fit the pattern", async () => {
    const e = await make({ type: "doc", content: [heading("Plan", 'x"><img')] });
    expect(find(e.getJSON() as DocNode, "heading")[0]?.attrs?.id).toBe("plan");
  });
});

describe("the slash menu's blocks", () => {
  it("each inserts its block", async () => {
    const expected: Record<string, (doc: DocNode) => boolean> = {
      paragraph: (d) => d.content?.[0]?.type === "paragraph",
      heading1: (d) => d.content?.[0]?.type === "heading" && d.content[0].attrs?.level === 1,
      heading2: (d) => d.content?.[0]?.attrs?.level === 2,
      heading3: (d) => d.content?.[0]?.attrs?.level === 3,
      bulletList: (d) => find(d, "bulletList").length === 1,
      orderedList: (d) => find(d, "orderedList").length === 1,
      taskList: (d) => find(d, "taskItem").length === 1,
      quote: (d) => find(d, "blockquote").length === 1,
      codeBlock: (d) => find(d, "codeBlock").length === 1,
      divider: (d) => find(d, "horizontalRule").length === 1,
      table: (d) => find(d, "tableRow").length === 3 && find(d, "tableHeader").length === 3,
      panelInfo: (d) => find(d, "panel")[0]?.attrs?.kind === "info",
      panelNote: (d) => find(d, "panel")[0]?.attrs?.kind === "note",
      panelSuccess: (d) => find(d, "panel")[0]?.attrs?.kind === "success",
      panelWarning: (d) => find(d, "panel")[0]?.attrs?.kind === "warning",
      panelError: (d) => find(d, "panel")[0]?.attrs?.kind === "error",
      tableOfContents: (d) => d.content?.[0]?.type === "tableOfContents" && d.content[0].attrs?.maxLevel === 3,
      childPages: (d) => JSON.stringify(find(d, "childPages")[0]?.attrs) === JSON.stringify({ scope: "children", depth: null, sort: "tree" }),
      // The picker asks which issue; this one answers lower case, as a person might type it.
      armatureIssue: (d) => JSON.stringify(find(d, "armatureIssueBlock")[0]?.attrs) === JSON.stringify({ key: "CP-4" }),
      armatureIssueList: (d) =>
        JSON.stringify(find(d, "armatureIssueList")[0]?.attrs) === JSON.stringify({ query: "project = CP", columns: ["key", "due"], limit: 5 }),
    };
    expect(SLASH_ITEMS.map((item) => item.key).sort()).toEqual(Object.keys(expected).sort());
    for (const item of SLASH_ITEMS) {
      // The pickers answer later, as a dialog does, never inside the slash command.
      const e = await make(undefined, {
        pickIssue: () => setTimeout(() => editor?.commands.insertArmatureIssueBlock("cp-4")),
        pickIssueList: () => setTimeout(() => editor?.commands.insertArmatureIssueList({ query: "project = CP", columns: ["key", "due"], limit: 5 })),
      });
      item.run(e.chain().focus());
      await new Promise((resolve) => setTimeout(resolve));
      expect(expected[item.key]?.(e.getJSON() as DocNode), item.key).toBe(true);
      e.destroy();
    }
  });
});

describe("the Armature issue block", () => {
  it("refuses a key that is not one, and keeps nothing but the key", async () => {
    const e = await make();
    expect(e.commands.insertArmatureIssueBlock("UTF-8x")).toBe(false);
    expect(find(e.getJSON() as DocNode, "armatureIssueBlock")).toHaveLength(0);
    e.commands.insertArmatureIssueBlock("SEC-1");
    expect(find(e.getJSON() as DocNode, "armatureIssueBlock")[0]?.attrs).toEqual({ key: "SEC-1" });
    expect(e.getText()).toContain("SEC-1");
  });

  it("is read back from what the editor copies, so cut and paste keep it", async () => {
    const e = await make({ type: "doc", content: [{ type: "armatureIssueBlock", attrs: { key: "CP-4" } }] });
    const html = e.getHTML();
    e.destroy();
    expect(html).toContain('data-armature-issue-block="CP-4"');
    const again = await make();
    again.commands.setContent(html);
    expect(find(again.getJSON() as DocNode, "armatureIssueBlock")[0]?.attrs).toEqual({ key: "CP-4" });
    again.commands.setContent('<div data-armature-issue-block="not a key">x</div>');
    expect(find(again.getJSON() as DocNode, "armatureIssueBlock")).toHaveLength(0);
  });
});

describe("tables", () => {
  async function table() {
    const e = await make();
    e.chain().insertTable({ rows: 2, cols: 2, withHeaderRow: true }).run();
    return e;
  }

  it("adds and removes rows and columns", async () => {
    const e = await table();
    e.commands.addRowAfter();
    e.commands.addColumnAfter();
    expect(find(e.getJSON() as DocNode, "tableRow")).toHaveLength(3);
    expect(find(e.getJSON() as DocNode, "tableRow")[0]?.content).toHaveLength(3);
    e.commands.deleteRow();
    e.commands.deleteColumn();
    expect(find(e.getJSON() as DocNode, "tableRow")).toHaveLength(2);
    expect(find(e.getJSON() as DocNode, "tableRow")[0]?.content).toHaveLength(2);
  });

  it("toggles the header row and column", async () => {
    const e = await table();
    e.chain().toggleHeaderRow().run();
    expect(find(e.getJSON() as DocNode, "tableHeader")).toHaveLength(0);
    e.chain().toggleHeaderColumn().run();
    expect(find(e.getJSON() as DocNode, "tableHeader")).toHaveLength(2);
  });

  it("merges and splits cells and colours one", async () => {
    const e = await table();
    // The two cells of the second row, as a pointer drag would select them.
    const cells: number[] = [];
    e.state.doc.descendants((node, pos) => {
      if (node.type.name === "tableCell") cells.push(pos);
    });
    e.chain().setCellSelection({ anchorCell: cells[0]!, headCell: cells[1]! }).mergeCells().run();
    expect(find(e.getJSON() as DocNode, "tableCell")[0]?.attrs?.colspan).toBe(2);
    e.chain().splitCell().run();
    expect(find(e.getJSON() as DocNode, "tableCell")).toHaveLength(2);
    e.chain().setCellAttribute("background", "warning").run();
    expect(find(e.getJSON() as DocNode, "tableCell").some((c) => c.attrs?.background === "warning")).toBe(true);
  });
});

describe("code blocks", () => {
  it("highlight the language they are given", async () => {
    const e = await make({
      type: "doc",
      content: [{ type: "codeBlock", attrs: { language: "go" }, content: [{ type: "text", text: 'func main() { return "x" }' }] }],
    });
    expect(e.view.dom.querySelector(".hljs-keyword")?.textContent).toBe("func");
    expect(e.view.dom.querySelector(".hljs-string")?.textContent).toBe('"x"');
  });
});

describe("panels", () => {
  it("change kind and come off again", async () => {
    const e = await make();
    e.chain().insertContent("hello").setPanel("info").setPanelKind("warning").run();
    expect(find(e.getJSON() as DocNode, "panel")[0]?.attrs?.kind).toBe("warning");
    expect(e.view.dom.querySelector('[data-panel="warning"][role="note"]')).not.toBeNull();
    e.chain().unsetPanel().run();
    expect(find(e.getJSON() as DocNode, "panel")).toHaveLength(0);
    expect(find(e.getJSON() as DocNode, "text").map((n) => n.text)).toEqual(["hello"]);
  });
});

describe("markdown", () => {
  function paste(e: Editor, text: string): boolean {
    const event = { clipboardData: { types: ["text/plain"], getData: (type: string) => (type === "text/plain" ? text : "") } } as unknown as ClipboardEvent;
    return Boolean(e.view.someProp("handlePaste", (f) => f(e.view as EditorView, event, Slice.empty)));
  }

  it("is read on paste, and unsafe links lose their link", async () => {
    const e = await make();
    const handled = paste(
      e,
      "## Plan\n\n- [ ] ship\n- [x] test\n\n```go\nx := 1\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n[ok](https://example.test) [bad](javascript:alert(1)) **bold**\n\n#### deep",
    );
    expect(handled).toBe(true);
    const doc = e.getJSON() as DocNode;
    expect(find(doc, "heading").map((h) => [h.attrs?.level, h.attrs?.id])).toEqual([
      [2, "plan"],
      [3, "deep"],
    ]);
    expect(find(doc, "taskItem").map((i) => i.attrs?.checked)).toEqual([false, true]);
    expect(find(doc, "codeBlock")[0]?.attrs?.language).toBe("go");
    expect(find(doc, "table")).toHaveLength(1);
    const links = find(doc, "text").flatMap((n) => (n.marks ?? []).filter((m) => m.type === "link").map((m) => m.attrs?.href));
    expect(links).toEqual(["https://example.test"]);
    expect(find(doc, "text").some((n) => n.text === "bold" && n.marks?.some((m) => m.type === "bold"))).toBe(true);
  });

  it("leaves plain prose and pastes into code alone", async () => {
    const e = await make();
    expect(paste(e, "just words, nothing more")).toBe(false);
    e.chain().setCodeBlock().run();
    expect(paste(e, "## not a heading")).toBe(false);
  });

  it("shortcuts turn typed markers into blocks", async () => {
    const e = await make();
    // Input rules run on typed text; insertContent does not type, so the
    // editor's own text input handler is driven the way a keystroke would.
    const type = (text: string) => {
      for (const ch of text) {
        const { from, to } = e.state.selection;
        const handled = e.view.someProp("handleTextInput", (f) => f(e.view as EditorView, from, to, ch, () => e.state.tr.insertText(ch, from, to)));
        if (!handled) e.view.dispatch(e.state.tr.insertText(ch, from, to));
      }
    };
    type("## Title");
    expect(find(e.getJSON() as DocNode, "heading")[0]?.attrs).toMatchObject({ level: 2, id: "title" });
    e.commands.setParagraph();
    e.commands.clearContent();
    type("- item");
    expect(find(e.getJSON() as DocNode, "bulletList")).toHaveLength(1);
    e.commands.clearContent();
    type("[ ] todo");
    expect(find(e.getJSON() as DocNode, "taskItem")).toHaveLength(1);
    e.commands.clearContent();
    type("**strong** ");
    expect(find(e.getJSON() as DocNode, "text")[0]?.marks?.[0]?.type).toBe("bold");
  });
});
