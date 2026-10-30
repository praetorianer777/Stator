import type { tableChartEn } from "./tableChart.en";

/** The German strings of the table chart, held to the English shape. */
export const tableChartDe: typeof tableChartEn = {
  kind: "Diagramm",
  kinds: { bar: "Balken", line: "Linien", pie: "Kreis" } as Record<string, string>,
  showTable: "Lesenden die Tabelle zeigen",
  chart: "Diagramm aus dieser Tabelle",
  unchart: "Diagramm entfernen, Tabelle behalten",
  deleteChart: "Diagramm samt Tabelle löschen",
  hint: "Die erste Zeile benennt jede Reihe, die erste Spalte jede Kategorie. Ändern Sie die Tabelle, folgt das Diagramm.",
  sample: {
    category: "Quartal",
    series: "Umsatz",
    rows: [
      ["Q1", "12"],
      ["Q2", "18"],
      ["Q3", "15"],
    ] as Array<[string, string]>,
  },
  title: (series: string[]) => series.join(", "),
  untitled: "Diagramm",
  summary: (kind: string, series: string[], categories: number) =>
    `${kind === "pie" ? "Kreisdiagramm" : kind === "line" ? "Liniendiagramm" : "Balkendiagramm"} von ${series.join(", ")} über ${categories === 1 ? "1 Kategorie" : `${categories} Kategorien`}.`,
  empty: "Tragen Sie Zahlen in die Tabelle ein: einen Namen je Zeile in der ersten Spalte und eine Überschrift je Reihe in der ersten Zeile.",
  emptyPie: "Ein Kreis braucht eine Zahl über null in der ersten Reihe der Tabelle.",
  pieFirst: (series: string) => `Der Kreis zeigt ${series}, die erste Reihe. Wählen Sie Balken oder Linien, um jede Reihe zu zeigen.`,
  dropped: (n: number) => `${n === 1 ? "1 weitere Reihe bleibt" : `${n} weitere Reihen bleiben`} außen vor: Ein Diagramm zeigt höchstens 8.`,
  showData: "Zahlen zeigen",
  category: "Kategorie",
  share: "Anteil",
  other: "Sonstige",
  none: "Keine",
};
