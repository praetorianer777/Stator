import type { Node as PMNode } from "@tiptap/pm/model";
import { HEADING_LEVELS, ANCHOR_PATTERN, dedupe, slug, textOf, type DocNode } from "./schema";

/** A heading as the page holds it: its level, its saved anchor if any, and its words. */
export interface FoundHeading {
  level: number;
  id: string | null;
  text: string;
}

/** One line of a table of contents, with the headings nested under it. */
export interface TocEntry {
  level: number;
  anchor: string;
  text: string;
  children: TocEntry[];
}

const oneLine = (text: string) => text.replace(/\s+/g, " ").trim();

function found(level: unknown, id: unknown, text: string): FoundHeading {
  return { level: Number(level) || 1, id: typeof id === "string" && ANCHOR_PATTERN.test(id) ? id : null, text: oneLine(text) };
}

function walkHeadings(doc: DocNode | null | undefined): Array<{ node: DocNode; heading: FoundHeading }> {
  const out: Array<{ node: DocNode; heading: FoundHeading }> = [];
  const walk = (nodes: DocNode[] | undefined) => {
    for (const node of nodes ?? []) {
      if (node.type === "heading") out.push({ node, heading: found(node.attrs?.level, node.attrs?.id, textOf(node)) });
      else walk(node.content);
    }
  };
  walk(doc?.content);
  return out;
}

/** The headings of a stored document in reading order, those inside panels, quotes and lists too. */
export function headingsOfDoc(doc: DocNode | null | undefined): FoundHeading[] {
  return walkHeadings(doc).map((entry) => entry.heading);
}

/**
 * The anchor of each heading, in order: the saved one, or the one made from
 * its words, numbered when another heading already has it. The contents list
 * and the headings it links to both take theirs from here, so a link always
 * has something to land on.
 */
export function anchorsOf(headings: FoundHeading[]): string[] {
  const taken = new Set(headings.flatMap((h) => (h.id ? [h.id] : [])));
  return headings.map((heading) => {
    if (heading.id) return heading.id;
    const anchor = dedupe(slug(heading.text), taken);
    taken.add(anchor);
    return anchor;
  });
}

/** The anchor of every heading node of a stored document, saved or not. */
export function anchorsByNode(doc: DocNode | null | undefined): Map<DocNode, string> {
  const entries = walkHeadings(doc);
  const anchors = anchorsOf(entries.map((entry) => entry.heading));
  return new Map(entries.map((entry, i) => [entry.node, anchors[i]!]));
}

/** The same for the document open in the editor. */
export function headingsOfEditor(doc: PMNode): FoundHeading[] {
  const out: FoundHeading[] = [];
  doc.descendants((node) => {
    if (node.type.name === "heading") out.push(found(node.attrs.level, node.attrs.id, node.textContent));
    return !node.isTextblock;
  });
  return out;
}

/**
 * The table of contents of headings down to maxLevel. A heading goes under
 * the last one above it of a higher level, so a skipped level still nests; a
 * heading without words is left out, and one without an anchor gets the one
 * the API's Headings would give it.
 */
export function buildToc(headings: FoundHeading[], maxLevel: number): TocEntry[] {
  const anchors = anchorsOf(headings);
  const roots: TocEntry[] = [];
  const open: TocEntry[] = [];
  headings.forEach((heading, i) => {
    if (heading.level > maxLevel || !heading.text) return;
    const entry: TocEntry = { level: heading.level, anchor: anchors[i]!, text: heading.text, children: [] };
    while (open.length > 0 && open[open.length - 1]!.level >= heading.level) open.pop();
    (open.length > 0 ? open[open.length - 1]!.children : roots).push(entry);
    open.push(entry);
  });
  return roots;
}

/** A stored maxLevel the API takes, or the deepest level when it is anything else. */
export function tocMaxLevel(value: unknown): number {
  const deepest = Math.max(...HEADING_LEVELS);
  return typeof value === "number" && Number.isInteger(value) && value >= 1 && value <= deepest ? value : deepest;
}
