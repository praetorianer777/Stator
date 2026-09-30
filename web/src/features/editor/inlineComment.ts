import { Mark } from "@tiptap/core";
import { INLINE_COMMENT_MARK } from "./schema";

/**
 * The passage an inline thread is about. The editor only carries it along
 * with the text it edits, so a publish keeps each thread on its words; it
 * draws nothing, since the reader is where threads are read.
 */
export const InlineComment = Mark.create({
  name: INLINE_COMMENT_MARK,
  // Two threads may cover the same words, and typing at a passage's edge
  // does not make it longer.
  excludes: "",
  inclusive: false,
  addAttributes() {
    return {
      threadId: {
        default: null,
        parseHTML: (el) => el.getAttribute("data-inline-comment"),
        renderHTML: (attrs) => ({ "data-inline-comment": attrs.threadId as string }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "span[data-inline-comment]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["span", HTMLAttributes, 0];
  },
});
