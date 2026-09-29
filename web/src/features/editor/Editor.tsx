import { useEffect, useId, useRef } from "react";
import { EditorContent, useEditor } from "@tiptap/react";
import type { JSONContent } from "@tiptap/core";
import type { MentionNodeAttrs } from "@tiptap/extension-mention";
import { EDITOR_LINE_HEIGHT_PX, EDITOR_MIN_ROWS, MENTION_MAX_SUGGESTIONS } from "@/config";
import { t } from "@/i18n";
import { useCopyHeadingLink } from "./CopyHeadingLink";
import { editorExtensions, sanitizePasted } from "./extensions";
import { MentionList, mentionMatches } from "./MentionList";
import { emptyDoc, isEmptyDoc, type Doc, type Mentionable } from "./schema";
import { SlashMenu } from "./SlashMenu";
import { filterSlashItems, type SlashItem } from "./slashItems";
import { EditorToolbar } from "./Toolbar";
import { useSuggestion } from "./useSuggestion";

/** What a form may do to the editor from outside: put words in, or empty it. */
export interface EditorHandle {
  insertMarkdown: (text: string) => void;
  clear: () => void;
}

export interface EditorProps {
  /** The id the suite types into: the editable element's. */
  id: string;
  value: Doc | null;
  /** Null when the document says nothing, so a blank page stores nothing. */
  onChange: (doc: Doc | null) => void;
  /** Who an at sign can name; without it the at sign offers nobody. */
  people?: Mentionable[];
  placeholder?: string;
  autoFocus?: boolean;
  rows?: number;
  /** Ctrl or Cmd with Enter submits the surrounding form. */
  onSubmit?: () => void;
  "aria-label"?: string;
  handle?: (handle: EditorHandle) => void;
}

/**
 * The page editor: formatting, markdown as you type and on paste, a slash
 * menu of blocks, tables, highlighted code, panels and heading anchors.
 */
export function Editor({
  id,
  value,
  onChange,
  people,
  placeholder = t.editor.placeholder,
  autoFocus = false,
  rows = EDITOR_MIN_ROWS,
  onSubmit,
  "aria-label": ariaLabel = t.editor.label,
  handle,
}: EditorProps) {
  const slashId = useId();
  const mentionId = useId();
  const submitRef = useRef(onSubmit);
  submitRef.current = onSubmit;
  const peopleRef = useRef(people ?? []);
  peopleRef.current = people ?? [];
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  const slash = useSuggestion<SlashItem>((item) => item);
  const mention = useSuggestion<Mentionable, MentionNodeAttrs>((person) => ({ id: person.id, label: person.name }));
  const { copy, status } = useCopyHeadingLink();

  const editor = useEditor({
    extensions: editorExtensions({
      placeholder,
      submit: () => submitRef.current?.(),
      slash: { items: ({ query }) => filterSlashItems(query), render: slash.renderer },
      mention: {
        items: ({ query }) => mentionMatches(peopleRef.current, query).slice(0, MENTION_MAX_SUGGESTIONS),
        render: mention.renderer,
      },
    }),
    content: (value ?? emptyDoc) as JSONContent,
    autofocus: autoFocus ? "end" : false,
    editorProps: {
      attributes: {
        id,
        role: "textbox",
        "aria-multiline": "true",
        "aria-label": ariaLabel,
        "data-editor": "rich",
        class: "doc-content min-h-0 px-4 py-3 outline-none",
        style: `min-height: ${rows * EDITOR_LINE_HEIGHT_PX}px`,
      },
    },
    onUpdate: ({ editor: e }) => {
      const json = e.getJSON() as Doc;
      onChangeRef.current(isEmptyDoc(json) ? null : json);
    },
  });

  // The list under the caret belongs to the editable element, which keeps
  // focus; these attributes tell a screen reader which option is current.
  const controls = slash.open ? slashId : mention.open && mention.open.items.length > 0 ? mentionId : null;
  const activeOption = !controls ? null : controls === slashId ? (slash.open?.items.length ? `${slashId}-${slash.active}` : null) : `${mentionId}-${mention.active}`;
  useEffect(() => {
    const dom = editor?.view.dom;
    if (!dom) return;
    const set = (name: string, value: string | null) => (value === null ? dom.removeAttribute(name) : dom.setAttribute(name, value));
    set("aria-controls", controls);
    set("aria-autocomplete", controls && "list");
    set("aria-activedescendant", activeOption);
  }, [editor, controls, activeOption]);

  useEffect(() => {
    if (!handle || !editor) return;
    handle({
      insertMarkdown: (markdown) => {
        const parsed = editor.markdown?.parse(markdown) as JSONContent | undefined;
        if (parsed?.content?.length)
          editor
            .chain()
            .focus("end")
            .insertContent(sanitizePasted(parsed).content ?? [])
            .run();
      },
      clear: () => {
        editor.commands.clearContent(true);
      },
    });
  }, [handle, editor]);

  return (
    <div className="rounded-control border border-border bg-surface" data-editor-frame>
      {editor && <EditorToolbar editor={editor} onCopyHeadingLink={copy} />}
      <EditorContent editor={editor} />
      {slash.open && (
        <SlashMenu id={slashId} items={slash.open.items} active={slash.active} rect={slash.open.rect} onHover={slash.setActive} onPick={slash.open.pick} />
      )}
      {mention.open && (
        <MentionList
          id={mentionId}
          items={mention.open.items}
          active={mention.active}
          rect={mention.open.rect}
          onHover={mention.setActive}
          onPick={mention.open.pick}
        />
      )}
      {status}
    </div>
  );
}
