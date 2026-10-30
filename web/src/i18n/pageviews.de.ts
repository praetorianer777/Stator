import type { viewsEn } from "./pageviews.en";

/** The German strings of page views, held to the English shape. */
export const viewsDe: typeof viewsEn = {
  title: "Seitenaufrufe",
  views: (n: number, shown: string) => (n === 1 ? "1 Aufruf" : `${shown} Aufrufe`),
  people: (n: number, shown: string) => (n === 1 ? "1 Person" : `${shown} Personen`),
  open: (views: string, people: string) => `${views} von ${people}. Seitenaufrufe anzeigen.`,
  inAll: "Insgesamt",
  lately: (days: number) => `Letzte ${days} Tage`,
  howCounted: "Ein Aufruf zählt jede Person einmal am Tag, wie oft sie die Seite auch öffnet.",
  readers: "Wer sie gelesen hat",
  readersIntro: (days: number) => `Personen, die die Seite in den letzten ${days} Tagen geöffnet haben, die jüngsten zuerst.`,
  readersIntroForever: "Personen, die die Seite geöffnet haben, die jüngsten zuerst.",
  lastOpened: (day: string) => `Zuletzt geöffnet am ${day}`,
  onDays: (n: number) => (n === 1 ? "an 1 Tag" : `an ${n} Tagen`),
  unnamed: (n: number) => (n === 1 ? "1 weitere Person möchte nicht genannt werden." : `${n} weitere Personen möchten nicht genannt werden.`),
  noReaders: "In diesem Zeitraum hat niemand die Seite geöffnet.",
  more: "Mehr anzeigen",
  editorsOnly: "Wer die Seite gelesen hat, sehen nur Personen, die sie bearbeiten dürfen.",
  yourChoice: "Ob Ihr eigener Name erscheint, legen Sie in Ihrem Profil fest.",
  failed: "Die Seitenaufrufe konnten nicht geladen werden. Versuchen Sie es gleich noch einmal.",
};
