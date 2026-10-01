import { afterEach, describe, expect, it } from "vitest";
import { Editor, type JSONContent } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import { editorExtensions } from "./extensions";
import { findKey, findMatches, replaceAll, replaceCurrent, selectedQuery, setFindQuery, stepMatch } from "./findReplace";

let editor: Editor | undefined;
afterEach(() => editor?.destroy());

const text = (value: string, marks?: string[]): JSONContent => ({ type: "text", text: value, ...(marks ? { marks: marks.map((type) => ({ type })) } : {}) });
const para = (...parts: (string | JSONContent)[]): JSONContent => ({ type: "paragraph", content: parts.map((p) => (typeof p === "string" ? text(p) : p)) });
const mention = (label: string): JSONContent => ({ type: "mention", attrs: { id: "u1", label } });
const chip = (key: string): JSONContent => ({ type: "armatureIssue", attrs: { key } });

function make(...content: JSONContent[]) {
  editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions({}), content: { type: "doc", content } });
  return editor;
}

function look(e: Editor, query: string, caseSensitive = false) {
  e.view.dispatch(setFindQuery(e.state, query, caseSensitive));
  return findKey.getState(e.state)!;
}

function found(e: Editor): string[] {
  const state = findKey.getState(e.state)!;
  return state.matches.map((m) => e.state.doc.textBetween(m.from, m.to));
}

describe("finding", () => {
  it("ignores case unless asked, and finds every occurrence in every block", () => {
    const e = make(para("Draft, draft and DRAFT."), { type: "heading", attrs: { level: 1 }, content: [text("A draft")] });
    expect(look(e, "draft").matches).toHaveLength(4);
    expect(look(e, "Draft", true).matches).toHaveLength(1);
    expect(found(e)).toEqual(["Draft"]);
    expect(look(e, "DRAFT", true).matches).toHaveLength(1);
  });

  it("finds words that run across marks", () => {
    const e = make(para("a ", text("dra", ["bold"]), text("ft", ["italic"]), " plan"));
    expect(look(e, "draft").matches).toHaveLength(1);
    expect(found(e)).toEqual(["draft"]);
  });

  it("never matches inside a mention or a chip, nor across one", () => {
    const e = make(para("Ask ", mention("Draft Bot"), " about ", chip("DRAFT-1"), "draft"));
    look(e, "draft");
    expect(found(e)).toEqual(["draft"]);
    expect(look(e, "Bot").matches).toHaveLength(0);
    expect(look(e, "about draft").matches).toHaveLength(0);
    expect(look(e, "￼").matches).toHaveLength(0);
  });

  it("does not run from one block into the next", () => {
    const e = make(para("end of dr"), para("aft"));
    expect(look(e, "draft").matches).toHaveLength(0);
  });

  it("keeps each position when a letter lowers to more than one character", () => {
    const doc = make(para("İ draft")).state.doc;
    expect(findMatches(doc, "draft", false)).toEqual([{ from: 3, to: 8 }]);
  });

  it("starts at the caret and steps round in both directions", () => {
    const e = make(para("one x two x three x"));
    e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc, 8)));
    expect(look(e, "x").current).toBe(1);
    e.view.dispatch(stepMatch(e.state, 1)!);
    expect(findKey.getState(e.state)!.current).toBe(2);
    e.view.dispatch(stepMatch(e.state, 1)!);
    expect(findKey.getState(e.state)!.current).toBe(0);
    e.view.dispatch(stepMatch(e.state, -1)!);
    expect(findKey.getState(e.state)!.current).toBe(2);
    const { from, to } = e.state.selection;
    expect(e.state.doc.textBetween(from, to)).toBe("x");
  });

  it("follows edits made while the bar is open", () => {
    const e = make(para("cat"));
    expect(look(e, "cat").matches).toHaveLength(1);
    e.commands.insertContentAt(e.state.doc.content.size - 1, " cat");
    expect(findKey.getState(e.state)!.matches).toHaveLength(2);
  });

  it("draws the matches as decorations, the current one apart", () => {
    const e = make(para("cat cat"));
    look(e, "cat");
    expect(e.view.dom.querySelectorAll("[data-find-match]")).toHaveLength(2);
    expect(e.view.dom.querySelectorAll('[data-find-match="current"]')).toHaveLength(1);
    look(e, "");
    expect(e.view.dom.querySelectorAll("[data-find-match]")).toHaveLength(0);
  });

  it("offers the selected words to look for, unless they leave their block", () => {
    const e = make(para("find me"), para("not me"));
    e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc, 1, 5)));
    expect(selectedQuery(e.state)).toBe("find");
    e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc, 1, 12)));
    expect(selectedQuery(e.state)).toBe("");
  });
});

describe("replacing", () => {
  it("replaces the current match, keeps its marks and moves to the next", () => {
    const e = make(para(text("draft", ["bold"]), " and draft"));
    look(e, "draft");
    e.view.dispatch(replaceCurrent(e.state, "plan")!);
    expect(e.getJSON().content?.[0]?.content?.[0]).toEqual(text("plan", ["bold"]));
    const state = findKey.getState(e.state)!;
    expect(state.matches).toHaveLength(1);
    expect(state.current).toBe(0);
  });

  it("does not find again what it just put in", () => {
    const e = make(para("a a"));
    look(e, "a");
    e.view.dispatch(replaceCurrent(e.state, "aa")!);
    const state = findKey.getState(e.state)!;
    expect(e.state.doc.textBetween(state.matches[state.current]!.from, state.matches[state.current]!.to)).toBe("a");
    expect(state.current).toBe(2);
    expect(state.matches[state.current]!.from).toBe(4);
  });

  it("replaces every match, across marks, in one step that one undo takes back", () => {
    const e = make(para("Draft one, ", text("dra", ["bold"]), "ft two"), para("draft three"));
    e.commands.insertContentAt(e.state.doc.content.size - 1, ".");
    look(e, "draft");
    const done = replaceAll(e.state, "plan")!;
    expect(done.count).toBe(3);
    e.view.dispatch(done.tr);
    expect(e.getText()).toBe("plan one, plan two\n\nplan three.");
    expect(findKey.getState(e.state)!.matches).toHaveLength(0);
    e.commands.undo();
    expect(e.getText()).toBe("Draft one, draft two\n\ndraft three.");
    e.commands.undo();
    expect(e.getText()).toBe("Draft one, draft two\n\ndraft three");
  });

  it("replaces only the matching case when asked", () => {
    const e = make(para("Draft draft"));
    look(e, "draft", true);
    e.view.dispatch(replaceAll(e.state, "plan")!.tr);
    expect(e.getText()).toBe("Draft plan");
  });

  it("removes the matches when the replacement is empty", () => {
    const e = make(para("a very very long way"));
    look(e, "very ");
    e.view.dispatch(replaceAll(e.state, "")!.tr);
    expect(e.getText()).toBe("a long way");
  });

  it("leaves chips and mentions whole", () => {
    const e = make(para(mention("Ann"), " Ann ", chip("ANN-1")));
    look(e, "Ann");
    e.view.dispatch(replaceAll(e.state, "Bo")!.tr);
    const content: JSONContent[] = e.getJSON().content?.[0]?.content ?? [];
    expect(content.map((n) => n.type)).toEqual(["mention", "text", "armatureIssue"]);
    expect(content[1]?.text).toBe(" Bo ");
  });

  it("does nothing without a match", () => {
    const e = make(para("nothing"));
    look(e, "zzz");
    expect(replaceCurrent(e.state, "x")).toBeNull();
    expect(replaceAll(e.state, "x")).toBeNull();
    expect(stepMatch(e.state, 1)).toBeNull();
  });
});
