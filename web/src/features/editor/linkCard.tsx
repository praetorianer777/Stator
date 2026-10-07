import { Node, mergeAttributes, type Editor } from "@tiptap/core";
import type { NodeType } from "@tiptap/pm/model";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { fetchLinkPreview, useLinkPreview } from "@/api/linkPreview";
import { LINK_CARD_VIEWS } from "@/config";
import { t } from "@/i18n";
import { LINK_CARD_NODE, LinkCard as LinkCardDrawing, linkCardView, shortAddress, webAddress, type LinkCardView } from "./LinkCardViews";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    linkCard: {
      /** Opens the dialog the slash menu's link preview asks for an address with. */
      pickLinkCard: () => ReturnType;
      /** Puts a card for an address where the caret is, and makes it the site's player when there is one. */
      insertLinkCard: (url: string) => ReturnType;
    };
  }
}

export interface LinkCardOptions {
  /** Opens the dialog that asks for an address; without it the slash menu's item does nothing. */
  pick: (() => void) | undefined;
}

// Toolbar buttons are the card's own: ProseMirror would otherwise take a
// click on one as selecting the card.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("button, a, iframe") !== null;

/**
 * A card for an address of a site that has a player becomes the player, as
 * a pasted video link is meant to be watched. The change is not a step of
 * its own to undo: the author asked for the link, and this is how it shows.
 */
function embedWhenItCan(editor: Editor, type: NodeType, url: string) {
  void fetchLinkPreview(url)
    .then((preview) => {
      if (!preview.embed || editor.isDestroyed) return;
      let found = -1;
      editor.state.doc.descendants((node, pos) => {
        if (found < 0 && node.type === type && node.attrs.url === url && node.attrs.view === "card") found = pos;
        return found < 0;
      });
      if (found < 0) return;
      const tr = editor.state.tr.setNodeMarkup(found, undefined, { url, view: "embed" }).setMeta("addToHistory", false);
      editor.view.dispatch(tr);
    })
    .catch(() => undefined);
}

function LinkCardNodeView({ node, editor, getPos }: NodeViewProps) {
  const url = webAddress(node.attrs.url) ?? "";
  const view = linkCardView(node.attrs.view);
  const { data: preview } = useLinkPreview(url);
  const show = (next: (typeof LINK_CARD_VIEWS)[number]) => {
    const pos = getPos();
    if (typeof pos !== "number") return;
    if (next !== "inline") {
      editor.chain().focus().setNodeSelection(pos).updateAttributes(LINK_CARD_NODE, { view: next }).run();
      return;
    }
    // Inline, the link reads as its page's title in a line of its own.
    const text = preview?.title || shortAddress(url);
    editor
      .chain()
      .focus()
      .insertContentAt(
        { from: pos, to: pos + node.nodeSize },
        { type: "paragraph", content: [{ type: "text", text, marks: [{ type: "link", attrs: { href: url } }] }] },
      )
      .run();
  };
  const current: (typeof LINK_CARD_VIEWS)[number] = view === "embed" && preview?.embed ? "embed" : "card";
  return (
    <NodeViewWrapper className="doc-link-card-edit" data-link-card-edit="" contentEditable={false}>
      <div role="group" aria-label={t.editor.linkCard.viewLabel} className="doc-link-card-views" data-link-card-views="">
        {LINK_CARD_VIEWS.map((option) => (
          <button
            key={option}
            type="button"
            aria-pressed={current === option}
            disabled={!editor.isEditable || (option === "embed" && !preview?.embed)}
            title={option === "embed" && preview && !preview.embed ? t.editor.linkCard.noEmbed : undefined}
            onClick={() => show(option)}
            data-link-card-view={option}
          >
            {t.editor.linkCard.views[option]}
          </button>
        ))}
        <a href={url} target="_blank" rel="noopener noreferrer nofollow" className="doc-link-card-open">
          {t.editor.linkCard.open}
        </a>
      </div>
      <LinkCardDrawing url={url} view={view} links={false} />
    </NodeViewWrapper>
  );
}

/** A link shown as a card, or as its site's player, holding only its address and its view. */
export const LinkCardNode = Node.create<LinkCardOptions>({
  name: LINK_CARD_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    return {
      url: {
        default: null,
        parseHTML: (el) => webAddress(el.getAttribute("data-url")),
        renderHTML: (attrs) => ({ "data-url": attrs.url }),
      },
      view: {
        default: "card",
        parseHTML: (el) => linkCardView(el.getAttribute("data-view")),
        renderHTML: (attrs) => ({ "data-view": attrs.view }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-link-card-node][data-url]", getAttrs: (el) => (webAddress((el as HTMLElement).getAttribute("data-url")) ? null : false) }];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-link-card-node": "" }), ["a", { href: String(node.attrs.url ?? "") }, String(node.attrs.url ?? "")]];
  },
  renderText({ node }) {
    return String(node.attrs.url ?? "");
  },
  addNodeView() {
    return ReactNodeViewRenderer(LinkCardNodeView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickLinkCard: () => () => {
        this.options.pick?.();
        return true;
      },
      insertLinkCard:
        (raw) =>
        ({ commands, dispatch }) => {
          const url = webAddress(raw);
          if (!url || !commands.insertContent({ type: this.name, attrs: { url, view: "card" as LinkCardView } })) return false;
          if (dispatch) embedWhenItCan(this.editor, this.type, url);
          return true;
        },
    };
  },
  addProseMirrorPlugins() {
    const editor = this.editor;
    const type = this.type;
    return [
      new Plugin({
        key: new PluginKey("linkCardPaste"),
        props: {
          // An address pasted alone on an empty line is meant to be shown,
          // not read: it becomes a card. Anywhere else it stays a link.
          handlePaste: (view, event) => {
            const url = webAddress(event.clipboardData?.getData("text/plain"));
            const { $from, empty } = view.state.selection;
            if (!url || !empty || $from.parent.type.name !== "paragraph" || $from.parent.content.size > 0 || $from.depth < 1) return false;
            const container = $from.node($from.depth - 1);
            const index = $from.index($from.depth - 1);
            if (!container.canReplaceWith(index, index + 1, type)) return false;
            view.dispatch(view.state.tr.replaceWith($from.before(), $from.after(), type.create({ url, view: "card" })).scrollIntoView());
            embedWhenItCan(editor, type, url);
            return true;
          },
        },
      }),
    ];
  },
});
