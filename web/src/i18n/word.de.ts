import type { wordImportEn } from "./word.en";

/** The German strings of importing a Word document, held to the English shape. */
export const wordImportDe: typeof wordImportEn = {
  menu: "Word-Dokumente importieren",
  title: (title: string) => `Word-Dokumente unter ${title} importieren`,
  hint: "Wählen Sie ein oder mehrere Word-Dokumente (.docx) oder eine .zip-Datei davon. Jedes Dokument wird eine veröffentlichte Seite mit dem Titel des Dokuments und seinen Bildern als Dateien der Seite, und jeder Ordner in einer .zip-Datei ebenfalls eine Seite. Mehrere Dokumente werden im Hintergrund importiert.",
  choose: "Dokumente wählen",
  chosen: (n: number) => (n === 1 ? "1 Datei gewählt" : `${n} Dateien gewählt`),
  noneChosen: "Noch keine Dokumente gewählt.",
  noWord:
    "Keine dieser Dateien ist ein Word-Dokument. Wählen Sie .docx-Dateien oder eine .zip-Datei davon; speichern Sie eine ältere .doc-Datei zuerst in Word als .docx.",
  tooLarge: (limit: string) => `Dieses Dokument ist größer als ${limit}, die ein Import annimmt. Verkleinern Sie seine Bilder in Word, oder teilen Sie es auf.`,
  tooLargeAll: (limit: string) => `Diese Dateien sind zusammen größer als ${limit}, die ein Import annimmt. Importieren Sie sie in mehreren Teilen.`,
  tooMany: (max: number) => `Wählen Sie höchstens ${max} Dokumente auf einmal, und importieren Sie den Rest danach.`,
  submit: "Importieren",
  sending: (percent: number) => `Die Dokumente werden gesendet, ${percent} %`,
  imported: "Die Seite wurde importiert",
  warningsTitle: "Was nicht wie geschrieben übernommen wurde",
  queued: "Der Import wartet auf seinen Beginn.",
  running: (done: number, total: number) => `${done} von ${total} Seiten importiert.`,
  progress: "Importierte Seiten",
  slow: "Der Import hat nach einer Minute noch nicht begonnen. Vielleicht läuft der Worker nicht; sobald er läuft, beginnt der Import, Sie können dieses Fenster also schließen und später wiederkommen.",
  gaveUp:
    "Der Import dauert viel länger als vorgesehen. Laden Sie die Seite neu, um die angelegten Seiten zu sehen, oder bitten Sie die Person, die Stator betreibt, nach dem Worker zu sehen.",
  done: (n: number) => (n === 1 ? "1 Seite importiert." : `${n} Seiten importiert.`),
  notImported: "Nicht importiert",
  skipped: "Ausgelassen, da keine Word-Dokumente",
  failures: {
    forbidden:
      "Sie dürfen unter dieser Seite keine Seiten mehr anlegen. Der Import wurde deshalb angehalten, und die angelegten Seiten liegen im Papierkorb. Bitten Sie eine Person, die den Bereich verwaltet, um die Berechtigung, und importieren Sie dann noch einmal.",
    parent_gone:
      "Die Seite, unter die die Dokumente kommen sollten, wurde gelöscht. Der Import wurde deshalb angehalten, und die angelegten Seiten liegen im Papierkorb. Wählen Sie eine andere Seite und importieren Sie noch einmal.",
    failed:
      "Der Import ist gescheitert, und die angelegten Seiten liegen im Papierkorb. Importieren Sie die Dokumente noch einmal; scheitert es wieder, bitten Sie die Person, die Stator betreibt, ins Protokoll des Workers zu sehen.",
  },
  close: "Schließen",
  cancel: "Abbrechen",
};
