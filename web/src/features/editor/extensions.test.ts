import { afterEach, describe, expect, it } from "vitest";
import { Editor, type JSONContent } from "@tiptap/core";
import { Slice } from "@tiptap/pm/model";
import { NodeSelection } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { DIAGRAM_DEFAULT_SOURCE } from "@/config";
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
      decision: (d) => d.content?.[0]?.type === "decision" && d.content[0].attrs?.state === "undecided",
      expand: (d) => d.content?.[0]?.type === "expand" && d.content[0].attrs?.title === "" && d.content[0].content?.[0]?.type === "paragraph",
      columns2: (d) => JSON.stringify(find(d, "column").map((c) => c.attrs?.width)) === "[50,50]",
      columns3: (d) => JSON.stringify(find(d, "column").map((c) => c.attrs?.width)) === "[33,34,33]",
      tableOfContents: (d) => d.content?.[0]?.type === "tableOfContents" && d.content[0].attrs?.maxLevel === 3,
      childPages: (d) => JSON.stringify(find(d, "childPages")[0]?.attrs) === JSON.stringify({ scope: "children", depth: null, sort: "tree" }),
      // The picker asks which issue; this one answers lower case, as a person might type it.
      armatureIssue: (d) => JSON.stringify(find(d, "armatureIssueBlock")[0]?.attrs) === JSON.stringify({ key: "CP-4" }),
      armatureIssueList: (d) =>
        JSON.stringify(find(d, "armatureIssueList")[0]?.attrs) === JSON.stringify({ query: "project = CP", columns: ["key", "due"], limit: 5 }),
      // The dialog that opens on a new status or date is answered below.
      status: (d) => JSON.stringify(find(d, "status")[0]?.attrs) === JSON.stringify({ label: "Blocked", color: "danger" }),
      date: (d) => JSON.stringify(find(d, "date")[0]?.attrs) === JSON.stringify({ date: "2026-11-02" }),
      emoji: (d) => find(d, "text")[0]?.text === ":",
      diagram: (d) => find(d, "diagram")[0]?.attrs?.source === DIAGRAM_DEFAULT_SOURCE,
      armatureChart: (d) =>
        JSON.stringify(find(d, "armatureChart")[0]?.attrs) ===
        JSON.stringify({ project: "CP", query: "project = CP", chart: "pie", groupBy: "type", days: 30 }),
      armatureRoadmap: (d) =>
        JSON.stringify(find(d, "armatureRoadmap")[0]?.attrs) === JSON.stringify({ project: "CP", query: "project = CP", groupBy: "team" }),
      properties: (d) => JSON.stringify(find(d, "propertyRow").map((r) => r.attrs?.key)) === JSON.stringify(["Owner", "Status"]),
      propertiesReport: (d) => JSON.stringify(find(d, "propertiesReport")[0]?.attrs) === JSON.stringify({ labels: ["adr"], space: null, columns: ["Owner"] }),
      labelledPages: (d) =>
        JSON.stringify(find(d, "labelledPages")[0]?.attrs) === JSON.stringify({ labels: ["adr"], match: "any", space: null, sort: "title", limit: 5 }),
      recentlyUpdated: (d) => JSON.stringify(find(d, "recentlyUpdated")[0]?.attrs) === JSON.stringify({ space: null, limit: 10 }),
      blogPosts: (d) => JSON.stringify(find(d, "blogPosts")[0]?.attrs) === JSON.stringify({ space: "NEWS", limit: 5 }),
      tableChart: (d) => find(d, "tableChart")[0]?.attrs?.chart === "bar" && find(d, "tableRow").length === 4,
      attachmentList: (d) => JSON.stringify(find(d, "attachmentList")[0]) === JSON.stringify({ type: "attachmentList" }),
      gallery: (d) =>
        JSON.stringify(find(d, "gallery")[0]) ===
        JSON.stringify({
          type: "gallery",
          attrs: { columns: 2 },
          content: [{ type: "galleryImage", attrs: { attachmentId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a91", caption: "Dock" } }],
        }),
      taskReport: (d) =>
        JSON.stringify(find(d, "taskReport")[0]?.attrs) === JSON.stringify({ space: "DOCS", assignee: "me", due: "week", state: "open", limit: 20 }),
      calendar: (d) => JSON.stringify(find(d, "calendar")[0]?.attrs) === JSON.stringify({ calendarId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a90", project: "CP" }),
      templateButton: (d) =>
        JSON.stringify(find(d, "templateButton")[0]?.attrs) ===
        JSON.stringify({ template: "meeting-notes", space: "DOCS", parent: null, label: "New notes", title: "Notes {date}" }),
      contributors: (d) => JSON.stringify(find(d, "contributors")[0]?.attrs) === JSON.stringify({ scope: "page", limit: 10 }),
      include: (d) => JSON.stringify(find(d, "include")[0]?.attrs) === JSON.stringify({ pageId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a80", excerptId: null }),
      excerpt: (d) => {
        const e = find(d, "excerpt")[0];
        return e?.attrs?.name === "Excerpt 1" && /^[0-9a-f-]{36}$/.test(String(e.attrs.id)) && e.content?.[0]?.type !== undefined;
      },
      // The dialog asks for the address; this one answers as a person would.
      linkCard: (d) => JSON.stringify(find(d, "linkCard")[0]?.attrs) === JSON.stringify({ url: "https://example.test/post", view: "card" }),
      mathBlock: (d) => find(d, "mathBlock")[0]?.attrs?.latex === "\\sqrt{2}",
      mathInline: (d) => find(d, "paragraph")[0]?.content?.[0]?.type === "mathInline" && find(d, "mathInline")[0]?.attrs?.latex === "\\sqrt{2}",
    };
    expect(SLASH_ITEMS.map((item) => item.key).sort()).toEqual(Object.keys(expected).sort());
    for (const item of SLASH_ITEMS) {
      // The pickers answer later, as a dialog does, never inside the slash command.
      const e = await make(undefined, {
        pickIssue: () => setTimeout(() => editor?.commands.insertArmatureIssueBlock("cp-4")),
        pickChart: () =>
          setTimeout(() => editor?.commands.insertArmatureChart({ project: "CP", query: "project = CP", chart: "pie", groupBy: "type", days: 30 })),
        pickRoadmap: () => setTimeout(() => editor?.commands.insertArmatureRoadmap({ project: "CP", query: "project = CP", groupBy: "team" })),
        pickPropertiesReport: () => setTimeout(() => editor?.commands.insertPropertiesReport({ labels: ["adr"], space: null, columns: ["Owner"] })),
        pickLabelledPages: () =>
          setTimeout(() => editor?.commands.insertLabelledPages({ labels: ["adr"], match: "any", space: null, sort: "title", limit: 5 })),
        pickBlogPosts: () => setTimeout(() => editor?.commands.insertBlogPosts({ space: "NEWS", limit: 5 })),
        pickTaskReport: () => setTimeout(() => editor?.commands.insertTaskReport({ space: "DOCS", assignee: "me", due: "week", state: "open", limit: 20 })),
        pickCalendar: () => setTimeout(() => editor?.commands.insertCalendar({ calendarId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a90", project: "CP" })),
        pickGallery: () =>
          setTimeout(() =>
            editor?.commands.insertGallery({ columns: 2, pictures: [{ attachmentId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a91", caption: "Dock" }] }),
          ),
        pickTemplateButton: () =>
          setTimeout(() =>
            editor?.commands.insertTemplateButton({ template: "meeting-notes", space: "DOCS", parent: null, label: "New notes", title: "Notes {date}" }),
          ),
        pickInclude: () => setTimeout(() => editor?.commands.insertInclude({ pageId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a80", excerptId: null })),
        pickLinkCard: () => setTimeout(() => editor?.commands.insertLinkCard("https://example.test/post")),
        pickIssueList: () => setTimeout(() => editor?.commands.insertArmatureIssueList({ query: "project = CP", columns: ["key", "due"], limit: 5 })),
        editInlineValue: (target) =>
          setTimeout(() =>
            editor?.commands.command(({ tr }) => {
              const attrs = {
                status: { label: "Blocked", color: "danger" },
                date: { date: "2026-11-02" },
                mathInline: { latex: "\\sqrt{2}" },
                mathBlock: { latex: "\\sqrt{2}" },
              };
              tr.setNodeMarkup(target.pos, undefined, attrs[target.kind]);
              return true;
            }),
          ),
      });
      item.run(e.chain().focus());
      await new Promise((resolve) => setTimeout(resolve));
      expect(expected[item.key]?.(e.getJSON() as DocNode), item.key).toBe(true);
      e.destroy();
    }
  });
});

describe("galleries", () => {
  it("are read back from what the editor copies, their pictures in order with their captions", async () => {
    const doc: DocNode = {
      type: "doc",
      content: [
        {
          type: "gallery",
          attrs: { columns: 4 },
          content: [
            { type: "galleryImage", attrs: { attachmentId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a91", caption: "Dock <b>" } },
            { type: "galleryImage", attrs: { attachmentId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a92", caption: null } },
          ],
        },
        { type: "image", attrs: { attachmentId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a93", alt: null, width: null } },
      ],
    };
    const e = await make(doc);
    const html = e.getHTML();
    e.destroy();
    const back = await make();
    back.commands.setContent(html);
    expect(back.getJSON().content?.slice(0, 2)).toEqual(doc.content);
  });
});

describe("diagrams", () => {
  it("are read back from what the editor copies, their lines and spaces kept", async () => {
    const doc: DocNode = { type: "doc", content: [{ type: "diagram", attrs: { source: 'flowchart LR\n  a["<b>Draft</b>"] --> b\n\n  b --> c' } }] };
    const e = await make(doc);
    const html = e.getHTML();
    expect(html).not.toContain("<b>");
    e.destroy();
    const back = await make();
    back.commands.setContent(html);
    expect(back.getJSON().content?.slice(0, 1)).toEqual(doc.content);
  });
});

describe("formulas", () => {
  it("are read back from what the editor copies, inline and on their own line", async () => {
    const doc: DocNode = {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Area " },
            { type: "mathInline", attrs: { latex: "\\pi r^2" } },
          ],
        },
        { type: "mathBlock", attrs: { latex: "a < b & c" } },
      ],
    };
    const e = await make(doc);
    const html = e.getHTML();
    expect(e.getText()).toContain("$\\pi r^2$");
    e.destroy();
    const back = await make();
    back.commands.setContent(html);
    // The editor keeps a line after a closing block to type on.
    expect(back.getJSON().content?.slice(0, 2)).toEqual(doc.content);
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

describe("decision items", () => {
  it("turn the line into a decision, undecided until somebody says otherwise", async () => {
    const e = await make();
    e.chain().insertContent("Ship weekly").setDecision().run();
    expect(find(e.getJSON() as DocNode, "decision")[0]?.attrs).toEqual({ state: "undecided" });
    e.commands.updateAttributes("decision", { state: "decided" });
    expect(find(e.getJSON() as DocNode, "decision")[0]?.attrs).toEqual({ state: "decided" });
    expect(find(e.getJSON() as DocNode, "text").map((n) => n.text)).toEqual(["Ship weekly"]);
  });

  it("are read back from the reader's view, its label left out", async () => {
    const e = await make();
    e.commands.setContent('<div data-decision="decided"><span class="doc-decision-badge">Decided</span><p data-decision-text>Use Postgres</p></div>');
    const [item] = find(e.getJSON() as DocNode, "decision");
    expect(item?.attrs).toEqual({ state: "decided" });
    expect(find(item!, "text").map((n) => n.text)).toEqual(["Use Postgres"]);
  });
});

describe("expand blocks", () => {
  it("wrap the blocks under the caret, store their title and come off again", async () => {
    const e = await make();
    e.chain().insertContent("hidden detail").setExpand().run();
    expect(e.storage.expand.focusTitle).toBe(true);
    const [block] = find(e.getJSON() as DocNode, "expand");
    expect(block?.attrs).toEqual({ title: "" });
    expect(find(block!, "text").map((n) => n.text)).toEqual(["hidden detail"]);
    e.chain().unsetExpand().run();
    expect(find(e.getJSON() as DocNode, "expand")).toHaveLength(0);
    expect(find(e.getJSON() as DocNode, "text").map((n) => n.text)).toEqual(["hidden detail"]);
  });

  it("is read back from what the editor copies, title and blocks alike", async () => {
    const e = await make({
      type: "doc",
      content: [{ type: "expand", attrs: { title: "Steps <b>" }, content: [{ type: "paragraph", content: [{ type: "text", text: "inside" }] }] }],
    });
    const html = e.getHTML();
    e.destroy();
    expect(html).toContain("data-title=");
    const again = await make();
    again.commands.setContent(html);
    const [block] = find(again.getJSON() as DocNode, "expand");
    expect(block?.attrs).toEqual({ title: "Steps <b>" });
    expect(find(block!, "text").map((n) => n.text)).toEqual(["inside"]);
  });

  it("is read back from the reader's view, its toggle left out", async () => {
    const e = await make();
    e.commands.setContent(
      '<div data-expand data-title="Read view" data-expanded="false"><button data-expand-toggle>Read view</button><div data-expand-body><p>body</p></div></div>',
    );
    const [block] = find(e.getJSON() as DocNode, "expand");
    expect(block?.attrs).toEqual({ title: "Read view" });
    expect(find(block!, "text").map((n) => n.text)).toEqual(["body"]);
  });
});

describe("columns", () => {
  const texts = (doc: DocNode) => find(doc, "text").map((n) => n.text);
  const widths = (doc: DocNode) => find(doc, "column").map((c) => c.attrs?.width);
  const columnTexts = (doc: DocNode) => find(doc, "column").map((c) => find(c, "text").map((n) => n.text));
  // The end of a column's one paragraph: two tokens in from where the column closes.
  const endOfColumn = (e: Editor, index: number) => {
    const at: number[] = [];
    e.state.doc.descendants((node, pos) => void (node.type.name === "column" && at.push(pos + node.nodeSize - 2)));
    return at[index] ?? -1;
  };

  it("take the blocks under the caret into the first column and put the caret there", async () => {
    const e = await make();
    e.chain().insertContent("left words").setColumns(2).insertContent(" and more").run();
    const doc = e.getJSON() as DocNode;
    expect(doc.content?.[0]?.type).toBe("columns");
    expect(widths(doc)).toEqual([50, 50]);
    expect(columnTexts(doc)).toEqual([["left words and more"], []]);
    expect(e.view.dom.querySelectorAll(".doc-columns > .doc-column")).toHaveLength(2);
    expect(e.view.dom.querySelector<HTMLElement>(".doc-column")?.style.getPropertyValue("--column-share")).toBe("50");
  });

  it("are not made inside a column", async () => {
    const e = await make();
    e.chain().insertContent("x").setColumns(3).run();
    expect(e.can().setColumns(2)).toBe(false);
    expect(find(e.getJSON() as DocNode, "columns")).toHaveLength(1);
  });

  it("change layout, adding a column or folding the last one into the one before", async () => {
    const e = await make();
    e.chain().insertContent("one").setColumns(3).run();
    e.chain().setTextSelection(endOfColumn(e, 2)).insertContent("three").run();
    expect(columnTexts(e.getJSON() as DocNode)).toEqual([["one"], [], ["three"]]);
    e.chain().setColumnLayout("twoWideLeft").run();
    expect(widths(e.getJSON() as DocNode)).toEqual([67, 33]);
    expect(columnTexts(e.getJSON() as DocNode)).toEqual([["one"], ["three"]]);
    e.chain().setColumnLayout("threeWideMiddle").run();
    expect(widths(e.getJSON() as DocNode)).toEqual([25, 50, 25]);
    expect(columnTexts(e.getJSON() as DocNode)).toEqual([["one"], ["three"], []]);
  });

  it("come off and leave their blocks in reading order, the caret where it was", async () => {
    const e = await make();
    e.chain().insertContent("first").setColumns(2).run();
    e.chain().setTextSelection(endOfColumn(e, 1)).insertContent("second").run();
    e.chain().unsetColumns().insertContent("!").run();
    const doc = e.getJSON() as DocNode;
    expect(find(doc, "columns")).toHaveLength(0);
    expect(texts(doc)).toEqual(["first", "second!"]);
  });

  it("are read back from what the editor copies and from the reader's view", async () => {
    const content: JSONContent = {
      type: "doc",
      content: [
        {
          type: "columns",
          content: [
            { type: "column", attrs: { width: 33 }, content: [{ type: "paragraph", content: [{ type: "text", text: "a" }] }] },
            { type: "column", attrs: { width: 67 }, content: [{ type: "paragraph", content: [{ type: "text", text: "b" }] }] },
          ],
        },
      ],
    };
    const e = await make(content);
    const html = e.getHTML();
    e.destroy();
    const again = await make();
    again.commands.setContent(html);
    expect(again.getJSON().content?.[0]).toEqual(content.content?.[0]);
    again.commands.setContent('<div data-columns><div data-column style="--column-share: 90"><p>x</p></div><div data-column><p>y</p></div></div>');
    expect(widths(again.getJSON() as DocNode)).toEqual([null, null]);
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

describe("keys after a caret move", () => {
  it("land where the browser moved the caret, even before it reported the move", async () => {
    const e = await make({
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "Welcome." }] }, { type: "horizontalRule" }, { type: "paragraph" }],
    });
    const dom = e.view.dom;
    dom.setAttribute("tabindex", "0");
    document.body.append(dom);
    dom.focus();
    e.commands.setNodeSelection(10);
    expect(e.state.selection).toBeInstanceOf(NodeSelection);
    // End moves the browser's caret at once, but tells of it in a selectionchange
    // that a busy browser lets the next key overtake, as jsdom does here.
    const last = dom.lastElementChild as HTMLElement;
    document.getSelection()?.collapse(last, 0);
    dom.dispatchEvent(new KeyboardEvent("keydown", { key: "/", bubbles: true, cancelable: true }));
    dom.dispatchEvent(new KeyboardEvent("keypress", { key: "/", charCode: "/".charCodeAt(0), bubbles: true, cancelable: true }));
    expect(find(e.getJSON() as DocNode, "horizontalRule")).toHaveLength(1);
    expect(e.state.selection.from).toBe(e.state.doc.content.size - 1);
    dom.remove();
  });
});
