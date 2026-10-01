import { useEffect, useRef, type KeyboardEvent } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewContent, NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Icon } from "@/components/icons";
import { EXPAND_TITLE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    expand: {
      /** Wraps the selected blocks in an expand block and puts the caret in its title. */
      setExpand: () => ReturnType;
      /** Takes the expand block away and keeps what was inside it. */
      unsetExpand: () => ReturnType;
    };
  }
  interface Storage {
    expand: ExpandStorage;
  }
}

export interface ExpandStorage {
  /** Set by setExpand so the block it makes takes the caret into its title. */
  focusTitle: boolean;
}

function clampTitle(value: unknown): string {
  return typeof value === "string" ? [...value].slice(0, EXPAND_TITLE_MAX_LENGTH).join("") : "";
}

// The title box is the block's own: ProseMirror would otherwise take its keys
// and clicks as editing the document around it.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("[data-expand-title]") !== null;

function ExpandNodeView({ node, editor, getPos, updateAttributes }: NodeViewProps) {
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const storage = editor.storage.expand;
    if (!storage.focusTitle) return;
    // A frame later than the focus the inserting command scheduled for the
    // editor, which would otherwise take the caret back.
    const frame = requestAnimationFrame(() => {
      storage.focusTitle = false;
      input.current?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [editor]);
  // Enter and the down arrow carry on into the blocks inside, as from a heading.
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if ((event.key !== "Enter" && event.key !== "ArrowDown") || event.nativeEvent.isComposing) return;
    const pos = getPos();
    if (typeof pos !== "number") return;
    event.preventDefault();
    editor.commands.setTextSelection(pos + 2);
    // At once rather than through the focus command, which waits a frame and
    // would leave the next key typed to the title.
    editor.view.focus();
  };
  return (
    <NodeViewWrapper className="doc-expand" data-expand="" data-expanded="true">
      <div className="doc-expand-head" contentEditable={false}>
        <Icon.ChevronDown className="doc-expand-chevron" />
        <input
          ref={input}
          type="text"
          className="doc-expand-title"
          aria-label={t.editor.expand.title}
          placeholder={t.editor.expand.titlePlaceholder}
          maxLength={EXPAND_TITLE_MAX_LENGTH}
          value={String(node.attrs.title ?? "")}
          readOnly={!editor.isEditable}
          onChange={(event) => updateAttributes({ title: clampTitle(event.target.value) })}
          onKeyDown={onKeyDown}
          data-expand-title=""
        />
      </div>
      <NodeViewContent className="doc-expand-body" data-expand-body="" />
    </NodeViewWrapper>
  );
}

/** A titled section of blocks that a reader's view shows closed until they open it. */
export const Expand = Node.create<Record<string, never>, ExpandStorage>({
  name: "expand",
  group: "block",
  content: "block+",
  defining: true,
  addStorage() {
    return { focusTitle: false };
  },
  addAttributes() {
    return {
      title: {
        default: "",
        parseHTML: (el) => clampTitle(el.getAttribute("data-title") ?? el.querySelector(":scope > [data-expand-toggle]")?.textContent ?? ""),
        renderHTML: (attrs) => ({ "data-title": attrs.title }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-expand]", contentElement: (el: HTMLElement) => el.querySelector<HTMLElement>(":scope > [data-expand-body]") ?? el }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-expand": "" }), ["div", { "data-expand-body": "" }, 0]];
  },
  addNodeView() {
    return ReactNodeViewRenderer(ExpandNodeView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      setExpand:
        () =>
        ({ commands, dispatch, editor }) => {
          const done = commands.wrapIn(this.name, { title: "" });
          if (done && dispatch) editor.storage.expand.focusTitle = true;
          return done;
        },
      unsetExpand:
        () =>
        ({ commands }) =>
          commands.lift(this.name),
    };
  },
});
