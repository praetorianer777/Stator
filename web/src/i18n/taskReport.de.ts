import type { taskReportEn } from "./taskReport.en";

/** The German strings of the task report, held to the English shape. */
export const taskReportDe: typeof taskReportEn = {
  edit: "Bericht bearbeiten",
  loading: "Die Aufgaben werden gesucht",
  failed: "Die Aufgaben konnten nicht aufgelistet werden. Laden Sie die Seite neu, um es erneut zu versuchen.",
  empty: "Keine Aufgabe auf einer Seite, die Sie lesen dürfen, passt zu diesem Bericht.",
  nobody: "Niemandem zugewiesen",
  truncated: (n: number) => `Die ersten ${n} Aufgaben werden gezeigt. Grenzen Sie den Bericht ein, um die übrigen zu sehen.`,
  title: (s: { space: string | null; assignee: string | null; due: string; state: string }, name: string) =>
    [
      `${s.state === "done" ? "Erledigte Aufgaben" : s.state === "all" ? "Aufgaben" : "Offene Aufgaben"}${s.space ? ` in ${s.space}` : ""}`,
      s.assignee === "me" ? "Ihnen zugewiesen" : s.assignee === "none" ? "niemandem zugewiesen" : s.assignee ? `${name || "einer Person"} zugewiesen` : "",
      ({ overdue: "überfällig", today: "heute fällig", week: "in den nächsten 7 Tagen fällig", none: "ohne Fälligkeitsdatum" } as Record<string, string>)[
        s.due
      ] ?? "",
    ]
      .filter(Boolean)
      .join(", "),
  dialog: {
    titleNew: "Aufgabenbericht einfügen",
    titleEdit: "Aufgabenbericht bearbeiten",
    space: "Bereich",
    everySpace: "Alle Bereiche",
    assignee: "Zugewiesen an",
    anybody: "Beliebige Person",
    reader: "Wer die Seite liest",
    nobody: "Niemanden",
    somebodyGone: "Eine Person, die gegangen ist",
    findPerson: "Person suchen",
    morePeople: "Es passen mehr Personen, als aufgeführt sind; tippen Sie mehr von einem Namen ein, um sie zu finden.",
    due: "Fällig",
    dues: { any: "An jedem Tag oder nie", overdue: "Überfällig", today: "Heute", week: "In den nächsten 7 Tagen", none: "Ohne Fälligkeitsdatum" } as Record<
      string,
      string
    >,
    state: "Status",
    states: { open: "Offen", done: "Erledigt", all: "Offen und erledigt" } as Record<string, string>,
    limit: "Anzeigen",
    limitChoice: (n: number) => `${n} Aufgaben`,
    cancel: "Abbrechen",
    insert: "Einfügen",
    save: "Speichern",
  },
};
