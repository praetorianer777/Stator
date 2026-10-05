import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { Editor, type JSONContent } from "@tiptap/core";
import { TABLE_CHARTS } from "@/config";
import { editorExtensions } from "@/features/editor/extensions";
import type { DocNode } from "@/features/editor/schema";
import { axeViolations } from "@/test/axe";
import { allowlist, problems } from "@/test/allowlist";
import { TableChart } from "./TableChart";
import { axisTicks, cellText, chartData, parseNumber, tableChartSettings } from "./data";

let editor: Editor | undefined;
afterEach(() => {
  cleanup();
  editor?.destroy();
  editor = undefined;
});

const cell = (type: string, text: string): DocNode => ({ type, content: [{ type: "paragraph", content: text ? [{ type: "text", text }] : [] }] });
function table(...rows: string[][]): DocNode {
  return {
    type: "table",
    content: rows.map((row, r) => ({ type: "tableRow", content: row.map((text) => cell(r === 0 ? "tableHeader" : "tableCell", text)) })),
  };
}
const sales = table(["Quarter", "North", "South", "Notes"], ["Q1", "12", "about 3", "slow"], ["Q2", "18", "", ""], ["Q3", "-4", "7", "fine"]);

describe("reading a table", () => {
  it("reads numbers as people write them, and names as names", () => {
    const cases: Array<[string, number | null]> = [
      ["12", 12],
      ["1,234", 1234],
      ["1.234", 1234],
      ["1,234.5", 1234.5],
      ["1.234,5", 1234.5],
      ["0,5", 0.5],
      ["-4", -4],
      ["−7", -7],
      ["12 %", 12],
      ["€ 3.50", 3.5],
      ["3.50€", 3.5],
      ["5 kg", 5],
      ["1'000", 1000],
      ["Q1", null],
      ["v2", null],
      ["", null],
      ["n/a", null],
      ["1.2.3", null],
    ];
    for (const [raw, want] of cases) expect(parseNumber(raw), raw).toBe(want);
  });

  it("takes the first row as series and the first column as categories, leaving out columns without numbers", () => {
    const data = chartData(sales);
    expect(data.categories).toEqual(["Q1", "Q2", "Q3"]);
    expect(data.series).toEqual([
      { name: "North", values: [12, 18, -4] },
      { name: "South", values: [null, null, 7] },
    ]);
    expect(chartData(undefined)).toEqual({ categories: [], series: [], dropped: 0 });
    const wide = table(["x", ...Array.from({ length: 10 }, (_, i) => `s${i}`)], ["a", ...Array.from({ length: 10 }, () => "1")]);
    expect(chartData(wide).series).toHaveLength(8);
    expect(chartData(wide).dropped).toBe(2);
    expect(cellText({ type: "tableCell", content: [{ type: "paragraph", content: [{ type: "date", attrs: { date: "2026-10-01" } }] }] })).toBe("2026-10-01");
  });

  it("puts ticks round and around zero", () => {
    expect(axisTicks(0, 18, 4)).toEqual([0, 5, 10, 15, 20]);
    expect(axisTicks(-4, 18, 4)).toEqual([-10, 0, 10, 20]);
    expect(axisTicks(0.1, 0.3, 4)).toEqual([0, 0.1, 0.2, 0.3]);
    expect(axisTicks(0, 0, 4)).toEqual([0, 1]);
  });

  it("puts right what the server would refuse, as the allowlist says", () => {
    expect(tableChartSettings({ chart: "donut", showTable: "yes" })).toEqual({ chart: "bar", showTable: true });
    expect(tableChartSettings({ chart: "pie", showTable: false })).toEqual({ chart: "pie", showTable: false });
    expect(allowlist.nodes.tableChart?.attrs?.chart?.enum).toEqual([...TABLE_CHARTS]);
    expect(allowlist.nodes.tableChart?.content).toEqual(["table"]);
  });
});

describe("the chart", () => {
  it("draws bars for each series with a legend, reads a category out by keyboard, and offers the numbers", async () => {
    render(
      <main>
        <h1>Sales</h1>
        <TableChart table={sales} kind="bar" />
      </main>,
    );
    const figure = screen.getByRole("figure");
    expect(within(figure).getByText("North, South", { selector: "figcaption" })).toBeInTheDocument();
    const plot = within(figure).getByRole("img", { name: "Bar chart of North, South over 3 categories." });
    expect(figure.querySelectorAll("[data-bar]")).toHaveLength(4);
    expect(
      within(figure)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["North", "South"]);
    plot.focus();
    fireEvent.keyDown(plot, { key: "ArrowRight" });
    expect(within(figure).getByRole("status")).toHaveTextContent(/Q1.*12.*North.*None.*South/);
    expect(within(figure).getByText("Show the numbers")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("breaks a line where a cell holds no number", () => {
    render(<TableChart table={sales} kind="line" />);
    expect(document.querySelectorAll('polyline[data-series="North"]')).toHaveLength(1);
    // South has one number, so one dot and no line.
    expect(document.querySelectorAll('polyline[data-series="South"]')).toHaveLength(0);
  });

  it("draws the first series as a pie of what is above zero, and says the rest is left out", () => {
    render(<TableChart table={sales} kind="pie" />);
    expect([...document.querySelectorAll("[data-chart-slice]")].map((p) => p.getAttribute("data-chart-slice"))).toEqual(["Q1", "Q2"]);
    expect(screen.getByText(/The pie shows North, the first series/)).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Pie chart of North over 3 categories." })).toBeInTheDocument();
  });

  it("says what to put in a table without numbers", () => {
    render(<TableChart table={table(["Name", "Role"], ["Ada", "Lead"])} kind="bar" />);
    expect(screen.getByRole("figure")).toHaveAttribute("data-state", "empty");
    expect(screen.getByText(/Put numbers in the table to draw it/)).toBeInTheDocument();
  });
});

describe("in the editor", () => {
  it("charts the table the caret is in, keeps it valid, and takes the chart away again", async () => {
    const content: JSONContent = { type: "doc", content: [table(["Quarter", "Sales"], ["Q1", "3"]) as JSONContent] };
    editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions({}), content });
    await new Promise((resolve) => setTimeout(resolve));
    editor.commands.setTextSelection(4);
    expect(editor.commands.chartTable()).toBe(true);
    const charted = editor.getJSON() as DocNode;
    expect(charted.content?.[0]?.type).toBe("tableChart");
    expect(charted.content?.[0]?.content?.[0]?.type).toBe("table");
    expect(problems(charted)).toEqual([]);
    // A chart's table is charted already.
    expect(editor.can().chartTable()).toBe(false);
    expect(editor.state.selection.$from.parent.textContent).toBe("Quarter");
    expect(editor.commands.unchartTable()).toBe(true);
    expect((editor.getJSON() as DocNode).content?.[0]?.type).toBe("table");
  });
});
