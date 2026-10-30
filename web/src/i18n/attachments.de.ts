import type { attachmentListEn, attachmentsEn, previewEn, lightboxEn, pdfEn, docxEn } from "./attachments.en";

/** The German strings of attachments, their previews and the lightbox, held to the English shape. */
export const attachmentListDe: typeof attachmentListEn = {
  title: "Dateien dieser Seite",
  upload: "Dateien hochladen",
  uploadHint: "Eine Datei mit dem Namen einer vorhandenen wird deren nächste Version.",
  uploaded: (name: string, version: number) => (version > 1 ? `${name} als Version ${version} hochgeladen.` : `${name} hochgeladen.`),
  unsaved: "Speichern Sie die Seite einmal, um ihre Dateien hier aufzulisten.",
  failed: "Die Dateien konnten nicht aufgelistet werden. Laden Sie die Seite neu, um es erneut zu versuchen.",
  empty: "Noch keine Dateien auf dieser Seite.",
  emptyEditable: "Noch keine Dateien auf dieser Seite. Laden Sie eine mit Dateien hochladen hoch.",
  someone: "Eine Person, die gegangen ist",
  version: (n: number) => `Version ${n}`,
  versionOf: (name: string, n: number) => `${name}, Version ${n}`,
  earlier: (n: number) => (n === 1 ? "1 frühere Version" : `${n} frühere Versionen`),
};

export const attachmentsDe: typeof attachmentsEn = {
  title: "Anhänge",
  count: (n: number) => `Anhänge (${n})`,
  attach: "Dateien anhängen",
  dropHere: "Dateien hier ablegen, um sie an die Seite anzuhängen.",
  empty: "Noch nichts angehängt.",
  emptyEditable: "Noch nichts angehängt. Legen Sie Dateien hier ab, oder wählen Sie sie mit Dateien anhängen.",
  loading: "Anhänge werden geladen",
  uploading: (name: string, percent: number) => `${name} wird hochgeladen, ${percent} %`,
  uploaded: (name: string) => `${name} angehängt.`,
  download: (name: string) => `${name} herunterladen`,
  open: (name: string) => `${name} öffnen`,
  remove: (name: string) => `${name} löschen`,
  removed: (name: string) => `${name} gelöscht.`,
  confirmRemove: "Die Datei lässt sich nicht wiederherstellen, und die Seite und ihre älteren Versionen zeigen sie als fehlende Datei.",
  confirmRemoveAll: (n: number) =>
    `Alle ${n} Versionen gehen mit. Keine davon lässt sich wiederherstellen, und die Seite und ihre älteren Versionen zeigen sie als fehlende Dateien.`,
  removeVersion: (name: string, n: number) => `${name}, Version ${n} löschen`,
  confirmRemoveVersion: "Die anderen Versionen bleiben, und wo die Seite diese Version zeigt, zeigt sie eine fehlende Datei.",
  removedVersion: (name: string, n: number) => `Version ${n} von ${name} gelöscht.`,
  restore: "Wiederherstellen",
  restoreVersion: (name: string, n: number) => `${name}, Version ${n} wiederherstellen`,
  restored: (name: string, from: number, to: number) => `Version ${from} von ${name} als Version ${to} wiederhergestellt.`,
  restoredFrom: (n: number) => `wiederhergestellt aus Version ${n}`,
  editedFrom: (n: number) => `bearbeitet aus Version ${n}`,
  uploadedBy: (who: string, when: string) => `${who}, ${when}`,
  tooLarge: (name: string, size: string, limit: string) =>
    `${name} (${size}) ist größer, als diese Website annimmt.${limit ? ` ${limit}` : ""} Verkleinern Sie die Datei oder teilen Sie sie auf, und hängen Sie sie erneut an.`,
  refused: (name: string, why: string) => `${name} konnte nicht angehängt werden: ${why}`,
  uploadBroke: (name: string) => `Das Hochladen von ${name} ist abgebrochen. Prüfen Sie Ihre Verbindung und hängen Sie die Datei erneut an.`,
  missing: (name: string) => (name ? `${name} (Datei gelöscht)` : "Gelöschte Datei"),
  missingImage: "Dieses Bild wurde gelöscht. Entfernen Sie es von der Seite, oder hängen Sie das Bild erneut an.",
  imageTools: "Bild",
  altText: "Alternativtext",
  altHint: "Beschreiben Sie das Bild für Menschen, die es nicht sehen können.",
  width: "Breite",
  widths: {
    small: "Klein",
    medium: "Mittel",
    large: "Groß",
    original: "Originalgröße",
  } as Record<string, string>,
  removeImage: "Bild entfernen",
};

export const previewDe: typeof previewEn = {
  open: (name: string) => `Vorschau von ${name}`,
  title: (name: string) => `Vorschau von ${name}`,
  frame: (name: string) => `${name}, als PDF angezeigt`,
  loading: (name: string) => `${name} wird geladen`,
  converting: (name: string) => `${name} wird für die Vorschau umgewandelt. Ein großes Dokument braucht beim ersten Mal einige Sekunden.`,
  newTab: "In neuem Tab öffnen",
  failed: "Die Vorschau konnte nicht geladen werden. Prüfen Sie Ihre Verbindung und öffnen Sie sie erneut, oder laden Sie die Datei herunter.",
};

export const lightboxDe: typeof lightboxEn = {
  position: (n: number, count: number) => `${n} von ${count}`,
  previous: "Vorheriges",
  next: "Nächstes",
  zoomIn: "Vergrößern",
  zoomOut: "Verkleinern",
  fit: (percent: number) => `An das Fenster anpassen, jetzt ${percent} %`,
  stage: (name: string) => `${name}, zoombar`,
  hint: "Plus und Minus zoomen, 0 passt das Bild wieder an das Fenster an, und die Pfeiltasten verschieben ein vergrößertes Bild.",
  hintMany:
    "Plus und Minus zoomen, 0 passt das Bild wieder an das Fenster an, und die Pfeiltasten verschieben ein vergrößertes Bild oder wechseln zum vorherigen und nächsten.",
  status: (name: string, position: string, percent?: number) => [position, name, percent === undefined ? "" : `${percent} %`].filter(Boolean).join(", "),
  imageFailed: (name: string) => `${name} konnte nicht angezeigt werden. Laden Sie die Seite neu, um zu sehen, ob es noch da ist, oder laden Sie es herunter.`,
  videoFailed: (name: string) => `${name} kann in diesem Browser nicht abgespielt werden. Laden Sie es herunter, um es auf Ihrem Gerät abzuspielen.`,
  openImage: (alt: string) => (alt ? `${alt} vergrößert ansehen` : "Bild vergrößert ansehen"),
  picture: "Bild",
  play: (name: string) => `${name} abspielen`,
};

export const pdfDe: typeof pdfEn = {
  exportMenu: "Als PDF exportieren",
  exportButton: "PDF",
  exportLabel: "Diese Seite als PDF exportieren",
  title: (title: string) => `${title} als PDF exportieren`,
  making: "Das PDF wird erstellt. Das dauert ein paar Sekunden; sobald es fertig ist, landet es in Ihren Downloads.",
  note: "Das PDF zeigt die veröffentlichte Seite so, wie Sie sie lesen, mit allen Blöcken.",
  networkFailed: "Das PDF konnte nicht abgerufen werden. Prüfen Sie Ihre Verbindung und versuchen Sie es erneut.",
  cancel: "Abbrechen",
  close: "Schließen",
  footer: (version: number, updated: string, printed: string) => `Version ${version} vom ${updated} · Gedruckt am ${printed}`,
  pages: "Seite {page} von {pages}",
  header: (parts: string[]) => parts.filter(Boolean).join(" · "),
};

export const docxDe: typeof docxEn = {
  exportMenu: "Als Word exportieren",
  exportButton: "Word",
  exportLabel: "Diese Seite als Word-Dokument exportieren",
  title: (title: string) => `${title} als Word exportieren`,
  making: "Das Word-Dokument wird erstellt. Sobald es fertig ist, landet es in Ihren Downloads.",
  note: "Das Dokument enthält die veröffentlichte Seite so, wie Sie sie lesen, bereit zum Bearbeiten ohne Verbindung. Blöcke, die Stator für jede Person füllt, etwa Aufgabenberichte und Armature-Vorgänge, stehen dort als Satz.",
  networkFailed: "Das Word-Dokument konnte nicht abgerufen werden. Prüfen Sie Ihre Verbindung und versuchen Sie es erneut.",
  cancel: "Abbrechen",
  close: "Schließen",
};
