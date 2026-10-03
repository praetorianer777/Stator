import { useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PropertiesReport } from "@/features/properties/PropertiesReport";
import { PropertiesReportDialog } from "@/features/properties/PropertiesReportDialog";
import { PROPERTIES_REPORT_NODE, reportSettings, type ReportSettings } from "@/features/properties/report";
import { drawInline } from "./DocView";
import { t } from "@/i18n";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    propertiesReport: {
      /** Opens the settings dialog for a new report; the dialog inserts it. */
      pickPropertiesReport: () => ReturnType;
      insertPropertiesReport: (settings: ReportSettings) => ReturnType;
    };
  }
}

export interface PropertiesReportOptions {
  /** Opens the settings dialog for a new report; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// Lists travel as JSON in one attribute each, as a column's name may hold a comma.
function listAttr(el: HTMLElement, name: string): unknown[] {
  try {
    const parsed: unknown = JSON.parse(el.getAttribute(name) ?? "[]");
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

const read = (el: HTMLElement) =>
  reportSettings({ labels: listAttr(el, "data-labels"), space: el.getAttribute("data-space"), columns: listAttr(el, "data-columns") });

// The report's own controls: ProseMirror would otherwise take a click on them
// as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a, button") !== null;

function ReportView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = reportSettings(node.attrs);
  return (
    <NodeViewWrapper contentEditable={false} data-properties-report-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <span className="min-w-0 flex-1 truncate text-xs">{t.properties.report.summary(settings.labels, settings.space)}</span>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-properties-report">
            {t.properties.report.edit}
          </Button>
        </div>
        <PropertiesReport settings={settings} draw={drawInline} inEditor />
      </div>
      {editing && (
        <PropertiesReportDialog
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
 * A register of the pages carrying some labels and their properties. The page
 * stores what to gather; each reader's view asks for the pages they may read.
 */
export const PropertiesReportNode = Node.create<PropertiesReportOptions>({
  name: PROPERTIES_REPORT_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    return {
      labels: { default: [], parseHTML: (el) => read(el).labels, renderHTML: (attrs) => ({ "data-labels": JSON.stringify(attrs.labels) }) },
      space: { default: null, parseHTML: (el) => read(el).space, renderHTML: (attrs) => (attrs.space ? { "data-space": attrs.space } : {}) },
      columns: { default: [], parseHTML: (el) => read(el).columns, renderHTML: (attrs) => ({ "data-columns": JSON.stringify(attrs.columns) }) },
    };
  },
  parseHTML() {
    return [
      {
        tag: "div[data-properties-report]",
        getAttrs: (el) => (read(el as HTMLElement).labels.length > 0 ? null : false),
      },
    ];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-properties-report": "" })];
  },
  // Its rows are not the page's words, so copied text leaves it out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(ReportView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickPropertiesReport: () => () => {
        this.options.pick?.();
        return true;
      },
      insertPropertiesReport:
        (settings) =>
        ({ commands }) => {
          const clean = reportSettings({ ...settings });
          return clean.labels.length > 0 ? commands.insertContent({ type: this.name, attrs: clean }) : false;
        },
    };
  },
});
