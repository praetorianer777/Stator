import type { publicReadingEn, anonymousAccessEn, publicLinksEn, shareEn } from "./sharing.en";

/** The German strings of public reading, public links and sharing, held to the English shape. */
export const publicReadingDe: typeof publicReadingEn = {
  someone: "jemand",
  included: "Aus einer anderen Seite eingebunden: öffnen",
  signIn: "Anmelden",
  allSpaces: "Alle Bereiche",
  spaces: "Bereiche",
  noSpaces: "Hier ist noch nichts veröffentlicht.",
  pages: "Seiten in diesem Bereich",
  children: "Seiten in diesem Ordner",
  noChildren: "Dieser Ordner enthält keine öffentlichen Seiten.",
  updated: (date: string) => `Aktualisiert am ${date}`,
  searchLabel: "Öffentliche Seiten durchsuchen",
  searchPlaceholder: "Suchen",
  search: "Suchen",
  searchTitle: (q: string) => `Ergebnisse für ${q}`,
  noHits: "Keine öffentliche Seite passt zu diesen Wörtern. Versuchen Sie andere.",
  more: "Weitere Ergebnisse",
  earlier: "Vorige Ergebnisse",
  notPublicTitle: "Nur mit Anmeldung lesbar",
  notPublicBody: "Das ist nicht öffentlich, oder die Adresse stimmt nicht. Wenn Sie zur Organisation gehören, melden Sie sich an, um es zu lesen.",
  signInToRead: "Anmelden und lesen",
  failed: "Das ließ sich nicht laden. Prüfen Sie die Verbindung und versuchen Sie es erneut.",
};

export const anonymousAccessDe: typeof anonymousAccessEn = {
  title: "Lesen ohne Anmeldung",
  intro:
    "Alle im Internet dürfen die Bereiche lesen, die es erlauben, ohne Konto. Sie lesen nur veröffentlichte Seiten, ohne Namen darin: keine Autoren, Kommentare, Reaktionen, Leser oder Erwähnungen. Eingeschränkte Seiten bleiben privat.",
  enabled: "Alle die Bereiche lesen lassen, die es erlauben",
  indexable: "Suchmaschinen die öffentlichen Seiten aufnehmen lassen",
  address: (url: string) => `Öffentliche Seiten liegen unter ${url}`,
  saved: "Gespeichert.",
  spaceTitle: "Lesen ohne Anmeldung",
  spaceIntro:
    "Alle dürfen die veröffentlichten Seiten dieses Bereichs ohne Anmeldung lesen. Eingeschränkte Seiten, Entwürfe, Kommentare und die Namen von Personen bleiben privat.",
  spaceToggle: "Alle diesen Bereich ohne Anmeldung lesen lassen",
  siteOff: "Das gilt erst, wenn ein Administrator der Organisation das Lesen ohne Anmeldung erlaubt.",
  personal: "Ein persönlicher Bereich ist nie öffentlich. Verschieben Sie die Seiten in einen Teambereich, um sie zu veröffentlichen.",
  spaceAddress: (url: string) => `Öffentliche Adresse: ${url}`,
};

export const publicLinksDe: typeof publicLinksEn = {
  title: "Öffentlicher Link",
  intro:
    "Wer einen öffentlichen Link hat, liest diese Seite ohne Konto, bis der Link widerrufen wird oder abläuft. Er öffnet nur diese Seite: nicht die Seiten darunter, ihre Kommentare, ihren Verlauf oder irgendeinen Namen.",
  newTitle: "Öffentlichen Link erstellen",
  label: "Bezeichnung (optional)",
  labelPlaceholder: "Für wen er ist",
  expires: "Läuft ab",
  inDays: (n: number) => (n === 1 ? "In 1 Tag" : `In ${n} Tagen`),
  never: "Nie",
  create: "Öffentlichen Link erstellen",
  freshBody: "Kopiere den Link jetzt: Er wird nur dieses eine Mal gezeigt. Wer ihn hat, kann diese Seite lesen.",
  address: "Öffentlicher Link",
  copy: "Link kopieren",
  copied: "Kopiert.",
  copyFailed: "Der Link konnte nicht kopiert werden. Markiere ihn und kopiere ihn selbst.",
  listLabel: "Gültige öffentliche Links",
  unlabelled: "Öffentlicher Link",
  you: "dir",
  someoneGone: "jemandem, der nicht mehr dabei ist",
  madeBy: (who: string, date: string) => `Erstellt von ${who} am ${date}.`,
  runsOut: (date: string) => `Läuft am ${date} ab.`,
  neverRunsOut: "Gilt, bis er widerrufen wird.",
  revoke: "Widerrufen",
  revokeNamed: (label: string) => `${label} widerrufen`,
  full: (n: number) => `Diese Seite hat ${n} öffentliche Links, so viele, wie eine Seite haben darf. Widerrufe einen, um einen neuen zu erstellen.`,
  refusals: {
    cannotManage: "Nur wer diese Seite bearbeiten darf, erstellt öffentliche Links für sie. Frag jemanden, der sie bearbeitet.",
    off: "Deine Organisation erlaubt keine öffentlichen Links. Ein Administrator der Organisation kann sie unter Einstellungen, Berechtigungen erlauben.",
    personal: "Eine Seite eines persönlichen Bereichs wird nie für alle geöffnet. Verschiebe sie in einen Teambereich, um einen Link zu erstellen.",
    folder: "Ein Ordner listet nur seine Seiten auf. Erstelle stattdessen einen Link für eine Seite darin.",
    trashed: "Diese Seite liegt im Papierkorb. Stelle sie wieder her, um einen Link zu erstellen.",
    unpublished: "Diese Seite oder eine Seite darüber ist noch nicht veröffentlicht. Veröffentliche sie, um einen Link zu erstellen.",
    restricted:
      "Diese Seite oder eine Seite darüber ist auf bestimmte Personen beschränkt und wird deshalb niemand anderem geöffnet. Hebe die Beschränkung auf, um einen Link zu erstellen, oder teile die Seite mit diesen Personen.",
  },
  goneTitle: "Dieser Link funktioniert nicht mehr",
  goneBody: "Er wurde widerrufen, ist abgelaufen, oder die Seite wird nicht mehr so geteilt. Bitte die Person, die ihn geschickt hat, um einen neuen.",
  settingsTitle: "Öffentliche Links",
  settingsIntro:
    "Lass die Bearbeiter einer Seite Links erstellen, über die jeder genau diese Seite ohne Konto lesen kann. Ausgeschaltet funktioniert kein Link mehr, bis Links wieder erlaubt sind, und es lassen sich keine neuen erstellen.",
  settingsToggle: "Öffentliche Links erlauben",
  saved: "Gespeichert.",
};

export const shareDe: typeof shareEn = {
  button: "Teilen",
  title: (page: string) => `${page} teilen`,
  intro:
    "Senden Sie diese Seite an Personen und Gruppen, auf Wunsch mit einer Notiz. Sie erfahren es in der App und per E-Mail. Teilen gibt niemandem Zugriff: Benachrichtigt wird nur, wer die Seite schon ansehen darf.",
  pickerLabel: "Senden an",
  pickedLabel: "Empfänger",
  nobodyYet: "Noch niemand ausgewählt.",
  full: (n: number) => `Eine Freigabe nennt höchstens ${n} Personen und Gruppen. Teilen Sie für weitere noch einmal.`,
  cannotView: "Darf diese Seite nicht ansehen",
  groupClosed: "Niemand darin darf diese Seite ansehen",
  groupViewers: (viewers: number, members: number) =>
    viewers === members
      ? members === 1
        ? "1 Mitglied, das diese Seite ansehen darf"
        : `${members} Mitglieder, die diese Seite alle ansehen dürfen`
      : `${viewers} von ${members} Mitgliedern dürfen diese Seite ansehen; nur sie werden benachrichtigt`,
  closedNote:
    "Teilen gibt keinen Zugriff. Entfernen Sie die markierten Personen und Gruppen, oder bitten Sie einen Administrator des Bereichs oder jemanden, der die Einschränkungen der Seite ändern darf, ihnen zuerst Zugriff zu geben.",
  messageLabel: "Notiz (optional)",
  messageHint: (max: number) => `Bis zu ${max} Zeichen, gesendet mit dem Link zur Seite.`,
  viewersTitle: "Wer diese Seite ansehen darf",
  viewersEveryone: "Alle in der Organisation dürfen diese Seite ansehen.",
  viewersCount: (n: number) => (n === 1 ? "1 Person darf diese Seite ansehen:" : `${n} Personen dürfen diese Seite ansehen:`),
  viewersRestricted: (n: number) =>
    n === 1 ? "Diese Seite ist eingeschränkt. 1 Person darf sie ansehen:" : `Diese Seite ist eingeschränkt. ${n} Personen dürfen sie ansehen:`,
  viewersMore: (n: number) => `und ${n} weitere`,
  refusedClosed:
    "Es wurde nichts geteilt, weil die Seite für jemanden, den Sie ausgewählt haben, nicht zugänglich ist. Entfernen Sie die markierten Personen und Gruppen, oder bitten Sie einen Administrator des Bereichs oder jemanden, der die Einschränkungen der Seite ändern darf, ihnen zuerst Zugriff zu geben.",
  cancel: "Abbrechen",
  send: "Teilen",
  sent: (n: number) => (n === 1 ? "Mit 1 Person geteilt." : `Mit ${n} Personen geteilt.`),
  done: "Fertig",
};
