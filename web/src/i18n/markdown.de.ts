import type { markdownEn } from "./markdown.en";

/** The German strings of Markdown export and import, held to the English shape. */
export const markdownDe: typeof markdownEn = {
  exportMenu: "Als Markdown exportieren",
  importMenu: "Markdown importieren",
  exportTitle: (title: string) => `${title} als Markdown exportieren`,
  exportWhat: "Was exportiert wird",
  exportMarkdown: "Diese Seite als eine Markdown-Datei",
  exportMarkdownHint: "Bilder und Dateien bleiben Links dorthin, wo ein Archiv sie ablegen würde.",
  exportPage: "Diese Seite mit ihren Dateien, als .zip",
  exportPageHint: "Die Markdown-Datei mit einem Ordner der Dateien der Seite.",
  exportSubtree: "Diese Seite, die Seiten darunter und ihre Dateien, als .zip",
  exportSubtreeHint: "Jede Seite darunter liegt in einem Ordner, der nach der Seite darüber benannt ist. Seiten, die Sie nicht sehen dürfen, fehlen.",
  download: "Herunterladen",
  exportNote: "Status, Erwähnungen, Vorgänge und erzeugte Blöcke bleiben kleine HTML-Elemente, die beim Import wieder eingelesen werden.",
  importTitle: (title: string) => `Markdown unter ${title} importieren`,
  importHint:
    "Wählen Sie Markdown-Dateien mit den Bildern und Dateien, die sie zeigen, einen Ordner davon oder eine .zip. Jede Datei wird eine Seite, jeder Ordner eine Seite mit seiner index.md oder README.md als Inhalt.",
  chooseFiles: "Dateien wählen",
  chooseFolder: "Ordner wählen",
  chosen: (n: number) => (n === 1 ? "1 Datei gewählt" : `${n} Dateien gewählt`),
  noneChosen: "Noch keine Dateien gewählt.",
  noMarkdown: "Keine davon ist eine Markdown-Datei. Wählen Sie .md-Dateien, einen Ordner mit solchen oder eine .zip davon.",
  tooLarge: (limit: string) => `Diese Dateien sind größer als ${limit}, was ein Import annimmt. Importieren Sie die Ordner einzeln.`,
  importSubmit: "Importieren",
  sending: (percent: number) => `Dateien werden gesendet, ${percent} %`,
  uploadBroke: "Das Hochladen ist abgebrochen. Prüfen Sie Ihre Verbindung und importieren Sie erneut.",
  imported: (n: number) => (n === 1 ? "1 Seite importiert." : `${n} Seiten importiert.`),
  importedPages: "Angelegte Seiten",
  warningsTitle: "Was nicht wie geschrieben übernommen wurde",
  close: "Schließen",
  cancel: "Abbrechen",
};
