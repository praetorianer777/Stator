import { useContext, useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TemplateButton } from "@/features/templateButton/TemplateButton";
import { TemplateButtonDialog } from "@/features/templateButton/TemplateButtonDialog";
import { TEMPLATE_BUTTON_NODE, templateButtonSettings, type TemplateButtonSettings } from "@/features/templateButton/button";
import { t } from "@/i18n";
import { DocPageContext } from "./BlockViews";
import { ownEvent, settingsAttrs } from "./pageLists";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    templateButton: {
      /** Opens the settings dialog for a new template button; the dialog inserts it. */
      pickTemplateButton: () => ReturnType;
      insertTemplateButton: (settings: TemplateButtonSettings) => ReturnType;
    };
  }
}

export interface TemplateButtonOptions {
  /** Opens the settings dialog for a new button; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

function TemplateButtonView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = templateButtonSettings(node.attrs);
  const page = useContext(DocPageContext);
  return (
    <NodeViewWrapper contentEditable={false} data-template-button-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <span className="min-w-0 flex-1 truncate text-xs">{t.templateButton.summary(settings.template)}</span>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-template-button">
            {t.templateButton.edit}
          </Button>
        </div>
        <TemplateButton settings={settings} inEditor />
      </div>
      {editing && (
        <TemplateButtonDialog
          initial={settings}
          pageSpace={page?.spaceKey ?? null}
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

/** A button that makes a page from a template. The page stores which template and where; each reader's view asks whether they may. */
export const TemplateButtonNode = Node.create<TemplateButtonOptions>({
  name: TEMPLATE_BUTTON_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    return settingsAttrs(templateButtonSettings);
  },
  parseHTML() {
    return [{ tag: "div[data-template-button-block]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-template-button-block": "" })];
  },
  // A button is no words of the page, so copied text leaves it out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(TemplateButtonView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickTemplateButton: () => () => {
        this.options.pick?.();
        return true;
      },
      insertTemplateButton:
        (settings) =>
        ({ commands }) => {
          const clean = templateButtonSettings({ ...settings });
          return clean.template ? commands.insertContent({ type: this.name, attrs: clean }) : false;
        },
    };
  },
});
