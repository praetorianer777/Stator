import type { contributorsEn } from "./contributors.en";

/** The German strings of the contributors list, held to the English shape. */
export const contributorsDe: typeof contributorsEn = {
  edit: "Liste bearbeiten",
  title: (scope: string) => (scope === "tree" ? "Mitwirkende an dieser Seite und den Seiten darunter" : "Mitwirkende an dieser Seite"),
  loading: "Es wird gesucht, wer veröffentlicht hat",
  failed: "Die Mitwirkenden konnten nicht aufgelistet werden. Laden Sie die Seite neu, um es noch einmal zu versuchen.",
  empty: "Hier hat noch niemand eine Version veröffentlicht.",
  unsaved: "Speichern Sie die Seite einmal, um aufzulisten, wer sie veröffentlicht hat.",
  meta: (edits: number, day: string) => `${edits === 1 ? "1 Version" : `${edits} Versionen`}, zuletzt am ${day}`,
  truncated: (n: number) => `Die ersten ${n} Personen werden gezeigt. Erhöhen Sie die Zahl in den Einstellungen der Liste, um mehr zu sehen.`,
  dialog: {
    title: "Mitwirkende",
    scope: "Zählen",
    scopes: { page: "Diese Seite", tree: "Diese Seite und die Seiten darunter" } as Record<string, string>,
    limit: "Anzeigen",
    limitChoice: (n: number) => `${n} Personen`,
    cancel: "Abbrechen",
    save: "Speichern",
  },
};
