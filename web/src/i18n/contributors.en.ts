/** The English strings of the contributors list. */
export const contributorsEn = {
  edit: "Edit list",
  title: (scope: string): string => (scope === "tree" ? "Contributors to this page and the pages below it" : "Contributors to this page"),
  loading: "Finding who published",
  failed: "The contributors could not be listed. Reload the page to try again.",
  empty: "Nobody has published a version here yet.",
  unsaved: "Save the page once to list who published it.",
  meta: (edits: number, day: string) => `${edits === 1 ? "1 version" : `${edits} versions`}, the last on ${day}`,
  truncated: (n: number) => `Showing the first ${n} people. Raise the number in the list's settings to see more.`,
  dialog: {
    title: "Contributors",
    scope: "Count",
    scopes: { page: "This page", tree: "This page and the pages below it" } as Record<string, string>,
    limit: "Show",
    limitChoice: (n: number) => `${n} people`,
    cancel: "Cancel",
    save: "Save",
  },
};
