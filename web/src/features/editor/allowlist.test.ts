import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { CODE_LANGUAGES, COLUMN_LAYOUTS, COLUMN_SHARE_MAX, COLUMN_SHARE_MIN, EDITOR_HEADING_LEVELS, EXPAND_TITLE_MAX_LENGTH } from "@/config";
import { allowlist, attrProblem, problems } from "@/test/allowlist";
import { anchorHeadings, editorExtensions } from "./extensions";
import { CELL_BACKGROUNDS, PANEL_KINDS, slug, type DocNode } from "./schema";
import { SLASH_ITEMS } from "./slashItems";

let editor: Editor | undefined;
afterEach(() => editor?.destroy());

function make() {
  editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions() });
  return editor;
}

describe("the web editor against the server's allowlist", () => {
  it("has no node or mark type the server does not know, and no attribute either", () => {
    const e = make();
    for (const [name, type] of Object.entries(e.schema.nodes)) {
      expect(allowlist.nodes, `node ${name}`).toHaveProperty([name]);
      expect(Object.keys(type.spec.attrs ?? {}).sort(), `attributes of ${name}`).toEqual(Object.keys(allowlist.nodes[name]?.attrs ?? {}).sort());
    }
    for (const [name, type] of Object.entries(e.schema.marks)) {
      expect(allowlist.marks, `mark ${name}`).toHaveProperty([name]);
      expect(Object.keys(type.spec.attrs ?? {}).sort(), `attributes of ${name}`).toEqual(Object.keys(allowlist.marks[name]?.attrs ?? {}).sort());
    }
    // And the other way: nothing the server allows is missing from the editor.
    expect(Object.keys(allowlist.nodes).sort()).toEqual(Object.keys(e.schema.nodes).sort());
    expect(Object.keys(allowlist.marks).sort()).toEqual(Object.keys(e.schema.marks).sort());
  });

  it("produces documents the server accepts, from every block and style it offers", () => {
    const docs: DocNode[] = [];
    for (const item of SLASH_ITEMS) {
      const e = make();
      e.commands.insertContent("Heading text");
      item.run(e.chain().focus());
      docs.push(e.getJSON() as DocNode);
      e.destroy();
    }
    const e = make();
    e.chain()
      .insertContent([
        { type: "paragraph", content: [{ type: "text", text: "styled" }] },
        { type: "paragraph", content: [{ type: "text", text: "linked" }] },
      ])
      .run();
    e.chain().selectAll().toggleBold().toggleItalic().toggleStrike().run();
    e.chain().setTextSelection({ from: 2, to: 4 }).setLink({ href: "https://example.test" }).run();
    e.chain()
      .focus("end")
      .setHardBreak()
      .insertContent({ type: "mention", attrs: { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", label: "Ada" } })
      .insertContent(" ")
      .toggleCode()
      .insertContent("x")
      .run();
    e.chain()
      .focus("end")
      .insertContent({ type: "orderedList", attrs: { start: 3 }, content: [{ type: "listItem", content: [{ type: "paragraph" }] }] })
      .run();
    e.chain().focus("end").toggleTaskList().run();
    e.commands.updateAttributes("taskItem", { checked: true });
    e.chain().focus("end").insertTable({ rows: 2, cols: 2, withHeaderRow: true }).setCellAttribute("background", "accent").toggleHeaderColumn().run();
    e.chain().focus("end").setCodeBlock({ language: "go" }).insertContent("x := 1").run();
    e.chain().focus("end").setPanel("error").setHeading({ level: 3 }).run();
    const attachmentId = "0195f000-0000-7000-8000-0000000000f1";
    e.chain()
      .focus("end")
      .insertContent([
        { type: "image", attrs: { attachmentId, alt: "A picture", width: 480 } },
        { type: "paragraph", content: [{ type: "attachment", attrs: { attachmentId, fileName: "plan.pdf" } }] },
        { type: "paragraph", content: [{ type: "text", text: "Say more", marks: [{ type: "hint" }] }] },
        { type: "expand", attrs: { title: "More" }, content: [{ type: "paragraph", content: [{ type: "text", text: "Hidden" }] }] },
      ])
      .insertArmatureIssueBlock("cp-4")
      .run();
    // A second chain: an inserted atom is selected, and the next insert would replace it.
    e.chain()
      .focus("end")
      .insertArmatureIssueList({ query: "statusCategory != done", columns: ["summary", "due"], limit: 100 })
      .run();
    docs.push(e.getJSON() as DocNode);

    for (const doc of docs) expect(problems(doc)).toEqual([]);
    const all = JSON.stringify(docs);
    for (const needle of [
      '"bold"',
      '"italic"',
      '"strike"',
      '"code"',
      '"link"',
      '"mention"',
      '"hardBreak"',
      '"background":"accent"',
      '"checked":true',
      '"start":3',
      '"language":"go"',
      '"type":"image"',
      '"width":480',
      '"type":"attachment"',
      '"type":"hint"',
      '"type":"armatureIssueBlock"',
      '"type":"expand","attrs":{"title":"More"}',
      '"columns":["summary","due"]',
    ]) {
      expect(all).toContain(needle);
    }
  });

  it("offers exactly the choices the server takes", () => {
    const enumOf = (node: string, attr: string) => allowlist.nodes[node]?.attrs?.[attr]?.enum;
    expect(enumOf("panel", "kind")).toEqual([...PANEL_KINDS]);
    expect(enumOf("tableCell", "background")).toEqual([...CELL_BACKGROUNDS]);
    expect(enumOf("tableHeader", "background")).toEqual([...CELL_BACKGROUNDS]);
    expect(allowlist.nodes.heading?.attrs?.level?.max).toBe(Math.max(...EDITOR_HEADING_LEVELS));
    expect(allowlist.nodes.expand?.attrs?.title?.maxLength).toBe(EXPAND_TITLE_MAX_LENGTH);
    const share = allowlist.nodes.column?.attrs?.width;
    expect([share?.min, share?.max]).toEqual([COLUMN_SHARE_MIN, COLUMN_SHARE_MAX]);
    const counts = COLUMN_LAYOUTS.map((layout) => layout.widths.length);
    expect([Math.min(...counts), Math.max(...counts)]).toEqual([allowlist.nodes.columns?.minContent, allowlist.nodes.columns?.maxContent]);
    for (const layout of COLUMN_LAYOUTS) for (const width of layout.widths) expect(attrProblem(share!, width), layout.key).toBeNull();
    const language = allowlist.nodes.codeBlock?.attrs?.language;
    for (const { id } of CODE_LANGUAGES) expect(attrProblem(language!, id), id).toBeNull();
    const anchor = allowlist.nodes.heading?.attrs?.id;
    for (const text of ["Getting started", "Übersicht & Ziele", "日本語", "!!!", "a".repeat(200), "x ".repeat(100)]) {
      expect(attrProblem(anchor!, slug(text)), text).toBeNull();
    }
  });

  it("finds what the server would refuse", () => {
    const hostile: DocNode = {
      type: "doc",
      content: [
        { type: "iframe" },
        { type: "paragraph", attrs: { style: "x" }, content: [{ type: "text", text: "x", marks: [{ type: "link", attrs: { href: "javascript:alert(1)" } }] }] },
        { type: "panel", attrs: { kind: "danger" }, content: [{ type: "paragraph" }] },
        { type: "columns", content: [{ type: "column", attrs: { width: 50 }, content: [{ type: "paragraph" }] }] },
      ],
    };
    expect(problems(hostile)).toHaveLength(5);
  });
});

// The API serves this file as it is; the Go side validates it too.
const builtIns = JSON.parse(readFileSync(resolve(process.cwd(), "../backend/internal/template/builtin/en.json"), "utf8")) as {
  templates: Array<{ key: string; name: string; body: DocNode }>;
};

const countHints = (doc: DocNode) => JSON.stringify(doc).split('"type":"hint"').length - 1;

describe("the built-in templates", () => {
  it("are all there", () => {
    expect(builtIns.templates.map((tpl) => tpl.key)).toEqual([
      "meeting-notes",
      "how-to",
      "troubleshooting",
      "retrospective",
      "decision-record",
      "product-requirements",
      "project-plan",
    ]);
  });

  it.each(builtIns.templates.map((tpl) => [tpl.key, tpl] as const))("%s is a document the server takes, and the editor keeps", (_key, tpl) => {
    expect(problems(tpl.body)).toEqual([]);
    expect(countHints(tpl.body)).toBeGreaterThan(0);
    editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions(), content: tpl.body });
    // Its anchors are the ones the editor gives, so opening it changes nothing.
    expect(anchorHeadings(editor.state)).toBeNull();
    const edited = editor.getJSON() as DocNode;
    expect(problems(edited)).toEqual([]);
    expect(countHints(edited)).toBe(countHints(tpl.body));
  });
});
