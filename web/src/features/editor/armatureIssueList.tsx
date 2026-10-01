import { useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ARMATURE_DEFAULT_COLUMNS, ARMATURE_LIST_DEFAULT_LIMIT } from "@/config";
import { IssueList, listSettings, type IssueListSettings } from "@/features/armature/IssueList";
import { IssueListDialog } from "@/features/armature/IssueListDialog";
import { ARMATURE_ISSUE_LIST_NODE } from "@/features/armature/issueKeys";
import { t } from "@/i18n";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    armatureIssueList: {
      /** Opens the settings dialog for a new list; the dialog inserts it. */
      pickArmatureIssueList: () => ReturnType;
      insertArmatureIssueList: (settings: IssueListSettings) => ReturnType;
    };
  }
}

export interface ArmatureIssueListOptions {
  /** Opens the settings dialog for a new list; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// The list's own controls: ProseMirror would otherwise take a click on them
// as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a, button") !== null;

function IssueListView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = listSettings(node.attrs);
  return (
    <NodeViewWrapper contentEditable={false} data-armature-issue-list-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <code className="min-w-0 flex-1 truncate text-xs" title={settings.query}>
            {settings.query}
          </code>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-issue-list">
            {t.armature.list.edit}
          </Button>
        </div>
        <IssueList settings={settings} inEditor />
      </div>
      {editing && (
        <IssueListDialog
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

/**
 * A table of the issues an NQL query matches. The page stores the query, the
 * columns and the most rows; each reader's view asks Armature for the rows.
 */
export const ArmatureIssueList = Node.create<ArmatureIssueListOptions>({
  name: ARMATURE_ISSUE_LIST_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    const read = (el: HTMLElement) =>
      listSettings({
        query: el.getAttribute("data-query"),
        columns: (el.getAttribute("data-columns") ?? "").split(","),
        limit: Number(el.getAttribute("data-limit")),
      });
    return {
      query: { default: "", parseHTML: (el) => read(el).query, renderHTML: (attrs) => ({ "data-query": attrs.query }) },
      columns: {
        default: [...ARMATURE_DEFAULT_COLUMNS],
        parseHTML: (el) => read(el).columns,
        renderHTML: (attrs) => ({ "data-columns": (attrs.columns ?? []).join(",") }),
      },
      limit: { default: ARMATURE_LIST_DEFAULT_LIMIT, parseHTML: (el) => read(el).limit, renderHTML: (attrs) => ({ "data-limit": attrs.limit }) },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-armature-issue-list]", getAttrs: (el) => ((el as HTMLElement).getAttribute("data-query")?.trim() ? null : false) }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-armature-issue-list": "" })];
  },
  // Its rows are not the page's words, so copied text leaves it out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(IssueListView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickArmatureIssueList: () => () => {
        this.options.pick?.();
        return true;
      },
      insertArmatureIssueList:
        (settings) =>
        ({ commands }) =>
          settings.query.trim() ? commands.insertContent({ type: this.name, attrs: listSettings({ ...settings }) }) : false,
    };
  },
});
