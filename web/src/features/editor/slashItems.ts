import type { ChainedCommands } from "@tiptap/core";
import type { ComponentType } from "react";
import { Icon, type IconProps } from "@/components/icons";
import { TABLE_DEFAULT_COLS, TABLE_DEFAULT_ROWS } from "@/config";
import { t } from "@/i18n";

export type BlockKey = keyof typeof t.editor.blocks;

/** One block the slash menu can insert. */
export interface SlashItem {
  key: BlockKey;
  label: string;
  description: string;
  icon: ComponentType<IconProps>;
  /** Other words a person might type for it. */
  keywords: string[];
  run: (chain: ChainedCommands) => void;
  /** Offered only while the organization has an Armature to ask. */
  armature?: boolean;
}

function item(key: BlockKey, icon: ComponentType<IconProps>, keywords: string[], run: (chain: ChainedCommands) => ChainedCommands): SlashItem {
  return { key, label: t.editor.blocks[key].label, description: t.editor.blocks[key].description, icon, keywords, run: (chain) => void run(chain).run() };
}

/** Every block a page can hold, in the order the menu lists them. */
export const SLASH_ITEMS: SlashItem[] = [
  item("paragraph", Icon.Lines, ["paragraph", "text", "p"], (c) => c.setParagraph()),
  item("heading1", Icon.Heading, ["h1", "title"], (c) => c.setHeading({ level: 1 })),
  item("heading2", Icon.Heading, ["h2", "subtitle"], (c) => c.setHeading({ level: 2 })),
  item("heading3", Icon.Heading, ["h3"], (c) => c.setHeading({ level: 3 })),
  item("bulletList", Icon.Lines, ["ul", "bullet", "unordered"], (c) => c.toggleBulletList()),
  item("orderedList", Icon.OrderedList, ["ol", "ordered", "steps"], (c) => c.toggleOrderedList()),
  item("taskList", Icon.Checklist, ["todo", "task", "checkbox"], (c) => c.toggleTaskList()),
  item("quote", Icon.Quote, ["blockquote", "citation"], (c) => c.toggleBlockquote()),
  item("codeBlock", Icon.CodeBlock, ["code", "snippet", "pre"], (c) => c.setCodeBlock()),
  item("divider", Icon.Divider, ["hr", "rule", "line", "separator"], (c) => c.setHorizontalRule()),
  item("table", Icon.Table, ["grid", "columns", "rows"], (c) => c.insertTable({ rows: TABLE_DEFAULT_ROWS, cols: TABLE_DEFAULT_COLS, withHeaderRow: true })),
  item("panelInfo", Icon.Panel, ["panel", "info", "callout"], (c) => c.setPanel("info")),
  item("panelNote", Icon.Panel, ["panel", "note", "aside"], (c) => c.setPanel("note")),
  item("panelSuccess", Icon.Panel, ["panel", "success", "tip"], (c) => c.setPanel("success")),
  item("panelWarning", Icon.Panel, ["panel", "warning", "caution"], (c) => c.setPanel("warning")),
  item("panelError", Icon.Panel, ["panel", "error", "danger"], (c) => c.setPanel("error")),
  item("decision", Icon.Decision, ["decision", "decide", "agreed", "resolution", "outcome"], (c) => c.setDecision()),
  item("linkCard", Icon.Link, ["link", "url", "preview", "card", "embed", "video", "bookmark"], (c) => c.pickLinkCard()),
  item("diagram", Icon.Diagram, ["diagram", "mermaid", "flowchart", "chart", "sequence", "graph", "architecture"], (c) => c.insertDiagram()),
  item("mathBlock", Icon.Sigma, ["math", "formula", "equation", "latex", "tex", "katex"], (c) => c.insertMathBlock()),
  item("expand", Icon.Disclosure, ["expand", "collapse", "toggle", "details", "fold"], (c) => c.setExpand()),
  item("columns2", Icon.Columns, ["columns", "layout", "side by side", "two", "split"], (c) => c.setColumns(2)),
  item("columns3", Icon.Columns, ["columns", "layout", "side by side", "three"], (c) => c.setColumns(3)),
  item("tableOfContents", Icon.Hash, ["toc", "contents", "headings", "outline"], (c) => c.insertTableOfContents()),
  item("childPages", Icon.Page, ["children", "pages", "subpages", "tree"], (c) => c.insertChildPages()),
  { ...item("armatureIssue", Icon.Task, ["armature", "issue", "ticket", "card"], (c) => c.pickArmatureIssue()), armature: true },
  { ...item("armatureIssueList", Icon.Table, ["armature", "issues", "query", "nql", "list"], (c) => c.pickArmatureIssueList()), armature: true },
  item("status", Icon.Label, ["status", "state", "badge", "label", "tag"], (c) => c.insertStatus()),
  item("date", Icon.Calendar, ["date", "day", "today", "deadline", "when"], (c) => c.insertDate()),
  item("mathInline", Icon.Sigma, ["math", "formula", "equation", "latex", "tex", "katex", "inline"], (c) => c.insertMathInline()),
  // The colon opens the emoji list as if typed, so there is one picker to learn.
  item("emoji", Icon.Smile, ["emoji", "smiley", "reaction"], (c) => c.insertContent(":")),
];

/** The blocks this editor offers: the Armature ones only where there is an Armature. */
export function slashItemsFor(armature: boolean): SlashItem[] {
  return armature ? SLASH_ITEMS : SLASH_ITEMS.filter((it) => !it.armature);
}

/** The blocks whose name or keywords contain what was typed after the slash. */
export function filterSlashItems(query: string, items: SlashItem[] = SLASH_ITEMS): SlashItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  return items.filter((it) => it.label.toLowerCase().includes(q) || it.keywords.some((k) => k.startsWith(q)));
}
