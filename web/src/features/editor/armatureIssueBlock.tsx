import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { IssueBlock } from "@/features/armature/IssueBlock";
import { ARMATURE_ISSUE_BLOCK_NODE, normalizeKey } from "@/features/armature/issueKeys";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    armatureIssueBlock: {
      /** Opens the picker that asks which issue; the picker inserts it. */
      pickArmatureIssue: () => ReturnType;
      insertArmatureIssueBlock: (key: string) => ReturnType;
    };
  }
}

export interface ArmatureIssueBlockOptions {
  /** Opens the issue picker; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// The card's links are its own: ProseMirror would otherwise take a click on
// them as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a") !== null;

function IssueBlockView({ node }: NodeViewProps) {
  return (
    <NodeViewWrapper contentEditable={false} data-armature-issue-block-node="">
      <IssueBlock issueKey={String(node.attrs.key ?? "")} />
    </NodeViewWrapper>
  );
}

/**
 * One Armature issue as a card: the page stores its key alone, and each
 * reader's view looks the issue up as them.
 */
export const ArmatureIssueBlock = Node.create<ArmatureIssueBlockOptions>({
  name: ARMATURE_ISSUE_BLOCK_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    return {
      key: {
        default: null,
        parseHTML: (el) => normalizeKey(el.getAttribute("data-armature-issue-block")),
        renderHTML: (attrs) => ({ "data-armature-issue-block": attrs.key }),
      },
    };
  },
  parseHTML() {
    return [
      { tag: "div[data-armature-issue-block]", getAttrs: (el) => (normalizeKey((el as HTMLElement).getAttribute("data-armature-issue-block")) ? null : false) },
    ];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes), String(node.attrs.key ?? "")];
  },
  renderText({ node }) {
    return String(node.attrs.key ?? "");
  },
  addNodeView() {
    return ReactNodeViewRenderer(IssueBlockView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickArmatureIssue: () => () => {
        this.options.pick?.();
        return true;
      },
      insertArmatureIssueBlock:
        (key) =>
        ({ commands }) => {
          const normalized = normalizeKey(key);
          return normalized ? commands.insertContent({ type: this.name, attrs: { key: normalized } }) : false;
        },
    };
  },
});
