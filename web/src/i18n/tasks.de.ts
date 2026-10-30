import type { tasksEn } from "./tasks.en";

/** The German strings of tasks, held to the English shape. */
export const tasksDe: typeof tasksEn = {
  gone: "Diese Aufgabe steht nicht mehr auf der Seite. Sie wurde vielleicht gelöscht oder steht nur in einem Entwurf.",
  title: "Meine Aufgaben",
  intro:
    "Checklisteneinträge, die Sie erwähnen, von allen Seiten, die Sie lesen dürfen. Eine Aufgabe gehört Ihnen, wenn ihre erste Erwähnung Sie nennt, und ist am ersten Datum darin fällig.",
  open: "Offen",
  done: "Erledigt",
  emptyOpen: "Keine offenen Aufgaben. Sobald jemand Sie in einem Checklisteneintrag erwähnt und die Seite veröffentlicht, erscheint er hier.",
  emptyDone: "Noch nichts abgehakt.",
  loadFailed: "Ihre Aufgaben konnten nicht geladen werden. Versuchen Sie es gleich noch einmal.",
  more: "Mehr anzeigen",
  tick: (text: string) => `Erledigt: ${text}`,
  readOnly: "Sie dürfen diese Seite nicht bearbeiten und können die Aufgabe deshalb hier nicht abhaken.",
  ticked: (text: string) => `${text} ist erledigt.`,
  reopened: (text: string) => `${text} ist wieder offen.`,
  tickFailed: "Die Aufgabe konnte nicht geändert werden. Laden Sie die Seite neu und versuchen Sie es noch einmal.",
  onPage: (title: string, space: string) => `${title} in ${space}`,
  assignedBy: (who: string) => `Zugewiesen von ${who}`,
  doneOn: (day: string) => `Erledigt am ${day}`,
  overdue: "Überfällig",
  overdueSince: (day: string) => `Überfällig, fällig am ${day}`,
  dueToday: "Heute fällig",
  dueOn: (day: string) => `Fällig am ${day}`,
  homeTitle: "Meine Aufgaben",
  homeEmpty: "Keine offenen Aufgaben für Sie.",
  homeAll: "Alle meine Aufgaben",
};
