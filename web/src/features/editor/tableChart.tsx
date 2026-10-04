import { useId } from "react";
import { Node, mergeAttributes, type JSONContent } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import { NodeViewContent, NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button, SelectInput } from "@/components/ui";
import { TABLE_CHARTS } from "@/config";
import { TableChart } from "@/features/tableChart/TableChart";
import { TABLE_CHART_NODE, tableChartSettings, type TableChartKind } from "@/features/tableChart/data";
import { t } from "@/i18n";
import type { DocNode } from "./schema";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    tableChart: {
      /** A chart of a small table to fill in, so there is something to see at once. */
      insertTableChart: () => ReturnType;
      /** Draws the table the caret is in as a chart, which keeps the table. */
      chartTable: () => ReturnType;
      /** Takes the chart away from the table the caret is in, leaving the table. */
      unchartTable: () => ReturnType;
    };
  }
}

const cell = (type: "tableHeader" | "tableCell", text: string): JSONContent => ({
  type,
  content: [{ type: "paragraph", content: text ? [{ type: "text", text }] : [] }],
});

/** The table a new chart starts with: a heading row and a few rows of numbers to replace. */
export function sampleTable(): JSONContent {
  const s = t.tableChart.sample;
  return {
    type: "table",
    content: [
      { type: "tableRow", content: [cell("tableHeader", s.category), cell("tableHeader", s.series)] },
      ...s.rows.map(([name, value]) => ({ type: "tableRow", content: [cell("tableCell", name), cell("tableCell", value)] })),
    ],
  };
}

// The settings bar's own controls: ProseMirror would otherwise take a click on them as editing.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("[data-block-settings]") !== null;

function TableChartView({ node, updateAttributes, editor, getPos }: NodeViewProps) {
  const c = t.tableChart;
  const id = useId();
  const settings = tableChartSettings(node.attrs);
  const table = (node.toJSON() as DocNode).content?.[0];
  return (
    <NodeViewWrapper data-table-chart-node="">
      <div className="doc-block">
        <div className="doc-block-settings" contentEditable={false} data-block-settings>
          <label htmlFor={`${id}-kind`} className="text-xs">
            {c.kind}
          </label>
          <SelectInput
            id={`${id}-kind`}
            controlSize="sm"
            value={settings.chart}
            onChange={(event) => updateAttributes({ chart: event.target.value as TableChartKind })}
            data-action="table-chart-kind"
          >
            {TABLE_CHARTS.map((kind) => (
              <option key={kind} value={kind}>
                {c.kinds[kind]}
              </option>
            ))}
          </SelectInput>
          <label className="flex min-w-0 flex-1 items-center gap-1.5 text-xs">
            <input
              type="checkbox"
              className="accent-accent"
              checked={settings.showTable}
              onChange={(event) => updateAttributes({ showTable: event.target.checked })}
            />
            {c.showTable}
          </label>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              const pos = getPos();
              if (typeof pos === "number")
                editor
                  .chain()
                  .focus()
                  .setTextSelection(pos + 5)
                  .unchartTable()
                  .run();
            }}
            data-action="unchart-table"
          >
            {c.unchart}
          </Button>
        </div>
        <div contentEditable={false}>
          <TableChart table={table} kind={settings.chart} dataTable={false} />
        </div>
        <p className="doc-page-list-meta" contentEditable={false}>
          {c.hint}
        </p>
        <NodeViewContent className="doc-table-chart-table" />
      </div>
    </NodeViewWrapper>
  );
}

/** A table and the chart drawn from it. The chart is never stored: it is the table's, drawn afresh. */
export const TableChartNode = Node.create({
  name: TABLE_CHART_NODE,
  group: "block",
  content: "table",
  isolating: true,
  draggable: true,
  addAttributes() {
    return {
      chart: {
        default: "bar",
        parseHTML: (el: HTMLElement) => tableChartSettings({ chart: el.getAttribute("data-chart") }).chart,
        renderHTML: (attrs: Record<string, unknown>) => ({ "data-chart": attrs.chart }),
      },
      showTable: {
        default: true,
        parseHTML: (el: HTMLElement) => el.getAttribute("data-show-table") !== "false",
        renderHTML: (attrs: Record<string, unknown>) => ({ "data-show-table": String(attrs.showTable) }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-table-chart]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-table-chart": "" }), 0];
  },
  addNodeView() {
    return ReactNodeViewRenderer(TableChartView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      insertTableChart:
        () =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: { chart: "bar", showTable: true }, content: [sampleTable()] }),
      chartTable:
        () =>
        ({ state, tr, dispatch }) => {
          const { $from } = state.selection;
          for (let depth = $from.depth; depth > 0; depth--) {
            if ($from.node(depth).type.name !== "table") continue;
            if ($from.node(depth - 1).type.name === this.name) return false;
            const start = $from.before(depth);
            const chart = this.type.create({ chart: "bar", showTable: true }, $from.node(depth));
            if (dispatch) {
              tr.replaceWith(start, start + $from.node(depth).nodeSize, chart);
              // The caret stays where it was in the table, one step deeper now.
              dispatch(tr.setSelection(TextSelection.near(tr.doc.resolve(state.selection.from + 1))));
            }
            return true;
          }
          return false;
        },
      unchartTable:
        () =>
        ({ state, tr, dispatch }) => {
          const { $from } = state.selection;
          for (let depth = $from.depth; depth > 0; depth--) {
            const node = $from.node(depth);
            if (node.type.name !== this.name) continue;
            const start = $from.before(depth);
            if (dispatch) {
              tr.replaceWith(start, start + node.nodeSize, node.content);
              dispatch(tr.setSelection(TextSelection.near(tr.doc.resolve(state.selection.from - 1))));
            }
            return true;
          }
          return false;
        },
    };
  },
});
