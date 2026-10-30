import type { annotateEn } from "./annotate.en";

/** The German strings of annotating an image, held to the English shape. */
export const annotateDe: typeof annotateEn = {
  action: "Markieren",
  open: (name: string) => `${name} markieren`,
  title: (name: string) => `${name} markieren`,
  tools: "Werkzeuge",
  tool: {
    select: "Auswählen",
    crop: "Zuschneiden",
    arrow: "Pfeil",
    box: "Rahmen",
    text: "Text",
  } as Record<string, string>,
  colour: "Farbe",
  colours: {
    red: "Rot",
    yellow: "Gelb",
    green: "Grün",
    blue: "Blau",
    black: "Schwarz",
    white: "Weiß",
  } as Record<string, string>,
  undo: "Rückgängig",
  redo: "Wiederholen",
  remove: "Ausgewählte Form löschen",
  stage: (name: string) => `${name}, zum Zeichnen`,
  hint: "Ziehen Sie auf dem Bild, um mit dem gewählten Werkzeug zu zeichnen, oder drücken Sie Enter, um eine Form in die Mitte zu setzen. Mit Auswählen wählt Enter die nächste Form; die Pfeiltasten verschieben sie, mit Umschalt ändern sie ihre Größe, und Entf löscht sie. Strg+Z macht rückgängig, Strg+Umschalt+Z wiederholt.",
  selected: (what: string) => `${what} ausgewählt.`,
  textLabel: "Text",
  addText: "Text einfügen",
  changeText: "Text ändern",
  textHint: "Schreiben Sie die Wörter und drücken Sie Enter, um sie auf das Bild zu setzen.",
  showInPage: "Das bearbeitete Bild in dieser Seite zeigen",
  save: "Als neue Version speichern",
  saving: "Wird gespeichert",
  cancel: "Abbrechen",
  loading: (name: string) => `${name} wird geladen`,
  failed: (name: string) =>
    `${name} konnte nicht zum Bearbeiten geladen werden. Laden Sie die Seite neu und versuchen Sie es erneut, oder laden Sie das Bild herunter.`,
  tooLarge: (name: string, width: number, height: number) =>
    `${name} ist ${width} mal ${height} Pixel groß, mehr als ein Browser bearbeiten kann. Verkleinern Sie es auf Ihrem Gerät und laden Sie es unter demselben Namen hoch.`,
  cannotSave: (type: string) =>
    `Dieser Browser kann keine ${type}-Bilder speichern. Versuchen Sie einen anderen Browser, oder laden Sie das Bild herunter und bearbeiten Sie es auf Ihrem Gerät.`,
  unchanged: "Zeichnen, schreiben oder schneiden Sie zuerst etwas, dann speichern Sie.",
  refused: (name: string, why: string) => `Das bearbeitete ${name} konnte nicht gespeichert werden: ${why}`,
  broke: (name: string) => `Das Speichern des bearbeiteten ${name} brach ab. Prüfen Sie Ihre Verbindung und speichern Sie erneut.`,
  confirmDiscard: "Was Sie auf dieses Bild gezeichnet haben, verwerfen?",
  saved: (name: string, version: number) => `Das bearbeitete ${name} ist als Version ${version} gespeichert.`,
};
