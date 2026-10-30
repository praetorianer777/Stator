/** The English strings of page properties and their report. */
export const propertiesEn = {
  report: {
    edit: "Edit report",
    loading: "Gathering the pages' properties",
    failed: "The report could not be read. Reload the page to try again.",
    empty: (labels: string[]) => `No published page you can read carries ${labels.length === 1 ? "the label" : "all the labels"} ${labels.join(", ")}.`,
    page: "Page",
    title: (labels: string[], space: string | null) => `Pages labelled ${labels.join(", ")}${space ? ` in ${space}` : ""}`,
    caption: (labels: string[], n: number) =>
      `${n === 1 ? "1 page" : `${n} pages`} labelled ${labels.join(", ")}, with their properties. Choose a column's heading to sort by it.`,
    summary: (labels: string[], space: string | null) => `Properties report of pages labelled ${labels.join(", ")}${space ? ` in ${space}` : ""}`,
    truncated: "Only the first 200 pages are listed; add a label or choose a space to narrow the report.",
  },
  dialog: {
    titleNew: "Insert a properties report",
    titleEdit: "Edit the properties report",
    labels: "Labels",
    labelsHint: "Separate labels with commas. The report lists the pages that carry all of them.",
    labelsNone: "Name at least one label to gather pages by.",
    labelsMany: (n: number) => `Name at most ${n} labels.`,
    labelBad: (raw: string) => `"${raw}" cannot be a label. Use letters, digits, hyphens, underscores and dots.`,
    space: "Space",
    everySpace: "Every space",
    columns: "Properties to show",
    columnsHint: "One name a line, in the order of the columns. Leave it empty to show every property the pages have.",
    columnLong: (n: number) => `Keep each name to ${n} characters.`,
    columnsMany: (n: number) => `Show at most ${n} properties.`,
    cancel: "Cancel",
    insert: "Insert",
    save: "Save",
  },
};
