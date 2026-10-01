import { EDITOR_HEADING_LEVELS, HEADING_SLUG_MAX_LENGTH } from "@/config";

/**
 * The document the editor, the reader and the server agree on. The server's
 * allowlist is the authority; allowlist.test.ts keeps this side within it.
 */
export const PANEL_KINDS = ["info", "note", "success", "warning", "error"] as const;
export const CELL_BACKGROUNDS = ["neutral", "accent", "success", "warning", "danger"] as const;
export const HEADING_LEVELS = EDITOR_HEADING_LEVELS;
export const CHILD_PAGES_SCOPES = ["children", "subtree"] as const;
export const CHILD_PAGES_SORTS = ["tree", "title", "updated"] as const;

/** The mark an inline thread's passage carries in a page body. */
export const INLINE_COMMENT_MARK = "inlineComment";

export type PanelKind = (typeof PANEL_KINDS)[number];
export type CellBackground = (typeof CELL_BACKGROUNDS)[number];
export type ChildPagesScope = (typeof CHILD_PAGES_SCOPES)[number];
export type ChildPagesSort = (typeof CHILD_PAGES_SORTS)[number];

export interface DocMark {
  type: string;
  attrs?: Record<string, unknown>;
}

export interface DocNode {
  type: string;
  text?: string;
  attrs?: Record<string, unknown>;
  marks?: DocMark[];
  content?: DocNode[];
}

export interface Doc extends DocNode {
  type: "doc";
}

/** Somebody the editor can name with an at sign. */
export interface Mentionable {
  id: string;
  name: string;
  email?: string;
  /** False for somebody who may not view the page: they can be named, and are not told. */
  canView?: boolean;
}

/** Finds the people an at sign may name, for what was typed after it. */
export type MentionSource = (query: string, signal: AbortSignal) => Promise<Mentionable[]>;

export const emptyDoc: Doc = { type: "doc", content: [{ type: "paragraph" }] };

/** A document that says nothing: no blocks, or only paragraphs with nothing in them. */
export function isEmptyDoc(doc: DocNode | null | undefined): boolean {
  if (!doc) return true;
  return (doc.content ?? []).every((block) => block.type === "paragraph" && !(block.content && block.content.length > 0));
}

const SCHEMES = ["http", "https", "mailto"];
const MAX_HREF_LENGTH = 2048;

/**
 * A web or mail address or a link within the site, and nothing a browser
 * would run; the same rule as the API's SafeHref.
 */
export function safeHref(href: unknown): string | null {
  if (typeof href !== "string" || href === "" || [...href].length > MAX_HREF_LENGTH) return null;
  // Browsers drop tabs and newlines inside a URL, so "java\tscript:" would
  // run; a backslash reads as a slash and makes "/\host" leave the site.
  for (const ch of href) {
    const code = ch.charCodeAt(0);
    if (code <= 0x20 || code === 0x7f || ch === "\\") return null;
  }
  if (href.startsWith("//")) return null;
  const colon = href.indexOf(":");
  const end = href.search(/[/?#]/);
  if (colon < 0 || (end >= 0 && end < colon)) return href;
  const scheme = href.slice(0, colon).toLowerCase();
  if (!SCHEMES.includes(scheme)) return null;
  if (scheme === "mailto") return href.length > colon + 1 ? href : null;
  try {
    return new URL(href).host ? href : null;
  } catch {
    return null;
  }
}

/** The text of a node and everything under it, for previews and for the unknown. */
export function textOf(node: DocNode): string {
  if (node.type === "text") return node.text ?? "";
  if (node.type === "mention") return `@${String(node.attrs?.label ?? "")}`;
  if (node.type === "hardBreak") return "\n";
  if (node.type === "attachment") return String(node.attrs?.fileName ?? "");
  if (node.type === "armatureIssue") return String(node.attrs?.key ?? "");
  return (node.content ?? []).map(textOf).join("");
}

/** What a heading anchor looks like; the API refuses anything else. */
export const ANCHOR_PATTERN = /^[\p{Ll}\p{Lo}\p{Lm}\p{N}]+(?:-[\p{Ll}\p{Lo}\p{Lm}\p{N}]+)*$/u;
const SLUG_CHAR = /[\p{Ll}\p{Lo}\p{Lm}\p{N}]/u;
export const FALLBACK_SLUG = "section";

/** Heading text as an anchor, the same way the API's Slug makes one. */
export function slug(text: string): string {
  let out = "";
  let count = 0;
  let hyphen = false;
  for (const ch of text.toLowerCase()) {
    if (count >= HEADING_SLUG_MAX_LENGTH) break;
    if (SLUG_CHAR.test(ch)) {
      if (hyphen && out) {
        out += "-";
        count++;
      }
      hyphen = false;
      out += ch;
      count++;
      continue;
    }
    hyphen = true;
  }
  return out || FALLBACK_SLUG;
}

/** The slug, or the slug with the first free number from 2 appended. */
export function dedupe(base: string, taken: Set<string>): string {
  if (!taken.has(base)) return base;
  for (let i = 2; ; i++) {
    const candidate = `${base}-${i}`;
    if (!taken.has(candidate)) return candidate;
  }
}

/** The address that opens the page at a heading. */
export function headingLink(anchor: string, location: Pick<Location, "origin" | "pathname" | "search"> = window.location): string {
  return `${location.origin}${location.pathname}${location.search}#${encodeURIComponent(anchor)}`;
}
