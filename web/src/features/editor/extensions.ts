import { Extension, Node, mergeAttributes, type AnyExtension, type JSONContent } from "@tiptap/core";
import type { Schema } from "@tiptap/pm/model";
import type { EditorState, Transaction } from "@tiptap/pm/state";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import StarterKit from "@tiptap/starter-kit";
import Heading, { type Level } from "@tiptap/extension-heading";
import { Placeholder } from "@tiptap/extensions";
import { TaskItem, TaskList } from "@tiptap/extension-list";
import { Table, TableCell, TableHeader, TableRow } from "@tiptap/extension-table";
import { CodeBlockLowlight } from "@tiptap/extension-code-block-lowlight";
import Mention, { type MentionOptions } from "@tiptap/extension-mention";
import Suggestion, { type SuggestionOptions } from "@tiptap/suggestion";
import { Markdown } from "@tiptap/markdown";
import { t } from "@/i18n";
import { lowlight } from "./languages";
import { ANCHOR_PATTERN, CELL_BACKGROUNDS, HEADING_LEVELS, PANEL_KINDS, dedupe, safeHref, slug, type CellBackground, type PanelKind } from "./schema";
import type { SlashItem } from "./slashItems";
import type { AttachmentIndex } from "./attachmentIndex";
import { AttachmentChip, FileUpload, Image, type UploadFile } from "./attachments";
import { ChildPages, TableOfContents } from "./blockNodes";
import { Column, Columns } from "./columns";
import { Decision } from "./decision";
import { Expand } from "./expand";
import { Hint } from "./hint";
import { InlineComment } from "./inlineComment";
import { ArmatureIssue, type IssueSource } from "./armatureIssue";
import { ArmatureIssueBlock } from "./armatureIssueBlock";
import { ArmatureIssueList } from "./armatureIssueList";
import { DateNode, Status, type InlineValueTarget } from "./inlineValues";
import { MathBlock, MathInline } from "./math";
import { Diagram } from "./diagram";
import { LinkCardNode } from "./linkCard";
import { EmojiSuggestion, type EmojiOptions } from "./emoji";
import { FindReplace } from "./findReplace";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    panel: {
      setPanel: (kind: PanelKind) => ReturnType;
      setPanelKind: (kind: PanelKind) => ReturnType;
      unsetPanel: () => ReturnType;
    };
  }
}

function oneOf<T extends string>(values: readonly T[], value: string | null): T | null {
  return value !== null && (values as readonly string[]).includes(value) ? (value as T) : null;
}

/** A highlighted box around blocks, coloured by the theme's role for its kind. */
export const Panel = Node.create({
  name: "panel",
  group: "block",
  content: "block+",
  defining: true,
  addAttributes() {
    return {
      kind: {
        default: "info",
        parseHTML: (el) => oneOf(PANEL_KINDS, el.getAttribute("data-panel")) ?? "info",
        renderHTML: (attrs) => ({ "data-panel": attrs.kind, "aria-label": t.editor.panels[attrs.kind as string] }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-panel]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { role: "note" }), 0];
  },
  addCommands() {
    return {
      setPanel:
        (kind) =>
        ({ commands }) =>
          commands.wrapIn(this.name, { kind }),
      setPanelKind:
        (kind) =>
        ({ commands }) =>
          commands.updateAttributes(this.name, { kind }),
      unsetPanel:
        () =>
        ({ commands }) =>
          commands.lift(this.name),
    };
  },
});

const background = {
  default: null,
  parseHTML: (el: HTMLElement) => oneOf(CELL_BACKGROUNDS, el.getAttribute("data-background")),
  renderHTML: (attrs: Record<string, unknown>) => (attrs.background ? { "data-background": attrs.background as CellBackground } : {}),
};

export const Cell = TableCell.extend({
  addAttributes() {
    return { ...this.parent?.(), background };
  },
});

export const HeaderCell = TableHeader.extend({
  addAttributes() {
    return { ...this.parent?.(), background };
  },
});

/** Whether an anchor still belongs to its heading's text: the slug itself, or the slug with a dedupe number. */
function derivedFrom(anchor: string, base: string): boolean {
  return anchor === base || (anchor.startsWith(`${base}-`) && /^\d+$/.test(anchor.slice(base.length + 1)));
}

/**
 * Gives every heading an anchor made from its text. A heading keeps its
 * anchor while its text still yields it, so adding a same-named heading
 * elsewhere numbers the newcomer and never moves an existing link.
 */
export function anchorHeadings(state: EditorState): Transaction | null {
  const headings: Array<{ pos: number; id: unknown; base: string }> = [];
  state.doc.descendants((node, pos) => {
    if (node.type.name === "heading") headings.push({ pos, id: node.attrs.id, base: slug(node.textContent) });
    return !node.isTextblock;
  });
  const claimed = new Set<string>();
  const kept = headings.map(({ id, base }) => {
    if (typeof id !== "string" || !ANCHOR_PATTERN.test(id) || !derivedFrom(id, base) || claimed.has(id)) return false;
    claimed.add(id);
    return true;
  });
  let tr: Transaction | null = null;
  headings.forEach((heading, i) => {
    if (kept[i]) return;
    const id = dedupe(heading.base, claimed);
    claimed.add(id);
    if (heading.id !== id) {
      tr ??= state.tr;
      tr.setNodeAttribute(heading.pos, "id", id);
    }
  });
  // Anchors follow the text; undoing the text brings its anchor back with it.
  (tr as Transaction | null)?.setMeta("addToHistory", false);
  return tr;
}

const anchorsKey = new PluginKey("headingAnchors");

export const HeadingAnchors = Extension.create({
  name: "headingAnchors",
  addGlobalAttributes() {
    return [
      {
        types: ["heading"],
        attributes: {
          id: {
            default: null,
            parseHTML: (el) => {
              const id = el.getAttribute("data-anchor") ?? el.getAttribute("id");
              return id && ANCHOR_PATTERN.test(id) ? id : null;
            },
            // Not an id in the editor: the read-only view of the same page
            // owns the real ones, and an id twice on a page breaks both.
            renderHTML: (attrs) => (attrs.id ? { "data-anchor": attrs.id } : {}),
          },
        },
      },
    ];
  },
  addProseMirrorPlugins() {
    return [
      new Plugin({
        key: anchorsKey,
        appendTransaction: (transactions, _old, state) => (transactions.some((tr) => tr.docChanged) ? anchorHeadings(state) : null),
      }),
    ];
  },
  onCreate() {
    const tr = anchorHeadings(this.editor.state);
    if (tr) this.editor.view.dispatch(tr);
  },
});

/** Drops what a pasted document could carry that a page may not: unsafe links, deep headings. */
export function sanitizePasted(node: JSONContent): JSONContent {
  const out: JSONContent = { ...node };
  if (node.type === "heading") {
    const level = Number(node.attrs?.level ?? 1);
    out.attrs = { ...node.attrs, level: Math.min(Math.max(level, 1), Math.max(...HEADING_LEVELS)) };
  }
  if (node.marks) out.marks = node.marks.filter((mark) => mark.type !== "link" || safeHref(mark.attrs?.href) !== null);
  if (node.content) out.content = node.content.map(sanitizePasted);
  return out;
}

/**
 * Drops the nodes and marks a schema lacks, keeping a dropped block's text as
 * a paragraph, so markdown pasted into a comment cannot bring in a table.
 */
export function fitSchema(node: JSONContent, schema: Schema): JSONContent {
  const out: JSONContent = { ...node };
  if (node.marks) out.marks = node.marks.filter((mark) => Boolean(schema.marks[mark.type]));
  if (node.content) {
    out.content = node.content.flatMap((child): JSONContent[] => {
      if (!child.type || schema.nodes[child.type]) return [fitSchema(child, schema)];
      const text = textOfJSON(child).trim();
      return text ? [{ type: "paragraph", content: [{ type: "text", text }] }] : [];
    });
  }
  return out;
}

function textOfJSON(node: JSONContent): string {
  if (node.type === "text") return node.text ?? "";
  return (node.content ?? []).map(textOfJSON).join(" ");
}

// Enough markdown to be worth reading as such: a heading, a list, a quote,
// a fence, a table row, a rule, or inline emphasis, code or a link.
const MARKDOWN_HINT = /(^|\n)\s*(#{1,6}\s|[-*+]\s|\d+[.)]\s|>\s?|```|\|.*\||---+\s*$)|\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\)|~~[^~]+~~/;

const MarkdownPaste = Extension.create({
  name: "markdownPaste",
  addProseMirrorPlugins() {
    const editor = this.editor;
    return [
      new Plugin({
        key: new PluginKey("markdownPaste"),
        props: {
          handlePaste: (view, event) => {
            const data = event.clipboardData;
            if (!data || data.types.includes("text/html")) return false;
            const text = data.getData("text/plain");
            if (!text || !MARKDOWN_HINT.test(text) || view.state.selection.$from.parent.type.spec.code) return false;
            const parsed = editor.markdown?.parse(text) as JSONContent | undefined;
            if (!parsed?.content?.length) return false;
            return editor.commands.insertContent(fitSchema(sanitizePasted(parsed), editor.schema).content ?? []);
          },
        },
      }),
    ];
  },
});

export interface SlashMenuOptions {
  suggestion: Omit<SuggestionOptions<SlashItem, SlashItem>, "editor">;
}

export const slashMenuKey = new PluginKey("slashMenu");

/** A slash at the start of a word opens the list of blocks to insert. */
export const SlashMenu = Extension.create<SlashMenuOptions>({
  name: "slashMenu",
  addOptions() {
    return {
      suggestion: {
        char: "/",
        pluginKey: slashMenuKey,
        allow: ({ state, range }) => !state.doc.resolve(range.from).parent.type.spec.code,
        command: ({ editor, range, props }) => props.run(editor.chain().focus().deleteRange(range)),
      },
    };
  },
  addProseMirrorPlugins() {
    return [Suggestion({ editor: this.editor, ...this.options.suggestion })];
  },
});

// The page's title is its h1, so a document's levels are drawn one down, as
// the read-only view draws them. data-level keeps a copy and paste inside the
// editor at its level; a heading pasted from elsewhere keeps its own.
const ShiftedHeading = Heading.extend({
  parseHTML() {
    return [
      {
        tag: "h2[data-level], h3[data-level], h4[data-level]",
        priority: 60,
        getAttrs: (el: HTMLElement) => {
          const level = Number(el.dataset.level);
          return (this.options.levels as number[]).includes(level) ? { level } : false;
        },
      },
      ...this.options.levels.map((level: Level) => ({ tag: `h${level}`, attrs: { level } })),
    ];
  },
  renderHTML({ node, HTMLAttributes }) {
    const level = this.options.levels.includes(node.attrs.level) ? node.attrs.level : this.options.levels[0];
    return [`h${level + 1}`, mergeAttributes(this.options.HTMLAttributes, HTMLAttributes, { "data-level": level }), 0];
  },
});

/** A page holds every block the allowlist names; a comment holds text and its structure only. */
export type EditorVariant = "page" | "comment";

export interface ExtensionOptions {
  /** Which allowlist the editor's schema follows; a page's by default. */
  variant?: EditorVariant;
  placeholder?: string;
  mention?: Partial<MentionOptions>["suggestion"];
  slash?: Partial<SlashMenuOptions["suggestion"]>;
  submit?: () => void;
  /** Where a dropped, pasted or picked file goes; without it the editor takes no files. */
  upload?: UploadFile;
  /** Which files the page still has, so a deleted one is drawn as missing. */
  attachments?: AttachmentIndex;
  /** What turns typed keys and pasted issue addresses into chips; without it nothing does. */
  armature?: IssueSource;
  /** Opens the picker the slash menu's Armature issue asks which issue with. */
  pickIssue?: () => void;
  /** Opens the settings dialog the slash menu's Armature issue list starts with. */
  pickIssueList?: () => void;
  /** Opens the dialog the slash menu's link preview asks for an address with. */
  pickLinkCard?: () => void;
  /** Opens the dialog that changes a status, a date or a formula. */
  editInlineValue?: (target: InlineValueTarget) => void;
  /** Draws the emoji a colon offers; without it a colon offers none. */
  emoji?: Partial<EmojiOptions["suggestion"]>;
  /** Opens the find bar with the selected words; without it Ctrl or Cmd+F is the browser's. */
  find?: (seed: string) => void;
}

/** Every extension the editor runs; the read-only view draws the same nodes. */
export function editorExtensions({
  variant = "page",
  placeholder,
  mention,
  slash,
  submit,
  upload,
  attachments,
  armature,
  pickIssue,
  pickIssueList,
  pickLinkCard,
  editInlineValue,
  emoji,
  find,
}: ExtensionOptions = {}): AnyExtension[] {
  const shared: AnyExtension[] = [
    StarterKit.configure({
      underline: false,
      codeBlock: false,
      heading: false,
      horizontalRule: variant === "page" ? {} : false,
      link: { openOnClick: false, autolink: true, isAllowedUri: (url) => safeHref(url) !== null },
    }),
    ShiftedHeading.configure({ levels: [...HEADING_LEVELS] }),
    Placeholder.configure({ placeholder: placeholder ?? "" }),
    CodeBlockLowlight.configure({ lowlight, defaultLanguage: null }),
    Markdown,
    MarkdownPaste,
    Mention.configure({
      renderText: ({ node }) => `@${String(node.attrs.label ?? "")}`,
      HTMLAttributes: { "data-mention": "" },
      suggestion: { char: "@", items: () => [], ...mention },
    }),
    ...(emoji ? [EmojiSuggestion.configure({ suggestion: emoji })] : []),
    Extension.create({
      name: "submitOnModEnter",
      addKeyboardShortcuts() {
        return {
          "Mod-Enter": () => {
            if (!submit) return false;
            submit();
            return true;
          },
        };
      },
    }),
  ];
  if (variant === "comment") return shared;
  return [
    ...shared,
    TaskList,
    TaskItem.configure({ nested: true, a11y: { checkboxLabel: () => t.editor.taskDone } }),
    Table.configure({ resizable: false }),
    TableRow,
    HeaderCell,
    Cell,
    Panel,
    Expand,
    Columns,
    Column,
    Decision,
    HeadingAnchors,
    SlashMenu.configure({ suggestion: slash }),
    TableOfContents,
    ChildPages,
    Image.configure({ index: attachments }),
    AttachmentChip.configure({ index: attachments }),
    FileUpload.configure({ upload }),
    Hint,
    InlineComment,
    ArmatureIssue.configure({ source: armature }),
    ArmatureIssueBlock.configure({ pick: pickIssue }),
    ArmatureIssueList.configure({ pick: pickIssueList }),
    Status.configure({ edit: editInlineValue }),
    DateNode.configure({ edit: editInlineValue }),
    MathInline.configure({ edit: editInlineValue }),
    MathBlock.configure({ edit: editInlineValue }),
    Diagram,
    LinkCardNode.configure({ pick: pickLinkCard }),
    FindReplace.configure({ open: find }),
  ];
}
