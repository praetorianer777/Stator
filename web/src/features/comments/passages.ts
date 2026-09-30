import { INLINE_COMMENT_MARK, type Doc, type DocNode } from "@/features/editor/schema";

/** A run of text in one block: the block's place in the document, and offsets into its text nodes' text. */
export interface PassageRange {
  path: number[];
  start: number;
  end: number;
}

export interface Selected extends PassageRange {
  quote: string;
}

export function nodeAt(doc: DocNode, path: readonly number[]): DocNode | undefined {
  let node: DocNode | undefined = doc;
  for (const i of path) node = node?.content?.[i];
  return node;
}

/** A block's text as a passage counts it: its text nodes, and nothing for a mention, a file or a break. */
export function blockText(block: DocNode | undefined): string {
  return (block?.content ?? []).map((n) => (n.type === "text" ? (n.text ?? "") : "")).join("");
}

export function quoteOf(doc: DocNode, range: PassageRange): string {
  return blockText(nodeAt(doc, range.path)).slice(range.start, range.end);
}

/**
 * The body with a passage's text split out and marked for a new thread, a mention or file inside it
 * too, so the passage has no gaps; nothing else changes, as the server requires.
 */
export function markPassage(doc: Doc, range: PassageRange, threadId: string): Doc {
  const copy = structuredClone(doc);
  const block = nodeAt(copy, range.path);
  if (!block?.content) return copy;
  const mark = { type: INLINE_COMMENT_MARK, attrs: { threadId } };
  const withMark = (node: DocNode): DocNode => ({ ...node, marks: [...(node.marks ?? []), mark] });
  const content: DocNode[] = [];
  let offset = 0;
  for (const node of block.content) {
    if (node.type !== "text") {
      content.push(range.start < offset && offset < range.end ? withMark(node) : node);
      continue;
    }
    const text = node.text ?? "";
    const from = offset;
    const to = offset + text.length;
    offset = to;
    if (to <= range.start || from >= range.end) {
      content.push(node);
      continue;
    }
    const piece = (a: number, b: number): DocNode => ({ ...node, text: text.slice(a - from, b - from) });
    if (from < range.start) content.push(piece(from, range.start));
    content.push(withMark(piece(Math.max(from, range.start), Math.min(to, range.end))));
    if (to > range.end) content.push(piece(range.end, to));
  }
  block.content = content;
  return copy;
}

/** Where a quote occurs exactly once within one block, or null. */
export function findPassage(doc: DocNode, quote: string): PassageRange | null {
  if (!quote) return null;
  let found: PassageRange | null = null;
  let count = 0;
  const walk = (node: DocNode, path: number[]) => {
    const inline = node.content?.some((child) => child.type === "text");
    if (inline) {
      const text = blockText(node);
      for (let at = text.indexOf(quote); at >= 0; at = text.indexOf(quote, at + 1)) {
        count += 1;
        found = { path, start: at, end: at + quote.length };
      }
      return;
    }
    node.content?.forEach((child, i) => {
      walk(child, [...path, i]);
    });
  };
  walk(doc, []);
  return count === 1 ? found : null;
}

/** Where a selection's passage is in a body read afresh: its old place if its words are still there, else its one other place. */
export function relocate(doc: DocNode, selected: Selected): PassageRange | null {
  if (quoteOf(doc, selected) === selected.quote) return selected;
  return findPassage(doc, selected.quote);
}

// Text the reader draws that is not a text node of the document.
const NOT_TEXT = "[data-mention], [data-attachment-chip], .sr-only";

/** How far into a block's text a point of the page lies. */
function offsetIn(block: Element, node: Node, offset: number): number {
  const point = document.createRange();
  point.setStart(node, offset);
  point.collapse(true);
  const walker = document.createTreeWalker(block, NodeFilter.SHOW_TEXT);
  let total = 0;
  for (let text = walker.nextNode() as Text | null; text; text = walker.nextNode() as Text | null) {
    const counted = !text.parentElement?.closest(NOT_TEXT);
    if (text === node) return counted ? total + offset : total;
    if (point.comparePoint(text, 0) >= 0) break;
    if (counted) total += text.length;
  }
  return total;
}

function blockOf(node: Node, root: Element): HTMLElement | null {
  const element = node instanceof Element ? node : node.parentElement;
  const block = element?.closest<HTMLElement>("[data-block]") ?? null;
  return block && root.contains(block) ? block : null;
}

/**
 * The passage a selection in the reader covers, trimmed, or null when it is empty or leaves its block;
 * ending at the very start of the next block, as a triple click does, is ending with its own.
 */
export function selectedPassage(root: Element, doc: DocNode, selection: Selection | null): Selected | null {
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null;
  const range = selection.getRangeAt(0);
  const block = blockOf(range.startContainer, root);
  if (!block) return null;
  const path = (block.dataset.block ?? "").split(".").map(Number);
  const text = blockText(nodeAt(doc, path));
  let start = offsetIn(block, range.startContainer, range.startOffset);
  let end: number;
  const endBlock = blockOf(range.endContainer, root);
  if (endBlock === block) end = offsetIn(block, range.endContainer, range.endOffset);
  else if (endBlock && offsetIn(endBlock, range.endContainer, range.endOffset) === 0) end = text.length;
  else return null;
  while (start < end && /\s/.test(text[start] ?? "")) start += 1;
  while (end > start && /\s/.test(text[end - 1] ?? "")) end -= 1;
  if (start >= end) return null;
  return { path, start, end, quote: text.slice(start, end) };
}
