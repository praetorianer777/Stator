import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { RequirementBlock } from "@/features/exciter/RequirementBlock";
import { EXCITER_REQUIREMENT_BLOCK_NODE, normalizeRequirementKey } from "@/features/exciter/requirementKeys";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    exciterRequirementBlock: {
      /** Opens the picker that asks which requirement; the picker inserts it. */
      pickExciterRequirementBlock: () => ReturnType;
      insertExciterRequirementBlock: (key: string) => ReturnType;
    };
  }
}

export interface ExciterRequirementBlockOptions {
  /** Opens the requirement picker; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// The block's links are its own: ProseMirror would otherwise take a click on
// them as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a") !== null;

function RequirementBlockView({ node }: NodeViewProps) {
  return (
    <NodeViewWrapper contentEditable={false} data-exciter-requirement-block-node="">
      <RequirementBlock requirementKey={String(node.attrs.key ?? "")} links={false} />
    </NodeViewWrapper>
  );
}

/**
 * One Exciter requirement with its statement and rationale: the page stores
 * its key alone, and each reader's view reads it as them.
 */
export const ExciterRequirementBlock = Node.create<ExciterRequirementBlockOptions>({
  name: EXCITER_REQUIREMENT_BLOCK_NODE,
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
        parseHTML: (el) => normalizeRequirementKey(el.getAttribute("data-exciter-requirement-block")),
        renderHTML: (attrs) => ({ "data-exciter-requirement-block": attrs.key }),
      },
    };
  },
  parseHTML() {
    return [
      {
        tag: "div[data-exciter-requirement-block]",
        getAttrs: (el) => (normalizeRequirementKey((el as HTMLElement).getAttribute("data-exciter-requirement-block")) ? null : false),
      },
    ];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes), String(node.attrs.key ?? "")];
  },
  renderText({ node }) {
    return String(node.attrs.key ?? "");
  },
  addNodeView() {
    return ReactNodeViewRenderer(RequirementBlockView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickExciterRequirementBlock: () => () => {
        this.options.pick?.();
        return true;
      },
      insertExciterRequirementBlock:
        (key) =>
        ({ commands }) => {
          const normalized = normalizeRequirementKey(key);
          return normalized ? commands.insertContent({ type: this.name, attrs: { key: normalized } }) : false;
        },
    };
  },
});
