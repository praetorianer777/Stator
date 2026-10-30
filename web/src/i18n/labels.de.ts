import type { labelsEn } from "./labels.en";

/** The German strings of labels, held to the English shape. */
export const labelsDe: typeof labelsEn = {
  title: "Schlagwörter",
  add: "Schlagwort hinzufügen",
  placeholder: "Schlagwort eingeben",
  options: "Schlagwörter zur Auswahl",
  create: (name: string) => `${name} hinzufügen`,
  remove: (name: string) => `Schlagwort ${name} entfernen`,
  added: (name: string) => `Schlagwort ${name} hinzugefügt.`,
  removed: (name: string) => `Schlagwort ${name} entfernt.`,
  loading: "Schlagwörter werden gesucht",
  noMatch: "Noch kein Schlagwort beginnt so.",
  invalid: "Verwenden Sie Buchstaben, Ziffern, Bindestriche, Unterstriche und Punkte, und beginnen Sie mit einem Buchstaben oder einer Ziffer.",
  tooLong: (max: number) => `Ein Schlagwort darf höchstens ${max} Zeichen haben.`,
  used: (pages: number) => (pages === 1 ? "auf 1 Seite" : `auf ${pages} Seiten`),
  heading: (name: string) => `Schlagwort ${name}`,
  inSpace: (space: string) => `Seiten in ${space} mit diesem Schlagwort`,
  everywhere: "Seiten in allen Bereichen mit diesem Schlagwort",
  showEverywhere: "Alle Bereiche zeigen",
  count: (total: number, from: number, to: number) =>
    total === 0 ? "Keine Seiten" : total <= to && from === 1 ? (total === 1 ? "1 Seite" : `${total} Seiten`) : `${from} bis ${to} von ${total} Seiten`,
  emptyTitle: "Keine Seite trägt dieses Schlagwort",
  emptyBody: "Vergeben Sie das Schlagwort auf der Seite selbst, oder prüfen Sie die Schreibweise.",
  failed: "Die Seiten mit diesem Schlagwort konnten nicht geladen werden. Versuchen Sie es gleich noch einmal.",
  changed: (who: string, when: string) => (who ? `${who} hat sie am ${when} geändert` : `Geändert am ${when}`),
  unpublished: "Unveröffentlicht",
  paging: "Seiten dieser Liste",
  previous: "Zurück",
  next: "Weiter",
  pageOf: (page: number, pages: number) => `Seite ${page} von ${pages}`,
};
