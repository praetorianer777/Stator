import type { WordImport } from "@/api/word";

/** The English strings of importing a Word document. */
export const wordImportEn = {
  menu: "Import Word documents",
  title: (title: string) => `Import Word documents under ${title}`,
  hint: "Choose one or more Word documents (.docx), or a .zip of them. Each document becomes a published page named after its title, with its pictures as the page's files, and each folder in a .zip a page too. Several documents are imported in the background.",
  choose: "Choose documents",
  chosen: (n: number) => (n === 1 ? "1 file chosen" : `${n} files chosen`),
  noneChosen: "No documents chosen yet.",
  noWord: "None of these is a Word document. Choose .docx files, or a .zip of them; save an older .doc file as .docx in Word first.",
  tooLarge: (limit: string) => `This document weighs more than ${limit}, which one import takes. Make its pictures smaller in Word, or split it.`,
  tooLargeAll: (limit: string) => `These files weigh more than ${limit}, which one import takes. Import them in several parts.`,
  tooMany: (max: number) => `Choose at most ${max} documents at once, and import the rest afterwards.`,
  submit: "Import",
  sending: (percent: number) => `Sending the documents, ${percent}%`,
  imported: "Imported the page",
  warningsTitle: "What did not come across as written",
  queued: "Waiting for the import to begin.",
  running: (done: number, total: number) => `Imported ${done} of ${total} pages.`,
  progress: "Pages imported",
  slow: "The import has not begun after a minute. The worker may not be running; the import begins once it does, so you may close this and come back later.",
  gaveUp: "The import is taking far longer than it should. Reload the page to see the pages it made, or ask whoever runs Stator to look at the worker.",
  done: (n: number) => (n === 1 ? "Imported 1 page." : `Imported ${n} pages.`),
  notImported: "Not imported",
  skipped: "Left out, since they are no Word documents",
  failures: {
    forbidden:
      "You may no longer add pages under this page, so the import stopped and the pages it made were moved to the trash. Ask an administrator of the space for the permission, then import again.",
    parent_gone:
      "The page the documents were going under was deleted, so the import stopped and the pages it made were moved to the trash. Choose another page and import again.",
    failed:
      "The import failed, and the pages it made were moved to the trash. Import the documents again; if it fails once more, ask whoever runs Stator to look at the worker's log.",
  } satisfies Record<NonNullable<WordImport["failure"]>, string>,
  close: "Close",
  cancel: "Cancel",
};
