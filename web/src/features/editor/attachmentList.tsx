import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { AttachmentList } from "@/features/attachments/AttachmentList";

export const ATTACHMENT_LIST_NODE = "attachmentList";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    attachmentList: {
      insertAttachmentList: () => ReturnType;
    };
  }
}

export interface AttachmentListOptions {
  /** The page being edited, whose files the list shows; none before it is saved. */
  pageId: string | undefined;
}

// The list's own controls: ProseMirror would otherwise take a click on them
// as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a, button, input, summary") !== null;

function AttachmentListView({ extension }: NodeViewProps) {
  const { pageId } = extension.options as AttachmentListOptions;
  return (
    <NodeViewWrapper contentEditable={false} data-attachment-list-node="">
      <div className="doc-block">
        {/* Whoever edits the page may put files on it. */}
        <AttachmentList pageId={pageId} editable />
      </div>
    </NodeViewWrapper>
  );
}

/** The page's own files, the latest version of each name, uploaded to from the block. It stores nothing. */
export const AttachmentListNode = Node.create<AttachmentListOptions>({
  name: ATTACHMENT_LIST_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pageId: undefined };
  },
  parseHTML() {
    return [{ tag: "div[data-attachment-list]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-attachment-list": "" })];
  },
  // The files are no words of the page, so copied text leaves them out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(AttachmentListView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      insertAttachmentList:
        () =>
        ({ commands }) =>
          commands.insertContent({ type: this.name }),
    };
  },
});
