import { useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PageListDialog } from "@/features/pageLists/PageListDialog";
import { LabelledPages, UpdatedPages } from "@/features/pageLists/PageLists";
import {
  LABELLED_PAGES_NODE,
  RECENTLY_UPDATED_NODE,
  labelledSettings,
  updatedSettings,
  type LabelledSettings,
  type UpdatedSettings,
} from "@/features/pageLists/lists";
import { t } from "@/i18n";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    pageLists: {
      /** Opens the settings dialog for a new content by label list; the dialog inserts it. */
      pickLabelledPages: () => ReturnType;
      insertLabelledPages: (settings: LabelledSettings) => ReturnType;
      insertRecentlyUpdated: (settings: UpdatedSettings) => ReturnType;
    };
  }
}

export interface PageListOptions {
  /** Opens the settings dialog for a new list; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// The list's own controls: ProseMirror would otherwise take a click on them
// as selecting the block.
export const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a, button") !== null;

/**
 * Every attribute travels as JSON in data-settings, as a list of labels does
 * not fit one HTML attribute otherwise; read reads it back put right.
 */
export function settingsAttrs<T extends object>(read: (attrs: Record<string, unknown>) => T) {
  const fresh = read({});
  const fromHTML = (el: HTMLElement): Record<string, unknown> => {
    try {
      const parsed: unknown = JSON.parse(el.getAttribute("data-settings") ?? "{}");
      return parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : {};
    } catch {
      return {};
    }
  };
  return Object.fromEntries(
    Object.entries(fresh).map(([name, value], i) => [
      name,
      {
        default: value,
        parseHTML: (el: HTMLElement) => read(fromHTML(el))[name as keyof T],
        // One attribute carries them all; the first writes it.
        renderHTML: (attrs: Record<string, unknown>) => (i === 0 ? { "data-settings": JSON.stringify(read(attrs)) } : {}),
      },
    ]),
  );
}

function LabelledView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = labelledSettings(node.attrs);
  return (
    <NodeViewWrapper contentEditable={false} data-labelled-pages-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <span className="min-w-0 flex-1 truncate text-xs">{t.pageLists.labelledTitle(settings.labels, settings.match, settings.space)}</span>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-page-list">
            {t.pageLists.edit}
          </Button>
        </div>
        <LabelledPages settings={settings} inEditor />
      </div>
      {editing && (
        <PageListDialog
          kind="labelled"
          initial={settings}
          isNew={false}
          onClose={() => setEditing(false)}
          onSave={(next) => {
            setEditing(false);
            updateAttributes(next);
          }}
        />
      )}
    </NodeViewWrapper>
  );
}

function UpdatedView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = updatedSettings(node.attrs);
  return (
    <NodeViewWrapper contentEditable={false} data-recently-updated-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <span className="min-w-0 flex-1 truncate text-xs">{t.pageLists.updatedTitle(settings.space)}</span>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-page-list">
            {t.pageLists.edit}
          </Button>
        </div>
        <UpdatedPages settings={settings} inEditor />
      </div>
      {editing && (
        <PageListDialog
          kind="updated"
          initial={settings}
          isNew={false}
          onClose={() => setEditing(false)}
          onSave={(next) => {
            setEditing(false);
            updateAttributes(next);
          }}
        />
      )}
    </NodeViewWrapper>
  );
}

const listBlock = { group: "block", atom: true, selectable: true, draggable: true } as const;

/** The published pages carrying some labels. The page stores what to list; each reader's view asks for the pages they may read. */
export const LabelledPagesNode = Node.create<PageListOptions>({
  name: LABELLED_PAGES_NODE,
  ...listBlock,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    return settingsAttrs(labelledSettings);
  },
  parseHTML() {
    return [{ tag: "div[data-labelled-pages]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-labelled-pages": "" })];
  },
  // Its pages are not the page's words, so copied text leaves it out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(LabelledView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickLabelledPages: () => () => {
        this.options.pick?.();
        return true;
      },
      insertLabelledPages:
        (settings) =>
        ({ commands }) => {
          const clean = labelledSettings({ ...settings });
          return clean.labels.length > 0 ? commands.insertContent({ type: this.name, attrs: clean }) : false;
        },
    };
  },
});

/** The pages published last, in a space or anywhere, as each reader may read them. */
export const RecentlyUpdatedNode = Node.create({
  name: RECENTLY_UPDATED_NODE,
  ...listBlock,
  addAttributes() {
    return settingsAttrs(updatedSettings);
  },
  parseHTML() {
    return [{ tag: "div[data-recently-updated]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-recently-updated": "" })];
  },
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(UpdatedView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      insertRecentlyUpdated:
        (settings) =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: updatedSettings({ ...settings }) }),
    };
  },
});
