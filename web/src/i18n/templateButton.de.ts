import type { templateButtonEn } from "./templateButton.en";

/** The German strings of the template button, held to the English shape. */
export const templateButtonDe: typeof templateButtonEn = {
  edit: "Schaltfläche bearbeiten",
  untitled: "Neue Seite aus einer Vorlage",
  defaultLabel: (name: string) => `Neue Seite: ${name}`,
  summary: (template: string) =>
    template ? `Eine Schaltfläche, die eine Seite aus der Vorlage ${template} anlegt` : "Eine Schaltfläche, die eine Seite aus einer Vorlage anlegt",
  loading: "Es wird geprüft, wohin die Seite käme",
  unset: "Diese Schaltfläche nennt noch keine Vorlage oder keinen Ort für ihre Seite. Bearbeiten Sie sie, um beides zu wählen.",
  missing:
    "Die Vorlage oder der Ort für ihre Seite ist nicht mehr da, oder Sie dürfen ihn nicht sehen. Bitten Sie, wer diese Seite pflegt, die Schaltfläche zu bearbeiten.",
  refused: (where: string) => `Sie dürfen ${where} keine Seiten anlegen. Bitten Sie eine Administratorin oder einen Administrator des Bereichs darum.`,
  makes: (name: string, where: string) => `Legt aus ${name} eine Seite ${where} an und öffnet sie zum Schreiben.`,
  makesAsking: (name: string, where: string) => `Fragt, was ${name} braucht, legt die Seite ${where} an und öffnet sie zum Schreiben.`,
  fillIn: (name: string) => `Neue Seite aus ${name}`,
  create: "Seite anlegen",
  cancel: "Abbrechen",
  inEditor: "Die Schaltfläche legt Seiten an, sobald die Seite gespeichert ist; beim Bearbeiten tut sie nichts.",
  topOf: (space: string) => `ganz oben in ${space}`,
  under: (title: string) => `unter ${title}`,
  dialog: {
    titleNew: "Vorlagen-Schaltfläche einfügen",
    titleEdit: "Vorlagen-Schaltfläche bearbeiten",
    template: "Vorlage",
    chooseTemplate: "Vorlage wählen",
    pickTemplate: "Wählen Sie die Vorlage, aus der die Schaltfläche Seiten anlegt.",
    space: "Bereich",
    thisSpace: "Der Bereich dieser Seite",
    parent: "Neue Seiten anlegen",
    top: "Ganz oben im Bereich",
    keptParent: "Unter der zuvor gewählten Seite",
    label: "Text der Schaltfläche",
    labelHint: "Leer nimmt den Namen der Vorlage.",
    title: "Titel jeder neuen Seite",
    titleHint: (token: string) =>
      `${token} wird zum Tag, an dem die Seite angelegt wird, und der Name einer Variablen in geschweiften Klammern zu ihrem Wert. Leer nimmt den Titel der Vorlage.`,
    cancel: "Abbrechen",
    insert: "Einfügen",
    save: "Speichern",
  },
};
