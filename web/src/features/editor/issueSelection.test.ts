import { afterEach, describe, expect, it } from "vitest";

import { Editor, type JSONContent } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import { CellSelection } from "@tiptap/pm/tables";
import { ARMATURE_CREATE_MAX_ITEMS, ARMATURE_SUMMARY_MAX_LENGTH } from "@/config";
import { problems } from "@/test/allowlist";
import { editorExtensions } from "./extensions";
import { cleanSummary, placeChips, planSelection } from "./issueSelection";
import type { DocNode } from "./schema";

let editor: Editor | undefined;
afterEach(() => editor?.destroy());

const text = (value: string): JSONContent => ({ type: "text", text: value });
const para = (...parts: (string | JSONContent)[]): JSONContent => ({
  type: "paragraph",
  content: parts.map((p) => (typeof p === "string" ? text(p) : p)),
});
const item = (type: "listItem" | "taskItem", value: string, nested?: JSONContent): JSONContent => ({
  type,
  ...(type === "taskItem" ? { attrs: { checked: false } } : {}),
  content: nested ? [para(value), nested] : [para(value)],
});
const cell = (type: "tableCell" | "tableHeader", value: string): JSONContent => ({
  type,
  content: [value ? para(value) : { type: "paragraph" }],
});
const row = (...cells: JSONContent[]): JSONContent => ({
  type: "tableRow",
  content: cells,
});

function make(...content: JSONContent[]) {
  editor = new Editor({
    element: document.createElement("div"),
    extensions: editorExtensions({}),
    content: { type: "doc", content },
  });
  return editor;
}

/** The position just inside the start of the first text matching words. */
function at(e: Editor, words: string, end = false): number {
  let found = -1;
  e.state.doc.descendants((node, pos) => {
    if (found >= 0 || !node.isText) return found < 0;
    const i = node.text?.indexOf(words) ?? -1;
    if (i >= 0) found = pos + i + (end ? words.length : 0);
    return false;
  });
  if (found < 0) throw new Error(`no text ${words}`);
  return found;
}

function select(e: Editor, from: number, to: number) {
  e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc, from, to)));
}

const summaries = (e: Editor) => planSelection(e.state)?.items.map((i) => i.summary) ?? [];

describe("splitting a selection into issues", () => {
  it("makes selected text inside one paragraph one issue, its white space made one", () => {
    const e = make(para("Notes: renew   the TLS certificate today"));
    select(e, at(e, "renew"), at(e, "certificate", true));
    const plan = planSelection(e.state);
    expect(plan?.kind).toBe("text");
    expect(summaries(e)).toEqual(["renew the TLS certificate"]);
  });

  it("makes nothing of an empty selection or of text in a code block", () => {
    const e = make(para("Plain words"), {
      type: "codeBlock",
      content: [text("make deploy")],
    });
    select(e, at(e, "Plain"), at(e, "Plain"));
    expect(planSelection(e.state)).toBeNull();
    select(e, at(e, "make"), at(e, "deploy", true));
    expect(planSelection(e.state)).toBeNull();
  });

  it("reads a chip already in the selection as its key", () => {
    const e = make(para("Follow up on ", { type: "armatureIssue", attrs: { key: "CP-1" } }, " soon"));
    select(e, at(e, "Follow"), at(e, "soon", true));
    expect(summaries(e)).toEqual(["Follow up on CP-1 soon"]);
  });

  it("makes one issue per list item, each without its nested list", () => {
    const e = make({
      type: "bulletList",
      content: [
        item("listItem", "Write the guide", {
          type: "bulletList",
          content: [item("listItem", "Draft the outline")],
        }),
        item("listItem", "Update the status page"),
        item("listItem", "Not selected"),
      ],
    });
    select(e, at(e, "Write"), at(e, "status page", true));
    const plan = planSelection(e.state);
    expect(plan?.kind).toBe("items");
    expect(summaries(e)).toEqual(["Write the guide", "Draft the outline", "Update the status page"]);
  });

  it("leaves out an item that only holds the selected ones", () => {
    const e = make({
      type: "orderedList",
      content: [
        item("listItem", "Parent", {
          type: "orderedList",
          content: [item("listItem", "First child"), item("listItem", "Second child")],
        }),
      ],
    });
    select(e, at(e, "First"), at(e, "Second child", true));
    expect(summaries(e)).toEqual(["First child", "Second child"]);
  });

  it("makes one issue per task item", () => {
    const e = make({
      type: "taskList",
      content: [item("taskItem", "Book the room"), item("taskItem", "Send the invite")],
    });
    select(e, at(e, "Book"), at(e, "invite", true));
    expect(summaries(e)).toEqual(["Book the room", "Send the invite"]);
  });

  it("makes one issue per table row from its first cell with text, header rows left out", () => {
    const e = make({
      type: "table",
      content: [
        row(cell("tableHeader", "Task"), cell("tableHeader", "Owner")),
        row(cell("tableCell", "Archive the old logs"), cell("tableCell", "Alice")),
        row(cell("tableCell", ""), cell("tableCell", "Rotate the backup keys")),
        row(cell("tableCell", ""), cell("tableCell", "")),
      ],
    });
    const cells: number[] = [];
    e.state.doc.descendants((node, pos) => {
      if (node.type.name === "tableHeader" || node.type.name === "tableCell") cells.push(pos);
    });
    e.view.dispatch(e.state.tr.setSelection(CellSelection.create(e.state.doc, cells[0]!, cells.at(-1)!)));
    expect(summaries(e)).toEqual(["Archive the old logs", "Rotate the backup keys"]);
  });

  it("cuts a summary to Armature's length, and files at most the most items", () => {
    expect([...cleanSummary("x".repeat(ARMATURE_SUMMARY_MAX_LENGTH + 10))]).toHaveLength(ARMATURE_SUMMARY_MAX_LENGTH);
    const many = Array.from({ length: ARMATURE_CREATE_MAX_ITEMS + 2 }, (_, i) => item("listItem", `Item ${i + 1}`));
    const e = make({ type: "bulletList", content: many });
    select(e, at(e, "Item 1"), at(e, `Item ${ARMATURE_CREATE_MAX_ITEMS + 2}`, true));
    const plan = planSelection(e.state);
    expect(plan?.items).toHaveLength(ARMATURE_CREATE_MAX_ITEMS);
    expect(plan?.dropped).toBe(2);
  });
});

describe("placing the chips of the issues made", () => {
  const body = (e: Editor) => e.getJSON() as DocNode;

  it("puts the chip in the selected text's place, and one undo takes it back", () => {
    const e = make(para("Then renew the TLS certificate."));
    select(e, at(e, "renew"), at(e, "certificate", true));
    const plan = planSelection(e.state)!;
    e.view.dispatch(placeChips(e.state.tr, plan, ["CP-6"]));
    expect(body(e).content?.[0]?.content).toEqual([text("Then "), { type: "armatureIssue", attrs: { key: "CP-6" } }, text(".")]);
    expect(problems(body(e))).toEqual([]);
    e.commands.undo();
    expect(e.state.doc.textContent).toBe("Then renew the TLS certificate.");
  });

  it("keeps each item's text, puts its chip after it, and leaves the ones not made alone", () => {
    const e = make({
      type: "bulletList",
      content: [item("listItem", "Write the guide"), item("listItem", "Update the page"), item("listItem", "Tell support ")],
    });
    select(e, at(e, "Write"), at(e, "support", true));
    const plan = planSelection(e.state)!;
    e.view.dispatch(placeChips(e.state.tr, plan, ["CP-7", null, "CP-8"]));
    const firstParagraphs = body(e).content?.[0]?.content?.map((li) => li.content?.[0]?.content);
    expect(firstParagraphs).toEqual([
      [text("Write the guide "), { type: "armatureIssue", attrs: { key: "CP-7" } }],
      [text("Update the page")],
      [text("Tell support "), { type: "armatureIssue", attrs: { key: "CP-8" } }],
    ]);
    expect(problems(body(e))).toEqual([]);
    e.commands.undo();
    expect(JSON.stringify(body(e))).not.toContain("armatureIssue");
  });

  it("puts a row's chip after the text that named it", () => {
    const e = make({
      type: "table",
      content: [row(cell("tableCell", ""), cell("tableCell", "Rotate the keys"))],
    });
    const cells: number[] = [];
    e.state.doc.descendants((node, pos) => {
      if (node.type.name === "tableCell") cells.push(pos);
    });
    e.view.dispatch(e.state.tr.setSelection(CellSelection.create(e.state.doc, cells[0]!, cells[1]!)));
    e.view.dispatch(placeChips(e.state.tr, planSelection(e.state)!, ["CP-9"]));
    expect(body(e).content?.[0]?.content?.[0]?.content?.[1]?.content?.[0]?.content).toEqual([
      text("Rotate the keys "),
      { type: "armatureIssue", attrs: { key: "CP-9" } },
    ]);
  });
});
