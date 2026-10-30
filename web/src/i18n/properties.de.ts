import type { propertiesEn } from "./properties.en";

/** The German strings of page properties and their report, held to the English shape. */
export const propertiesDe: typeof propertiesEn = {
  report: {
    edit: "Bericht bearbeiten",
    loading: "Die Eigenschaften der Seiten werden gesammelt",
    failed: "Der Bericht konnte nicht gelesen werden. Laden Sie die Seite neu, um es erneut zu versuchen.",
    empty: (labels: string[]) =>
      `Keine veröffentlichte Seite, die Sie lesen dürfen, trägt ${labels.length === 1 ? "das Schlagwort" : "alle Schlagwörter"} ${labels.join(", ")}.`,
    page: "Seite",
    title: (labels: string[], space: string | null) => `Seiten mit ${labels.join(", ")}${space ? ` in ${space}` : ""}`,
    caption: (labels: string[], n: number) =>
      `${n === 1 ? "1 Seite" : `${n} Seiten`} mit ${labels.join(", ")} und ihren Eigenschaften. Wählen Sie die Überschrift einer Spalte, um danach zu sortieren.`,
    summary: (labels: string[], space: string | null) => `Eigenschaftenbericht der Seiten mit ${labels.join(", ")}${space ? ` in ${space}` : ""}`,
    truncated: "Nur die ersten 200 Seiten sind aufgeführt; fügen Sie ein Schlagwort hinzu oder wählen Sie einen Bereich, um den Bericht einzugrenzen.",
  },
  dialog: {
    titleNew: "Eigenschaftenbericht einfügen",
    titleEdit: "Eigenschaftenbericht bearbeiten",
    labels: "Schlagwörter",
    labelsHint: "Trennen Sie Schlagwörter mit Kommas. Der Bericht listet die Seiten, die alle tragen.",
    labelsNone: "Nennen Sie mindestens ein Schlagwort, nach dem Seiten gesammelt werden.",
    labelsMany: (n: number) => `Nennen Sie höchstens ${n} Schlagwörter.`,
    labelBad: (raw: string) => `"${raw}" kann kein Schlagwort sein. Verwenden Sie Buchstaben, Ziffern, Bindestriche, Unterstriche und Punkte.`,
    space: "Bereich",
    everySpace: "Alle Bereiche",
    columns: "Angezeigte Eigenschaften",
    columnsHint: "Ein Name pro Zeile, in der Reihenfolge der Spalten. Leer lassen, um jede Eigenschaft der Seiten zu zeigen.",
    columnLong: (n: number) => `Halten Sie jeden Namen bei höchstens ${n} Zeichen.`,
    columnsMany: (n: number) => `Zeigen Sie höchstens ${n} Eigenschaften.`,
    cancel: "Abbrechen",
    insert: "Einfügen",
    save: "Speichern",
  },
};
