import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { CODE_LANGUAGES, EDITOR_HEADING_LEVELS } from "@/config";
import { editorExtensions } from "./extensions";
import { CELL_BACKGROUNDS, PANEL_KINDS, safeHref, slug, type DocNode } from "./schema";
import { SLASH_ITEMS } from "./slashItems";

// The server's allowlist, generated from its Go table by make
// document-allowlist; the Go side refuses a stale copy.
interface Attr {
  kind: "string" | "integer" | "boolean" | "integers" | "null";
  nullable?: boolean;
  enum?: string[];
  min?: number;
  max?: number;
  maxLength?: number;
  pattern?: string;
  url?: boolean;
}
interface NodeSpec {
  attrs?: Record<string, Attr>;
  content?: string[];
  inline?: boolean;
  allowsMarks?: boolean;
}
interface Allowlist {
  nodes: Record<string, NodeSpec>;
  marks: Record<string, { attrs?: Record<string, Attr> }>;
}
const allowlist = JSON.parse(readFileSync(resolve(process.cwd(), "../api/document-allowlist.json"), "utf8")) as Allowlist;

function attrProblem(rule: Attr, value: unknown): string | null {
  if (value === null || value === undefined) return rule.nullable || rule.kind === "null" ? null : "is empty";
  const integer = (v: unknown) => typeof v === "number" && Number.isInteger(v) && v >= (rule.min ?? 0) && v <= (rule.max ?? 0);
  switch (rule.kind) {
    case "null":
      return "must be empty";
    case "boolean":
      return typeof value === "boolean" ? null : "is not a boolean";
    case "integer":
      return integer(value) ? null : "is out of range";
    case "integers":
      return Array.isArray(value) && value.length <= (rule.maxLength ?? 0) && value.every(integer) ? null : "is not a list of widths";
    case "string": {
      if (typeof value !== "string") return "is not a string";
      if (rule.maxLength && [...value].length > rule.maxLength) return "is too long";
      if (rule.enum && !rule.enum.includes(value)) return "is not one of the allowed values";
      if (rule.pattern && !new RegExp(rule.pattern, "u").test(value)) return "does not match its pattern";
      if (rule.url && safeHref(value) === null) return "is not a safe address";
      return null;
    }
  }
}

/** Everything the server would refuse in a document, as readable lines. */
function problems(node: DocNode, parent: NodeSpec = { content: ["doc"] }, path = "doc"): string[] {
  const spec = allowlist.nodes[node.type];
  if (!spec) return [`${path}: node ${node.type} is not allowed`];
  const out: string[] = [];
  if (!parent.content?.includes(node.type)) out.push(`${path}: ${node.type} may not sit here`);
  for (const [name, value] of Object.entries(node.attrs ?? {})) {
    const rule = spec.attrs?.[name];
    if (!rule) out.push(`${path}: attribute ${name} is not allowed`);
    else {
      const problem = attrProblem(rule, value);
      if (problem) out.push(`${path}: ${name}=${JSON.stringify(value)} ${problem}`);
    }
  }
  if (node.marks?.length && !(spec.inline && parent.allowsMarks)) out.push(`${path}: ${node.type} may not carry marks`);
  for (const mark of node.marks ?? []) {
    const markSpec = allowlist.marks[mark.type];
    if (!markSpec) {
      out.push(`${path}: mark ${mark.type} is not allowed`);
      continue;
    }
    for (const [name, value] of Object.entries(mark.attrs ?? {})) {
      const rule = markSpec.attrs?.[name];
      const problem = rule ? attrProblem(rule, value) : "is not allowed";
      if (problem) out.push(`${path}: ${mark.type}.${name}=${JSON.stringify(value)} ${problem}`);
    }
  }
  (node.content ?? []).forEach((child, i) => {
    out.push(...problems(child, spec, `${path}/${child.type}[${i}]`));
  });
  return out;
}

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
      .insertContent({ type: "mention", attrs: { id: "u1", label: "Ada" } })
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
      ],
    };
    expect(problems(hostile)).toHaveLength(4);
  });
});
