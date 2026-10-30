/** The English strings of the table chart. */
export const tableChartEn = {
  kind: "Chart",
  kinds: { bar: "Bars", line: "Lines", pie: "Pie" } as Record<string, string>,
  showTable: "Show the table to readers",
  chart: "Chart this table",
  unchart: "Remove the chart, keep the table",
  deleteChart: "Delete the chart and its table",
  hint: "The first row names each series and the first column each category. Edit the table and the chart follows.",
  sample: {
    category: "Quarter",
    series: "Sales",
    rows: [
      ["Q1", "12"],
      ["Q2", "18"],
      ["Q3", "15"],
    ] as Array<[string, string]>,
  },
  title: (series: string[]) => series.join(", "),
  untitled: "Chart",
  summary: (kind: string, series: string[], categories: number) =>
    `${kind === "pie" ? "Pie chart" : kind === "line" ? "Line chart" : "Bar chart"} of ${series.join(", ")} over ${categories === 1 ? "1 category" : `${categories} categories`}.`,
  empty: "Put numbers in the table to draw it: a name for each row in the first column, and a heading for each series in the first row.",
  emptyPie: "A pie needs a number above zero in the table's first series.",
  pieFirst: (series: string) => `The pie shows ${series}, the first series. Choose bars or lines to show every series.`,
  dropped: (n: number) => `${n === 1 ? "1 more series is" : `${n} more series are`} left out: a chart shows 8 at most.`,
  showData: "Show the numbers",
  category: "Category",
  share: "Share",
  other: "Other",
  none: "None",
};
