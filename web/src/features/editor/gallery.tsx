import { useContext, useState, useSyncExternalStore } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { attachmentUrl } from "@/api/attachments";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { Gallery } from "@/features/gallery/Gallery";
import { GalleryDialog } from "@/features/gallery/GalleryDialog";
import { GALLERY_IMAGE_NODE, GALLERY_NODE, galleryCaption, galleryColumns, galleryJSON, galleryOf, type GallerySettings } from "@/features/gallery/gallery";
import { t } from "@/i18n";
import type { AttachmentIndex } from "./attachmentIndex";
import { ATTACHMENT_ID_PATTERN } from "./attachments";
import { DocPageContext } from "./BlockViews";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    gallery: {
      /** Opens the dialog for a new gallery; the dialog inserts it. */
      pickGallery: () => ReturnType;
      insertGallery: (settings: GallerySettings) => ReturnType;
    };
  }
}

export interface GalleryOptions {
  /** Opens the dialog for a new gallery; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
  /** Which files the page still has, so a deleted picture is drawn as a gap. */
  index: AttachmentIndex | undefined;
}

// The gallery's own controls: ProseMirror would otherwise take a click on
// them as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("button") !== null;

const noIndex = () => () => {};

function GalleryView({ node, extension, editor, getPos }: NodeViewProps) {
  const { index } = extension.options as GalleryOptions;
  const known = useSyncExternalStore(index?.subscribe ?? noIndex, () => index?.known());
  const page = useContext(DocPageContext);
  const [editing, setEditing] = useState(false);
  const settings = galleryOf(node.toJSON());
  return (
    <NodeViewWrapper contentEditable={false} data-gallery-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <span className="min-w-0 flex-1 truncate text-xs">{t.gallery.summary(settings.pictures.length, settings.columns)}</span>
          {editor.isEditable && (
            <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-gallery">
              {t.gallery.edit}
            </Button>
          )}
        </div>
        <Gallery settings={settings} url={attachmentUrl} known={known} editing />
      </div>
      {editing && (
        <GalleryDialog
          initial={settings}
          pageId={page?.id}
          isNew={false}
          onClose={() => setEditing(false)}
          onSave={(next) => {
            setEditing(false);
            const json = galleryJSON(next);
            const pos = getPos();
            // The dialog held the page still, but a gallery no longer where it was opened is left as it is.
            if (!json || typeof pos !== "number" || editor.state.doc.nodeAt(pos)?.type.name !== GALLERY_NODE) return;
            const fresh = editor.schema.nodeFromJSON(json);
            editor
              .chain()
              .focus()
              .command(({ tr }) => {
                tr.replaceWith(pos, pos + (editor.state.doc.nodeAt(pos)?.nodeSize ?? 0), fresh);
                return true;
              })
              .setNodeSelection(pos)
              .run();
          }}
        />
      )}
    </NodeViewWrapper>
  );
}

/** One picture of a gallery: a file of the page by id, with its caption. Only a gallery holds it. */
export const GalleryImage = Node.create({
  name: GALLERY_IMAGE_NODE,
  atom: true,
  selectable: false,
  addAttributes() {
    return {
      attachmentId: {
        default: null,
        parseHTML: (el) => {
          const id = el.getAttribute("data-attachment-id");
          return id && ATTACHMENT_ID_PATTERN.test(id) ? id : null;
        },
        renderHTML: () => ({}),
      },
      caption: { default: null, parseHTML: (el) => galleryCaption(el.getAttribute("alt")), renderHTML: () => ({}) },
    };
  },
  parseHTML() {
    return [{ tag: "img[data-gallery-image]", getAttrs: (el) => (ATTACHMENT_ID_PATTERN.test(el.getAttribute("data-attachment-id") ?? "") ? null : false) }];
  },
  renderHTML({ node, HTMLAttributes }) {
    const id = String(node.attrs.attachmentId);
    return [
      "img",
      mergeAttributes(HTMLAttributes, { src: attachmentUrl(id, true), alt: node.attrs.caption ?? "", "data-gallery-image": "", "data-attachment-id": id }),
    ];
  },
});

/** Pictures of the page side by side, which open in the lightbox; it stores which, in order, with captions. */
export const GalleryNode = Node.create<GalleryOptions>({
  name: GALLERY_NODE,
  group: "block",
  content: `${GALLERY_IMAGE_NODE}+`,
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined, index: undefined };
  },
  addAttributes() {
    return {
      columns: {
        default: galleryColumns(undefined),
        parseHTML: (el) => galleryColumns(Number(el.getAttribute("data-gallery-columns"))),
        // Not data-columns, which a column layout reads as its own.
        renderHTML: (attrs) => ({ "data-gallery-columns": attrs.columns }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-gallery-block]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-gallery-block": "" }), 0];
  },
  // The captions are the pictures' words, which copied text keeps.
  renderText({ node }) {
    return galleryOf(node.toJSON())
      .pictures.flatMap((p) => (p.caption ? [p.caption] : []))
      .join("\n");
  },
  addNodeView() {
    return ReactNodeViewRenderer(GalleryView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickGallery: () => () => {
        this.options.pick?.();
        return true;
      },
      insertGallery:
        (settings) =>
        ({ commands }) => {
          const json = galleryJSON(settings);
          return json ? commands.insertContent(json) : false;
        },
    };
  },
});
