import { useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { EXCITER_LIST_DEFAULT_LIMIT } from "@/config";
import { RequirementList, requirementListSettings, type RequirementListSettings } from "@/features/exciter/RequirementList";
import { RequirementListDialog } from "@/features/exciter/RequirementListDialog";
import { EXCITER_REQUIREMENT_LIST_NODE, normalizeProjectKey } from "@/features/exciter/requirementKeys";
import { t } from "@/i18n";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    exciterRequirementList: {
      /** Opens the settings dialog for a new list; the dialog inserts it. */
      pickExciterRequirementList: () => ReturnType;
      insertExciterRequirementList: (settings: RequirementListSettings) => ReturnType;
    };
  }
}

export interface ExciterRequirementListOptions {
  /** Opens the settings dialog for a new list; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// The list's own controls: ProseMirror would otherwise take a click on them
// as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a, button") !== null;

function RequirementListView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = requirementListSettings(node.attrs);
  return (
    <NodeViewWrapper contentEditable={false} data-exciter-requirement-list-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <code className="min-w-0 flex-1 truncate text-xs" title={settings.query}>
            {settings.query ? `${settings.project}: ${settings.query}` : settings.project}
          </code>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-requirement-list">
            {t.exciter.list.edit}
          </Button>
        </div>
        <RequirementList settings={settings} inEditor />
      </div>
      {editing && (
        <RequirementListDialog
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
 * A table of the requirements a query in Exciter's language finds in one
 * project. The page stores the project, the query and the most rows; each
 * reader's view asks Exciter for the rows, so currentUser() is the reader.
 */
export const ExciterRequirementList = Node.create<ExciterRequirementListOptions>({
  name: EXCITER_REQUIREMENT_LIST_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    const read = (el: HTMLElement) =>
      requirementListSettings({ project: el.getAttribute("data-project"), query: el.getAttribute("data-query"), limit: Number(el.getAttribute("data-limit")) });
    return {
      project: { default: "", parseHTML: (el) => read(el).project, renderHTML: (attrs) => ({ "data-project": attrs.project }) },
      query: { default: "", parseHTML: (el) => read(el).query, renderHTML: (attrs) => ({ "data-query": attrs.query }) },
      limit: { default: EXCITER_LIST_DEFAULT_LIMIT, parseHTML: (el) => read(el).limit, renderHTML: (attrs) => ({ "data-limit": attrs.limit }) },
    };
  },
  parseHTML() {
    return [
      { tag: "div[data-exciter-requirement-list]", getAttrs: (el) => (normalizeProjectKey((el as HTMLElement).getAttribute("data-project")) ? null : false) },
    ];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-exciter-requirement-list": "" })];
  },
  // Its rows are not the page's words, so copied text leaves it out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(RequirementListView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickExciterRequirementList: () => () => {
        this.options.pick?.();
        return true;
      },
      insertExciterRequirementList:
        (settings) =>
        ({ commands }) =>
          normalizeProjectKey(settings.project) ? commands.insertContent({ type: this.name, attrs: requirementListSettings({ ...settings }) }) : false,
    };
  },
});
