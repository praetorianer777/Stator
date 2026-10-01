import { Extension } from "@tiptap/core";
import { PluginKey } from "@tiptap/pm/state";
import Suggestion, { type SuggestionOptions } from "@tiptap/suggestion";
import { EMOJI_COMMON, EMOJI_MAX_SUGGESTIONS } from "@/config";

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
// first colon so a reader who never edits never downloads it.
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

export interface EmojiOptions {
  suggestion: Omit<SuggestionOptions<Emoji, Emoji>, "editor">;
}

export const emojiSuggestionKey = new PluginKey("emojiSuggestion");

/**
 * A colon at the start of a word offers emoji by name; the one picked goes
 * in as text, so it reads, copies and is found like any other character.
 */
export const EmojiSuggestion = Extension.create<EmojiOptions>({
  name: "emojiSuggestion",
  addOptions() {
    return {
      suggestion: {
        char: ":",
        pluginKey: emojiSuggestionKey,
        allow: ({ state, range }) => {
          const $from = state.doc.resolve(range.from);
          return !$from.parent.type.spec.code && !$from.marks().some((mark) => mark.type.spec.code);
        },
        items: async ({ query }) => matchEmoji(await loadEmoji(), query),
        command: ({ editor, range, props }) => {
          editor.chain().focus().insertContentAt(range, props.emoji).run();
        },
      },
    };
  },
  addProseMirrorPlugins() {
    return [Suggestion({ editor: this.editor, ...this.options.suggestion })];
  },
});
