import type { decisionsEn } from "./decisions.en";

/** The German strings of the decision log, held to the English shape. */
export const decisionsDe: typeof decisionsEn = {
  title: "Entscheidungen",
  filter: "Anzeigen",
  all: "Alle",
  decided: "Entschieden",
  undecided: "Offen",
  empty: "Hier gibt es noch keine Entscheidungen. Fügen Sie einer Seite eine über Entscheidung im Schrägstrich-Menü hinzu.",
  emptyFiltered: "Keine Entscheidungen in diesem Zustand.",
  onPage: (title: string, when: string) => `auf ${title}, ${when}`,
  truncated: "Nur die neuesten Entscheidungen sind aufgeführt. Grenzen Sie die Liste nach Zustand ein.",
};
