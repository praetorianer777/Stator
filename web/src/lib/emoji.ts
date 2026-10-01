import { EMOJI_COMMON, EMOJI_MAX_SUGGESTIONS } from "@/config";

// Apart from the editor's colon so a reaction can search the same list
// without the reader downloading the editor.

/** One emoji a colon can insert, with the names it is found by. */
export interface Emoji {
  emoji: string;
  /** Shortcodes, the first one the emoji's own. */
  names: string[];
  tags: string[];
  description: string;
}

let loading: Promise<Emoji[]> | null = null;

// The list is bundled, never fetched from elsewhere, and arrives with the
// first colon or reaction search, so a reader who does neither never
// downloads it.
export function loadEmoji(): Promise<Emoji[]> {
  loading ??= import("gemoji").then(({ gemoji }) => gemoji.map(({ emoji, names, tags, description }) => ({ emoji, names, tags, description })));
  return loading;
}

const NO_MATCH = Number.POSITIVE_INFINITY;

// Lower is better: the shortcode itself, then a shortcode it starts, then a
// word of a shortcode, a tag or the description it starts.
function rank(item: Emoji, q: string): number {
  if (item.names.includes(q)) return 0;
  if (item.names.some((name) => name.startsWith(q))) return 1;
  if (item.names.some((name) => name.split(/[_-]/).some((part) => part.startsWith(q)))) return 2;
  if (item.tags.some((tag) => tag.startsWith(q))) return 3;
  if (
    item.description
      .toLowerCase()
      .split(/\s+/)
      .some((word) => word.startsWith(q))
  )
    return 4;
  return NO_MATCH;
}

/** The emoji whose names start with what was typed after the colon, best first; the common ones before anything is typed. */
export function matchEmoji(all: Emoji[], query: string, limit: number = EMOJI_MAX_SUGGESTIONS): Emoji[] {
  const q = query.trim().toLowerCase();
  if (!q) {
    return EMOJI_COMMON.flatMap((name) => all.find((item) => item.names.includes(name)) ?? []).slice(0, limit);
  }
  return all
    .map((item, index) => ({ item, index, score: rank(item, q) }))
    .filter((found) => found.score !== NO_MATCH)
    .sort((a, b) => a.score - b.score || a.index - b.index)
    .slice(0, limit)
    .map((found) => found.item);
}
