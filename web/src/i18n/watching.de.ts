import type { watchEn } from "./watching.en";

/** The German strings of watching, held to the English shape. */
export const watchDe: typeof watchEn = {
  button: "Beobachten",
  watching: "Beobachtet",
  menu: (title: string) => `${title} beobachten`,
  page: "Diese Seite beobachten",
  subtree: "Diese Seite und alle Seiten darunter beobachten",
  space: "Den ganzen Bereich beobachten",
  stopPage: "Diese Seite nicht mehr beobachten",
  stopSpace: "Den Bereich nicht mehr beobachten",
  watchers: "Wer beobachtet",
  coveredBySubtree: (title: string) => `Erfasst, weil Sie ${title} und die Seiten darunter beobachten`,
  coveredBySpace: "Erfasst, weil Sie den ganzen Bereich beobachten",
  selected: "(an)",
  watchersTitle: (title: string) => `Wer ${title} beobachtet`,
  watchersIntro: "Diese Personen erfahren von jeder neuen Version dieser Seite, die mit Benachrichtigung veröffentlicht wird.",
  watchersNone: "Noch beobachtet niemand diese Seite.",
  watchersMore: (shown: number, total: number) => `${shown} von ${total} angezeigt.`,
  via: {
    page: "Beobachtet diese Seite",
    subtree: (title: string) => `Beobachtet ${title} und die Seiten darunter`,
    space: "Beobachtet den ganzen Bereich",
  },
  failed: "Das Beobachten konnte nicht geändert werden. Versuchen Sie es gleich noch einmal.",
  listTitle: "Beobachtet",
  listIntro:
    "Die Seiten und Bereiche, die Sie beobachten. Sie erfahren von neuen Versionen, die mit Benachrichtigung veröffentlicht werden, und von Seiten, die darunter erstmals veröffentlicht werden.",
  listEmpty: "Sie beobachten noch nichts",
  listEmptyBody:
    "Öffnen Sie eine Seite oder einen Bereich und wählen Sie oben Beobachten. Seiten, die Sie anlegen und veröffentlichen, werden für Sie beobachtet, sofern Sie das nicht ausschalten.",
  columnWhat: "Seite oder Bereich",
  columnKind: "Umfasst",
  columnSince: "Seit",
  columnActions: "Aktionen",
  kind: { page: "Diese Seite", subtree: "Diese Seite und die Seiten darunter", space: "Den ganzen Bereich", blog: "Neue Beiträge im Blog" } as Record<
    "page" | "subtree" | "space" | "blog",
    string
  >,
  blogNamed: (name: string) => `Blog von ${name}`,
  stop: "Nicht mehr beobachten",
  stopNamed: (what: string) => `${what} nicht mehr beobachten`,
  stopped: (what: string) => `Sie beobachten ${what} nicht mehr.`,
  spaceNamed: (name: string) => `Bereich ${name}`,
  previous: "Zurück",
  next: "Weiter",
  range: (from: number, to: number, total: number) => `${from} bis ${to} von ${total}`,
};
