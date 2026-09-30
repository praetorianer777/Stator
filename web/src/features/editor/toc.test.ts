import { describe, expect, it } from "vitest";
import { belowQuery, childPagesOptions, defaultChildPages, nestBelow } from "./childPages";
import type { DocNode } from "./schema";
import { buildToc, headingsOfDoc, tocMaxLevel, type TocEntry } from "./toc";

const heading = (level: number, text: string, id: string | null = null): DocNode => ({
  type: "heading",
  attrs: { level, id },
  content: text ? [{ type: "text", text }] : undefined,
});
const doc = (...content: DocNode[]): DocNode => ({ type: "doc", content });

/** The entries as indented lines, which reads better in a failure than nested objects. */
function outline(entries: TocEntry[], indent = ""): string[] {
  return entries.flatMap((e) => [`${indent}${e.text} #${e.anchor}`, ...outline(e.children, `${indent}  `)]);
}

describe("a table of contents", () => {
  it("nests each heading under the last one above it of a higher level", () => {
    const page = doc(
      heading(1, "Install", "install"),
      heading(2, "Linux", "linux"),
      heading(3, "Debian", "debian"),
      heading(2, "macOS", "macos"),
      heading(1, "Use", "use"),
      // A skipped level still goes under the heading above it.
      heading(3, "Shortcuts", "shortcuts"),
    );
    expect(outline(buildToc(headingsOfDoc(page), 3))).toEqual([
      "Install #install",
      "  Linux #linux",
      "    Debian #debian",
      "  macOS #macos",
      "Use #use",
      "  Shortcuts #shortcuts",
    ]);
  });

  it("stops at its depth, and a heading below the cut takes nothing with it", () => {
    const page = doc(heading(1, "Install", "install"), heading(2, "Linux", "linux"), heading(3, "Debian", "debian"), heading(1, "Use", "use"));
    expect(outline(buildToc(headingsOfDoc(page), 1))).toEqual(["Install #install", "Use #use"]);
    expect(outline(buildToc(headingsOfDoc(page), 2))).toEqual(["Install #install", "  Linux #linux", "Use #use"]);
  });

  it("links two headings with the same words to their own anchors", () => {
    const page = doc(heading(1, "Setup", "setup"), heading(2, "Notes", "notes"), heading(1, "Setup", "setup-2"), heading(2, "Notes", "notes-2"));
    expect(outline(buildToc(headingsOfDoc(page), 3))).toEqual(["Setup #setup", "  Notes #notes", "Setup #setup-2", "  Notes #notes-2"]);
  });

  it("gives a heading saved without an anchor the one the API would, clear of the saved ones", () => {
    const page = doc(heading(1, "Plan", null), heading(1, "Plan", "plan"), heading(2, "Übersicht & Ziele", null));
    expect(outline(buildToc(headingsOfDoc(page), 3))).toEqual(["Plan #plan-2", "Plan #plan", "  Übersicht & Ziele #übersicht-ziele"]);
  });

  it("reads headings inside panels, quotes and lists, and leaves out a heading with no words", () => {
    const page = doc(
      { type: "panel", attrs: { kind: "info" }, content: [heading(1, "In a panel", "in-a-panel")] },
      { type: "blockquote", content: [heading(2, "Quoted", "quoted")] },
      heading(1, "", "section"),
      heading(2, "  Spread\nover lines  ", "spread-over-lines"),
    );
    expect(outline(buildToc(headingsOfDoc(page), 3))).toEqual(["In a panel #in-a-panel", "  Quoted #quoted", "  Spread over lines #spread-over-lines"]);
  });

  it("is empty for a page without headings, or with nothing at all", () => {
    expect(buildToc(headingsOfDoc(doc({ type: "paragraph", content: [{ type: "text", text: "Just words." }] })), 3)).toEqual([]);
    expect(buildToc(headingsOfDoc(null), 3)).toEqual([]);
    expect(buildToc(headingsOfDoc(doc()), 3)).toEqual([]);
  });

  it("reads a stored depth only when the API would take it", () => {
    expect(tocMaxLevel(2)).toBe(2);
    for (const bad of [0, 4, 1.5, "2", null, undefined]) expect(tocMaxLevel(bad), String(bad)).toBe(3);
  });
});

describe("a child pages block's options", () => {
  it("reads stored attributes, and falls back where a document holds something else", () => {
    expect(childPagesOptions({ scope: "subtree", depth: 3, sort: "updated" })).toEqual({ scope: "subtree", depth: 3, sort: "updated" });
    expect(childPagesOptions({ scope: "space", depth: 11, sort: "created" })).toEqual(defaultChildPages);
    expect(childPagesOptions({ scope: "subtree", depth: 0 })).toEqual({ scope: "subtree", depth: null, sort: "tree" });
    expect(childPagesOptions(undefined)).toEqual(defaultChildPages);
  });

  it("asks the API for a depth only for a subtree cut at one", () => {
    expect(belowQuery({ scope: "children", depth: 4, sort: "title" })).toEqual({ scope: "children", sort: "title" });
    expect(belowQuery({ scope: "subtree", depth: null, sort: "tree" })).toEqual({ scope: "subtree", sort: "tree" });
    expect(belowQuery({ scope: "subtree", depth: 2, sort: "updated" })).toEqual({ scope: "subtree", sort: "updated", depth: 2 });
  });

  it("nests the API's list under the pages it names, in the order given", () => {
    const pages = [
      { id: "b", parentId: "root" },
      { id: "b1", parentId: "b" },
      { id: "b11", parentId: "b1" },
      { id: "a", parentId: "root" },
      { id: "a1", parentId: "a" },
    ];
    const ids = (entries: ReturnType<typeof nestBelow<{ id: string; parentId: string }>>): unknown[] =>
      entries.map((e) => (e.children.length ? [e.page.id, ids(e.children)] : e.page.id));
    expect(ids(nestBelow(pages, "root"))).toEqual([
      ["b", [["b1", ["b11"]]]],
      ["a", ["a1"]],
    ]);
  });
});
