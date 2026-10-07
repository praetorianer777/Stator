import { useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { IssueChart } from "@/features/armature/IssueChart";
import { IssueChartDialog } from "@/features/armature/IssueChartDialog";
import { ARMATURE_CHART_NODE, chartSettings, type ChartSettings } from "@/features/armature/chart";
import { t } from "@/i18n";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    armatureChart: {
      /** Opens the settings dialog for a new chart; the dialog inserts it. */
      pickArmatureChart: () => ReturnType;
      insertArmatureChart: (settings: ChartSettings) => ReturnType;
    };
  }
}

export interface ArmatureChartOptions {
  /** Opens the settings dialog for a new chart; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// The chart's own controls: ProseMirror would otherwise take a click on them
// as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a, button, svg, details, tr") !== null;

function ChartView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = chartSettings(node.attrs);
  return (
    <NodeViewWrapper contentEditable={false} data-armature-chart-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <code className="min-w-0 flex-1 truncate text-xs" title={settings.query}>
            {settings.project}: {settings.query}
          </code>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-issue-chart">
            {t.armature.chart.edit}
          </Button>
        </div>
        <IssueChart settings={settings} inEditor />
      </div>
      {editing && (
        <IssueChartDialog
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
 * A chart of the issues an NQL query matches in one project. The page stores
 * what to count and how to draw it; each reader's view asks Armature for counts.
 */
export const ArmatureChart = Node.create<ArmatureChartOptions>({
  name: ARMATURE_CHART_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    const read = (el: HTMLElement) =>
      chartSettings({
        project: el.getAttribute("data-project"),
        query: el.getAttribute("data-query"),
        chart: el.getAttribute("data-chart"),
        groupBy: el.getAttribute("data-group-by"),
        days: Number(el.getAttribute("data-days")),
      });
    const fresh = chartSettings({});
    return {
      project: { default: fresh.project, parseHTML: (el) => read(el).project, renderHTML: (attrs) => ({ "data-project": attrs.project }) },
      query: { default: fresh.query, parseHTML: (el) => read(el).query, renderHTML: (attrs) => ({ "data-query": attrs.query }) },
      chart: { default: fresh.chart, parseHTML: (el) => read(el).chart, renderHTML: (attrs) => ({ "data-chart": attrs.chart }) },
      groupBy: { default: fresh.groupBy, parseHTML: (el) => read(el).groupBy, renderHTML: (attrs) => ({ "data-group-by": attrs.groupBy }) },
      days: { default: fresh.days, parseHTML: (el) => read(el).days, renderHTML: (attrs) => ({ "data-days": attrs.days }) },
    };
  },
  parseHTML() {
    return [
      {
        tag: "div[data-armature-chart]",
        getAttrs: (el) => (chartSettings({ project: (el as HTMLElement).getAttribute("data-project") }).project ? null : false),
      },
    ];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-armature-chart": "" })];
  },
  // Its counts are not the page's words, so copied text leaves it out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(ChartView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickArmatureChart: () => () => {
        this.options.pick?.();
        return true;
      },
      insertArmatureChart:
        (settings) =>
        ({ commands }) => {
          const clean = chartSettings({ ...settings });
          return clean.project && clean.query.trim() ? commands.insertContent({ type: this.name, attrs: clean }) : false;
        },
    };
  },
});
