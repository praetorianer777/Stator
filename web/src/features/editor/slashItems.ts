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
];

/** The blocks whose name or keywords contain what was typed after the slash. */
export function filterSlashItems(query: string, items: SlashItem[] = SLASH_ITEMS): SlashItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  return items.filter((it) => it.label.toLowerCase().includes(q) || it.keywords.some((k) => k.startsWith(q)));
}
