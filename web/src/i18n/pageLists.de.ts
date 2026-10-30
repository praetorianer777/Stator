import type { pageListsEn } from "./pageLists.en";

/** The German strings of the page list macros, held to the English shape. */
export const pageListsDe: typeof pageListsEn = {
  edit: "Liste bearbeiten",
  loading: "Die Seiten werden gesucht",
  failed: "Die Seiten konnten nicht aufgelistet werden. Laden Sie die Seite neu, um es erneut zu versuchen.",
  labelledTitle: (labels: string[], match: string, space: string | null) =>
    `Seiten mit ${labels.join(match === "any" ? " oder " : " und ")}${space ? ` in ${space}` : ""}`,
  labelledEmpty: "Noch keine veröffentlichte Seite, die Sie lesen dürfen, trägt diese Schlagwörter.",
  updatedTitle: (space: string | null) => (space ? `Zuletzt aktualisiert in ${space}` : "Zuletzt aktualisiert"),
  updatedEmpty: "Noch ist nichts veröffentlicht, das Sie lesen dürfen.",
  by: (name: string) => `von ${name}`,
  dialog: {
    labelledNew: "Liste von Seiten nach Schlagwort einfügen",
    labelledEdit: "Liste von Seiten nach Schlagwort bearbeiten",
    updatedNew: "Liste zuletzt aktualisierter Seiten einfügen",
    updatedEdit: "Liste zuletzt aktualisierter Seiten bearbeiten",
    labels: "Schlagwörter",
    labelsHint: "Trennen Sie Schlagwörter mit Kommas.",
    labelsNone: "Nennen Sie mindestens ein Schlagwort, nach dem Seiten aufgelistet werden.",
    match: "Seiten mit",
    matches: { all: "Allen Schlagwörtern", any: "Einem der Schlagwörter" } as Record<string, string>,
    space: "Bereich",
    everySpace: "Alle Bereiche",
    sort: "Reihenfolge",
    sorts: { updated: "Zuletzt aktualisierte zuerst", title: "Nach Titel" } as Record<string, string>,
    limit: "Anzeigen",
    limitChoice: (n: number) => `${n} Seiten`,
    cancel: "Abbrechen",
    insert: "Einfügen",
    save: "Speichern",
  },
};
