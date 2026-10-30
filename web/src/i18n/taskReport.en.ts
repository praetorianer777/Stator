/** The English strings of the task report. */
export const taskReportEn = {
  edit: "Edit report",
  loading: "Finding the tasks",
  failed: "The tasks could not be listed. Reload the page to try again.",
  empty: "No task on a page you can read matches this report.",
  nobody: "Nobody assigned",
  truncated: (n: number) => `Showing the first ${n} tasks. Narrow the report to see the rest.`,
  title: (s: { space: string | null; assignee: string | null; due: string; state: string }, name: string) =>
    [
      `${s.state === "done" ? "Done tasks" : s.state === "all" ? "Tasks" : "Open tasks"}${s.space ? ` in ${s.space}` : ""}`,
      s.assignee === "me" ? "assigned to you" : s.assignee === "none" ? "assigned to nobody" : s.assignee ? `assigned to ${name || "one person"}` : "",
      ({ overdue: "overdue", today: "due today", week: "due in the next 7 days", none: "without a due date" } as Record<string, string>)[s.due] ?? "",
    ]
      .filter(Boolean)
      .join(", "),
  dialog: {
    titleNew: "Insert a task report",
    titleEdit: "Edit the task report",
    space: "Space",
    everySpace: "Every space",
    assignee: "Assigned to",
    anybody: "Anybody",
    reader: "Whoever reads the page",
    nobody: "Nobody",
    somebodyGone: "Somebody who has left",
    findPerson: "Find a person",
    morePeople: "More people match than are listed; type more of a name to find them.",
    due: "Due",
    dues: { any: "Any day, or none", overdue: "Overdue", today: "Today", week: "In the next 7 days", none: "No due date" } as Record<string, string>,
    state: "State",
    states: { open: "Open", done: "Done", all: "Open and done" } as Record<string, string>,
    limit: "Show",
    limitChoice: (n: number) => `${n} tasks`,
    cancel: "Cancel",
    insert: "Insert",
    save: "Save",
  },
};
