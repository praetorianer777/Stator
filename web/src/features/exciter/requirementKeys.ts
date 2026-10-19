import { EXCITER_KEY_MAX_LENGTH, EXCITER_LOOKUP_MAX_KEYS } from "@/config";
import type { DocNode } from "@/features/editor/schema";

/** The inline chip that names an Exciter requirement by its key. */
export const EXCITER_REQUIREMENT_NODE = "exciterRequirement";
/** The block that shows one requirement with its statement and rationale. */
export const EXCITER_REQUIREMENT_BLOCK_NODE = "exciterRequirementBlock";
/** The block that lists what a query finds in one project. */
export const EXCITER_REQUIREMENT_LIST_NODE = "exciterRequirementList";

/** A project key as Exciter writes it, the part of a requirement key before -R. */
export const PROJECT_KEY_PATTERN = /^[A-Z][A-Z0-9]{1,9}$/;

/** The project key upper case, or null when it is not one. */
export function normalizeProjectKey(raw: unknown): string | null {
  if (typeof raw !== "string") return null;
  const key = raw.trim().toUpperCase();
  return PROJECT_KEY_PATTERN.test(key) ? key : null;
}

/** A requirement key as Exciter writes it; the server's allowlist holds the same. */
export const REQUIREMENT_KEY_PATTERN = /^([A-Z][A-Z0-9]{1,9})-R([1-9][0-9]*)$/;

/**
 * A key just typed, then a space or a sign: the key starts the text or
 * follows a space or an opening sign, so "aCHK-R1" stays text.
 */
export const TYPED_REQUIREMENT_KEY = /(?:^|[\s([{"'])([A-Z][A-Z0-9]{1,9}-R[1-9][0-9]*)([\s.,;:!?)\]}"'])$/;

/** The key upper case, or null when it is not one. */
export function normalizeRequirementKey(raw: unknown): string | null {
  if (typeof raw !== "string") return null;
  const key = raw.trim().toUpperCase();
  return key.length <= EXCITER_KEY_MAX_LENGTH && REQUIREMENT_KEY_PATTERN.test(key) ? key : null;
}

/** The project part of a key: CHK of CHK-R42. */
export function requirementProject(key: string): string {
  return key.slice(0, key.lastIndexOf("-R"));
}

/** Where a requirement opens in the Exciter at baseUrl. */
export function requirementUrl(baseUrl: string, key: string, projectKey = requirementProject(key)): string {
  return `${baseUrl.replace(/\/+$/, "")}/p/${encodeURIComponent(projectKey)}/r/${encodeURIComponent(key)}`;
}

/** Every key a document's requirement chips and blocks name, once each, in reading order. */
export function requirementKeysOf(nodes: DocNode | readonly DocNode[] | null | undefined): string[] {
  const found = new Set<string>();
  const walk = (node: DocNode) => {
    if (node.type === EXCITER_REQUIREMENT_NODE || node.type === EXCITER_REQUIREMENT_BLOCK_NODE) {
      const key = normalizeRequirementKey(node.attrs?.key);
      if (key) found.add(key);
    }
    for (const child of node.content ?? []) walk(child);
  };
  if (Array.isArray(nodes)) for (const node of nodes) walk(node);
  else if (nodes) walk(nodes as DocNode);
  return [...found];
}

/** Keys in batches the lookup takes, sorted so the same keys make the same requests. */
export function requirementBatches(keys: readonly string[]): string[][] {
  const sorted = [...new Set(keys)].sort();
  const out: string[][] = [];
  for (let i = 0; i < sorted.length; i += EXCITER_LOOKUP_MAX_KEYS) out.push(sorted.slice(i, i + EXCITER_LOOKUP_MAX_KEYS));
  return out;
}
