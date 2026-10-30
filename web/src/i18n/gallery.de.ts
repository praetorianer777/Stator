import type { galleryEn } from "./gallery.en";

/** The German strings of the gallery, held to the English shape. */
export const galleryDe: typeof galleryEn = {
  label: (n: number) => (n === 1 ? "Galerie mit 1 Bild" : `Galerie mit ${n} Bildern`),
  summary: (n: number, columns: number) => `${n === 1 ? "1 Bild" : `${n} Bilder`}, bis zu ${columns} in einer Reihe`,
  edit: "Galerie bearbeiten",
  open: (caption: string, n: number, count: number) => `${caption || "Bild"} größer zeigen, ${n} von ${count}`,
  missing: "Dieses Bild wurde gelöscht. Entfernen Sie es aus der Galerie oder laden Sie es erneut hoch.",
  nothingShown: "Keines der Bilder dieser Galerie kann Ihnen gezeigt werden.",
  dialog: {
    titleNew: "Galerie einfügen",
    titleEdit: "Galerie bearbeiten",
    fromPage: "Bilder dieser Seite",
    noPictures: "Diese Seite hat noch keine Bilder. Laden Sie unten welche hoch.",
    failed: "Die Dateien der Seite konnten nicht aufgelistet werden. Schließen Sie dies und versuchen Sie es erneut.",
    unsaved: "Speichern Sie die Seite einmal, um Bilder in eine Galerie zu setzen.",
    upload: "Bilder hochladen",
    chosen: (n: number, max: number) => `In der Galerie: ${n} von höchstens ${max}`,
    chosenEmpty: "Noch nichts. Haken Sie oben Bilder an oder laden Sie neue hoch.",
    caption: (name: string) => `Bildunterschrift für ${name}`,
    moveUp: (name: string) => `${name} nach vorne`,
    moveDown: (name: string) => `${name} nach hinten`,
    remove: (name: string) => `${name} aus der Galerie entfernen`,
    columns: "Bilder in einer Reihe",
    columnsHint: "Ein Telefon zeigt zwei in einer Reihe, was Sie auch wählen.",
    perRow: (n: number) => `${n} in einer Reihe`,
    nonePicked: "Wählen Sie mindestens ein Bild oder laden Sie eines hoch, um eine Galerie zu machen.",
    cancel: "Abbrechen",
    insert: "Galerie einfügen",
    save: "Speichern",
  },
};
