import { ARMATURE_LOOKUP_MAX_KEYS } from "@/config";
import type { DocNode } from "@/features/editor/schema";

/** The inline chip and the block that name an Armature issue by its key. */
export const ARMATURE_ISSUE_NODE = "armatureIssue";
export const ARMATURE_ISSUE_BLOCK_NODE = "armatureIssueBlock";

/** An issue key as Stator stores it, the shape Armature's keys have; the server's allowlist holds the same. */
export const ISSUE_KEY_PATTERN = /^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$/;

/**
 * A key just typed, then a space or a sign: the key starts the text or
 * follows a space or an opening sign, so "aCP-1" or "UTF-8x" stays text.
 */
export const TYPED_KEY = /(?:^|[\s([{"'])([A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17})([\s.,;:!?)\]}"'])$/;

/** The key upper case, or null when it is not one. */
export function normalizeKey(raw: unknown): string | null {
  if (typeof raw !== "string") return null;
  const key = raw.trim().toUpperCase();
  return ISSUE_KEY_PATTERN.test(key) ? key : null;
}

/** The project part of a key: CP of CP-12. */
export function projectOf(key: string): string {
  return key.slice(0, key.lastIndexOf("-"));
}

/** Where an issue opens in the Armature at baseUrl. */
export function issueUrl(baseUrl: string, key: string): string {
  return `${baseUrl.replace(/\/+$/, "")}/issues/${encodeURIComponent(key)}`;
}

/**
 * The key an address names when it is an issue of the Armature at baseUrl,
 * in any case and with any trailing slash, query or fragment; else null.
 */
export function keyFromIssueUrl(text: string, baseUrl: string | null | undefined): string | null {
  if (!baseUrl) return null;
  const trimmed = text.trim();
  if (!trimmed || /\s/.test(trimmed)) return null;
  let url: URL;
  let base: URL;
  try {
    url = new URL(trimmed);
    base = new URL(baseUrl);
  } catch {
    return null;
  }
  if (url.origin.toLowerCase() !== base.origin.toLowerCase() || url.username || url.password) return null;
  const match = /^\/issues\/([^/]+)\/?$/i.exec(url.pathname);
  if (!match?.[1]) return null;
  let key: string;
  try {
    key = decodeURIComponent(match[1]);
  } catch {
    return null;
  }
  return normalizeKey(key);
}

/** Every key a document's chips and blocks name, once each, in reading order. */
export function issueKeysOf(nodes: DocNode | readonly DocNode[] | null | undefined): string[] {
  const found = new Set<string>();
  const walk = (node: DocNode) => {
    if (node.type === ARMATURE_ISSUE_NODE || node.type === ARMATURE_ISSUE_BLOCK_NODE) {
      const key = normalizeKey(node.attrs?.key);
      if (key) found.add(key);
    }
    for (const child of node.content ?? []) walk(child);
  };
  if (Array.isArray(nodes)) for (const node of nodes) walk(node);
  else if (nodes) walk(nodes as DocNode);
  return [...found];
}

/** Keys in batches the lookup takes, sorted so the same keys make the same requests. */
export function lookupBatches(keys: readonly string[]): string[][] {
  const sorted = [...new Set(keys)].sort();
  const out: string[][] = [];
  for (let i = 0; i < sorted.length; i += ARMATURE_LOOKUP_MAX_KEYS) out.push(sorted.slice(i, i + ARMATURE_LOOKUP_MAX_KEYS));
  return out;
}
