import { useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { Contributors } from "@/features/contributors/Contributors";
import { ContributorsDialog } from "@/features/contributors/ContributorsDialog";
import { CONTRIBUTORS_NODE, contributorsSettings, type ContributorsSettings } from "@/features/contributors/contributors";
import { t } from "@/i18n";
import { ownEvent, settingsAttrs } from "./pageLists";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    contributors: {
      insertContributors: (settings: ContributorsSettings) => ReturnType;
    };
  }
}

function ContributorsView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = contributorsSettings(node.attrs);
  return (
    <NodeViewWrapper contentEditable={false} data-contributors-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <span className="min-w-0 flex-1 truncate text-xs">{t.contributors.title(settings.scope)}</span>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-contributors">
            {t.contributors.edit}
          </Button>
        </div>
        <Contributors settings={settings} />
      </div>
      {editing && (
        <ContributorsDialog
          initial={settings}
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

/** The people who published the page, or it and the pages below it. The page stores which and how many; each reader's view asks. */
export const ContributorsNode = Node.create({
  name: CONTRIBUTORS_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addAttributes() {
    return settingsAttrs(contributorsSettings);
  },
  parseHTML() {
    return [{ tag: "div[data-contributors-block]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-contributors-block": "" })];
  },
  // The people are not the page's words, so copied text leaves them out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(ContributorsView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      insertContributors:
        (settings) =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: contributorsSettings({ ...settings }) }),
    };
  },
});
