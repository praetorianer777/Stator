import { Extension } from "@tiptap/core";
import { PluginKey } from "@tiptap/pm/state";
import Suggestion, { type SuggestionOptions } from "@tiptap/suggestion";
import { loadEmoji, matchEmoji, type Emoji } from "@/lib/emoji";

export { loadEmoji, matchEmoji, type Emoji };

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
