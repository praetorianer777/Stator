import { Mark } from "@tiptap/core";
import { INLINE_COMMENT_MARK } from "./schema";

/** The passage an inline thread is about, carried along with the text so a publish keeps it; the reader draws it. */
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
