import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { t } from "@/i18n";
import { DocView } from "./DocView";
import { INCLUDE_NODE, IncludeBlock, IncludeChain, includeId } from "./IncludeViews";
import { PassagesContext } from "./passages";
import type { Doc } from "./schema";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    include: {
      /** Opens the picker the slash menu's include asks what to include with. */
      pickInclude: () => ReturnType;
      /** Puts an include of a page, or one excerpt of it, where the caret is. */
      insertInclude: (target: { pageId: string; excerptId: string | null }) => ReturnType;
    };
  }
}

export interface IncludeOptions {
  /** Opens the picker; without it the slash menu's include does nothing. */
  pick: (() => void) | undefined;
  /** The page being edited, so an include that leads back to it is caught. */
  pageId: string | undefined;
}

// The source link is the block's own: ProseMirror would otherwise take a
// click on it as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a") !== null;

function drawIncluded(doc: Doc) {
  return (
    <PassagesContext value={null}>
      <DocView doc={doc} anchors={false} />
    </PassagesContext>
  );
}

// The author sees what readers will, framed as not theirs to edit here: the
// words belong to the page they come from.
function IncludeNodeView({ node, extension }: NodeViewProps) {
  const pageId = includeId(node.attrs.pageId);
  const own = (extension.options as IncludeOptions).pageId;
  return (
    <NodeViewWrapper className="doc-include-edit" data-include-edit="" contentEditable={false} title={t.editor.include.notHere}>
      {pageId && (
        <IncludeChain value={own ? [own] : []}>
          <IncludeBlock pageId={pageId} excerptId={includeId(node.attrs.excerptId)} draw={drawIncluded} />
        </IncludeChain>
      )}
    </NodeViewWrapper>
  );
}

/** Another page, or one excerpt of it, shown in this one as each reader may read it. */
export const Include = Node.create<IncludeOptions>({
  name: INCLUDE_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined, pageId: undefined };
  },
  addAttributes() {
    return {
      pageId: {
        default: null,
        parseHTML: (el) => includeId(el.getAttribute("data-include-page")),
        renderHTML: (attrs) => ({ "data-include-page": attrs.pageId }),
      },
      excerptId: {
        default: null,
        parseHTML: (el) => includeId(el.getAttribute("data-include-excerpt")),
        renderHTML: (attrs) => (attrs.excerptId ? { "data-include-excerpt": attrs.excerptId } : {}),
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-include-page]", getAttrs: (el) => (includeId((el as HTMLElement).getAttribute("data-include-page")) ? null : false) }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes)];
  },
  addNodeView() {
    return ReactNodeViewRenderer(IncludeNodeView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickInclude: () => () => {
        this.options.pick?.();
        return true;
      },
      insertInclude:
        ({ pageId, excerptId }) =>
        ({ commands }) => {
          const page = includeId(pageId);
          // A page that includes itself would show itself over and over.
          if (!page || page === this.options.pageId) return false;
          return commands.insertContent({ type: this.name, attrs: { pageId: page, excerptId: includeId(excerptId) } });
        },
    };
  },
});
