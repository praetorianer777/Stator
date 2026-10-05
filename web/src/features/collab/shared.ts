import type * as Y from "yjs";
import { COLLAB_COLORS } from "@/config";

/** Where a shared draft keeps what, so every browser reads the same names. */
export const BODY_FIELD = "body";
export const TITLE_FIELD = "title";
const META = "meta";
const SEEDED = "seeded";

export function titleOf(doc: Y.Doc): Y.Text {
  return doc.getText(TITLE_FIELD);
}

/** Whether a shared draft has had its first content, which the editor waits for. */
export function isSeeded(doc: Y.Doc): boolean {
  return doc.getMap(META).get(SEEDED) === true;
}

export function markSeeded(doc: Y.Doc) {
  doc.getMap(META).set(SEEDED, true);
}

/**
 * Makes the shared title read next, as one deletion and one insertion
 * between what both share at the start and at the end, so a change made at
 * the same time elsewhere in the title survives.
 */
export function setSharedTitle(text: Y.Text, next: string) {
  const was = text.toString();
  if (was === next) return;
  let start = 0;
  while (start < was.length && start < next.length && was[start] === next[start]) start++;
  let end = 0;
  while (end < was.length - start && end < next.length - start && was[was.length - 1 - end] === next[next.length - 1 - end]) end++;
  text.doc?.transact(() => {
    if (was.length - start - end > 0) text.delete(start, was.length - start - end);
    const inserted = next.slice(start, next.length - end);
    if (inserted) text.insert(start, inserted);
  });
}

/** A person's colour for their caret and their avatar, the same in every browser. */
export function colorFor(userId: string): string {
  let hash = 0;
  for (const ch of userId) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0;
  return COLLAB_COLORS[hash % COLLAB_COLORS.length] as string;
}

/** Who is in a shared draft, as awareness says, once each. */
export interface Collaborator {
  id: string;
  name: string;
  color: string;
}

export function collaboratorsOf(states: Map<number, Record<string, unknown>>, selfClient: number, selfId: string): Collaborator[] {
  const seen = new Map<string, Collaborator>();
  for (const [client, state] of states) {
    if (client === selfClient) continue;
    const user = state.user as Partial<Collaborator> | undefined;
    if (!user?.id || user.id === selfId || seen.has(user.id)) continue;
    seen.set(user.id, { id: user.id, name: String(user.name ?? ""), color: String(user.color ?? "") });
  }
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
}
