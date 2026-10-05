import { useContext, useEffect, useId, useRef, useState } from "react";
import { EditorContent, useEditor } from "@tiptap/react";
import type { JSONContent, Editor as TiptapEditor } from "@tiptap/core";
import type { MentionNodeAttrs } from "@tiptap/extension-mention";
import { EDITOR_LINE_HEIGHT_PX, EDITOR_MIN_ROWS, MENTION_MAX_SUGGESTIONS, MENTION_SEARCH_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";
import { useCopyHeadingLink } from "./CopyHeadingLink";
import { editorExtensions, fitSchema, sanitizePasted, type EditorVariant } from "./extensions";
import { MentionList, mentionMatches } from "./MentionList";
import { emptyDoc, isEmptyDoc, type Doc, type Mentionable, type MentionSource } from "./schema";
import { SlashMenu } from "./SlashMenu";
import { filterSlashItems, slashItemsFor, type SlashItem } from "./slashItems";
import { EditorToolbar } from "./Toolbar";
import { FindBar } from "./FindBar";
import { selectedQuery } from "./findReplace";
import { useSuggestion } from "./useSuggestion";
import type { AttachmentIndex } from "./attachmentIndex";
import type { UploadFile } from "./attachments";
import type { IssueSource } from "./armatureIssue";
import { IssuePicker } from "@/features/armature/IssuePicker";
import { IssueListDialog } from "@/features/armature/IssueListDialog";
import { IssueChartDialog } from "@/features/armature/IssueChartDialog";
import { newChartSettings } from "@/features/armature/chart";
import { IssueRoadmapDialog } from "@/features/armature/IssueRoadmapDialog";
import { newRoadmapSettings } from "@/features/armature/roadmap";
import { PropertiesReportDialog } from "@/features/properties/PropertiesReportDialog";
import { PageListDialog } from "@/features/pageLists/PageListDialog";
import { labelledSettings } from "@/features/pageLists/lists";
import { TaskReportDialog } from "@/features/taskReport/TaskReportDialog";
import { taskReportSettings } from "@/features/taskReport/report";
import { CalendarDialog } from "@/features/calendar/CalendarDialog";
import { TemplateButtonDialog } from "@/features/templateButton/TemplateButtonDialog";
import { templateButtonSettings } from "@/features/templateButton/button";
import { calendarSettings } from "@/features/calendar/calendar";
import { CreateIssuesDialog } from "@/features/armature/CreateIssuesDialog";
import { placeChips, planSelection, type SelectionPlan } from "./issueSelection";
import { ARMATURE_DEFAULT_COLUMNS, ARMATURE_LIST_DEFAULT_LIMIT } from "@/config";
import type { InlineValueTarget } from "./inlineValues";
import { DateDialog, MathDialog, StatusDialog } from "./InlineValueDialogs";
import { LinkCardDialog } from "./LinkCardDialog";
import { ExcerptPicker } from "@/features/pages/ExcerptPicker";
import { DocPageContext } from "./BlockViews";
import type { Emoji } from "./emoji";
import { EmojiList } from "./EmojiList";

// The dialog held the page still, but a node that is no longer where it was
// opened is left as it is rather than changing whatever is there now. The
// caret goes on after an inline node; a block, with no text to put it in,
// stays selected.
function changeInlineValue(editor: TiptapEditor, target: InlineValueTarget, attrs: InlineValueTarget["attrs"]) {
  const node = editor.state.doc.nodeAt(target.pos);
  if (node?.type.name !== target.kind) return;
  const chain = editor
    .chain()
    .focus()
    .command(({ tr }) => {
      tr.setNodeMarkup(target.pos, undefined, { ...node.attrs, ...attrs });
      return true;
    });
  (node.isInline ? chain.setTextSelection(target.pos + node.nodeSize) : chain.setNodeSelection(target.pos)).run();
}

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
  /** Who an at sign can name, when they are known up front. */
  people?: Mentionable[];
  /** Looks up who an at sign can name as the person types; it takes the place of people. */
  mentionSource?: MentionSource;
  placeholder?: string;
  autoFocus?: boolean;
  rows?: number;
  /** Ctrl or Cmd with Enter submits the surrounding form. */
  onSubmit?: () => void;
  "aria-label"?: string;
  handle?: (handle: EditorHandle) => void;
  /** Takes dropped, pasted and picked files to the page; without it the editor takes none. */
  upload?: UploadFile;
  /** Which files the page still has, so a deleted one is drawn as missing. */
  attachments?: AttachmentIndex;
  /** A comment's editor offers text and its structure, and no slash menu. */
  variant?: EditorVariant;
  /** What turns typed keys and pasted issue addresses into Armature chips. */
  armature?: IssueSource;
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
  mentionSource,
  placeholder = t.editor.placeholder,
  autoFocus = false,
  rows = EDITOR_MIN_ROWS,
  onSubmit,
  "aria-label": ariaLabel = t.editor.label,
  handle,
  upload,
  attachments,
  variant = "page",
  armature,
}: EditorProps) {
  // The page being edited, so an include starts in its space and never shows the page itself.
  const page = useContext(DocPageContext);
  const slashId = useId();
  const mentionId = useId();
  const submitRef = useRef(onSubmit);
  submitRef.current = onSubmit;
  const peopleRef = useRef(people ?? []);
  peopleRef.current = people ?? [];
  // The last people a lookup found, so each keystroke narrows them at once
  // while the next lookup is on its way.
  const foundRef = useRef<Mentionable[]>([]);
  const searchesRef = useRef(false);
  searchesRef.current = mentionSource !== undefined;
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const armatureRef = useRef(armature);
  armatureRef.current = armature;
  const [pickingIssue, setPickingIssue] = useState(false);
  const [makingList, setMakingList] = useState(false);
  const [makingChart, setMakingChart] = useState(false);
  const [makingRoadmap, setMakingRoadmap] = useState(false);
  const [makingReport, setMakingReport] = useState(false);
  const [makingLabelled, setMakingLabelled] = useState(false);
  const [makingTasks, setMakingTasks] = useState(false);
  const [makingCalendar, setMakingCalendar] = useState(false);
  const [makingButton, setMakingButton] = useState(false);
  const [pickingLink, setPickingLink] = useState(false);
  const [pickingInclude, setPickingInclude] = useState(false);
  const [filing, setFiling] = useState<SelectionPlan | null>(null);
  const [editingValue, setEditingValue] = useState<InlineValueTarget | null>(null);
  const emojiId = useId();
  const emoji = useSuggestion<Emoji>((item) => item);
  // The words to look for when the bar opens, and a count that tells an open
  // bar to take focus again on a second Ctrl or Cmd+F.
  const [finding, setFinding] = useState<{ seed: string; token: number } | null>(null);
  const openFind = (seed: string) => setFinding((was) => ({ seed, token: (was?.token ?? 0) + 1 }));

  const slash = useSuggestion<SlashItem>((item) => item);
  const mention = useSuggestion<Mentionable, MentionNodeAttrs>((person) => ({ id: person.id, label: person.name }));
  const { copy, status } = useCopyHeadingLink();

  const editor = useEditor({
    extensions: editorExtensions({
      variant,
      placeholder,
      submit: () => submitRef.current?.(),
      upload,
      attachments,
      armature,
      pickIssue: () => setPickingIssue(true),
      pickIssueList: () => setMakingList(true),
      pickChart: () => setMakingChart(true),
      pickRoadmap: () => setMakingRoadmap(true),
      pickPropertiesReport: () => setMakingReport(true),
      pickLabelledPages: () => setMakingLabelled(true),
      pickTaskReport: () => setMakingTasks(true),
      pickCalendar: () => setMakingCalendar(true),
      pickTemplateButton: () => setMakingButton(true),
      pickLinkCard: () => setPickingLink(true),
      pickInclude: () => setPickingInclude(true),
      pageId: page?.id,
      editInlineValue: setEditingValue,
      emoji: { render: emoji.renderer },
      find: openFind,
      slash: { items: ({ query }) => filterSlashItems(query, slashItemsFor(Boolean(armatureRef.current?.baseUrl()))), render: slash.renderer },
      mention: {
        items: ({ query }) => mentionMatches(searchesRef.current ? foundRef.current : peopleRef.current, query).slice(0, MENTION_MAX_SUGGESTIONS),
        render: mention.renderer,
      },
    }),
    content: (value ?? emptyDoc) as JSONContent,
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

  // TipTap's own autofocus runs on a timer set by the render that builds the
  // editor. React may commit that render later, as when Suspense holds back
  // the editor's first showing, and the focus then lands on a view that is
  // not in the page yet. An effect runs only once it is.
  useEffect(() => {
    if (autoFocus && editor && !editor.isDestroyed) editor.commands.focus("end");
  }, [autoFocus, editor]);

  const mentionQuery = mention.open ? mention.open.query : null;
  const replaceMentions = mention.replace;
  useEffect(() => {
    if (!mentionSource || mentionQuery === null) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      mentionSource(mentionQuery, controller.signal).then(
        (found) => {
          if (controller.signal.aborted) return;
          foundRef.current = found;
          replaceMentions(found.slice(0, MENTION_MAX_SUGGESTIONS));
        },
        () => {},
      );
    }, MENTION_SEARCH_DEBOUNCE_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [mentionSource, mentionQuery, replaceMentions]);

  // The list under the caret belongs to the editable element, which keeps
  // focus; these attributes tell a screen reader which option is current.
  const controls = slash.open ? slashId : mention.open && mention.open.items.length > 0 ? mentionId : emoji.open && emoji.open.rect ? emojiId : null;
  const activeOption = !controls
    ? null
    : controls === slashId
      ? slash.open?.items.length
        ? `${slashId}-${slash.active}`
        : null
      : controls === emojiId
        ? emoji.open?.items.length
          ? `${emojiId}-${emoji.active}`
          : null
        : `${mentionId}-${mention.active}`;
  useEffect(() => {
    // Between an unmount and the next mount, as when a Suspense boundary hides
    // and shows the editor again, TipTap throws on any access to the view.
    const dom = editor?.isInitialized ? editor.view.dom : null;
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
            .insertContent(fitSchema(sanitizePasted(parsed), editor.schema).content ?? [])
            .run();
      },
      clear: () => {
        editor.commands.clearContent(true);
      },
    });
  }, [handle, editor]);

  return (
    <div className="rounded-control border border-border bg-surface" data-editor-frame>
      {editor && (
        <EditorToolbar
          editor={editor}
          onCopyHeadingLink={copy}
          variant={variant}
          onCreateIssues={armature?.canCreate?.() ? () => setFiling(planSelection(editor.state)) : undefined}
          finding={finding !== null}
          onFind={variant === "page" ? () => (finding ? setFinding(null) : openFind(selectedQuery(editor.state))) : undefined}
        />
      )}
      {finding && editor && <FindBar editor={editor} seed={finding.seed} focusToken={finding.token} onClose={() => setFinding(null)} />}
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
      {emoji.open && (
        <EmojiList id={emojiId} items={emoji.open.items} active={emoji.active} rect={emoji.open.rect} onHover={emoji.setActive} onPick={emoji.open.pick} />
      )}
      {status}
      {editingValue?.kind === "status" && editor && (
        <StatusDialog
          initial={editingValue.attrs}
          onClose={() => setEditingValue(null)}
          onSave={(attrs) => {
            setEditingValue(null);
            changeInlineValue(editor, editingValue, attrs);
          }}
        />
      )}
      {editingValue?.kind === "date" && editor && (
        <DateDialog
          initial={editingValue.attrs.date}
          onClose={() => setEditingValue(null)}
          onSave={(date) => {
            setEditingValue(null);
            changeInlineValue(editor, editingValue, { date });
          }}
        />
      )}
      {(editingValue?.kind === "mathInline" || editingValue?.kind === "mathBlock") && editor && (
        <MathDialog
          initial={editingValue.attrs.latex}
          display={editingValue.kind === "mathBlock"}
          onClose={() => setEditingValue(null)}
          onSave={(latex) => {
            setEditingValue(null);
            changeInlineValue(editor, editingValue, { latex });
          }}
        />
      )}
      {pickingIssue && editor && (
        <IssuePicker
          onClose={() => setPickingIssue(false)}
          onInsert={(key) => {
            setPickingIssue(false);
            editor.chain().focus().insertArmatureIssueBlock(key).run();
          }}
        />
      )}
      {pickingInclude && editor && (
        <ExcerptPicker
          initialSpaceKey={page?.spaceKey}
          excludePageId={page?.id}
          onClose={() => setPickingInclude(false)}
          onPick={(choice) => {
            setPickingInclude(false);
            editor.chain().focus().insertInclude({ pageId: choice.pageId, excerptId: choice.excerptId }).run();
          }}
        />
      )}
      {pickingLink && editor && (
        <LinkCardDialog
          onClose={() => setPickingLink(false)}
          onInsert={(url) => {
            setPickingLink(false);
            editor.chain().focus().insertLinkCard(url).run();
          }}
        />
      )}
      {makingChart && editor && (
        <IssueChartDialog
          initial={newChartSettings()}
          isNew
          onClose={() => setMakingChart(false)}
          onSave={(settings) => {
            setMakingChart(false);
            editor.chain().focus().insertArmatureChart(settings).run();
          }}
        />
      )}
      {makingRoadmap && editor && (
        <IssueRoadmapDialog
          initial={newRoadmapSettings()}
          isNew
          onClose={() => setMakingRoadmap(false)}
          onSave={(settings) => {
            setMakingRoadmap(false);
            editor.chain().focus().insertArmatureRoadmap(settings).run();
          }}
        />
      )}
      {makingLabelled && editor && (
        <PageListDialog
          kind="labelled"
          initial={labelledSettings({})}
          isNew
          onClose={() => setMakingLabelled(false)}
          onSave={(settings) => {
            setMakingLabelled(false);
            editor.chain().focus().insertLabelledPages(settings).run();
          }}
        />
      )}
      {makingTasks && editor && (
        <TaskReportDialog
          initial={taskReportSettings({})}
          isNew
          onClose={() => setMakingTasks(false)}
          onSave={(settings) => {
            setMakingTasks(false);
            editor.chain().focus().insertTaskReport(settings).run();
          }}
        />
      )}
      {makingCalendar && editor && (
        <CalendarDialog
          initial={calendarSettings({})}
          spaceKey={page?.spaceKey ?? null}
          isNew
          onClose={() => setMakingCalendar(false)}
          onSave={(settings) => {
            setMakingCalendar(false);
            editor.chain().focus().insertCalendar(settings).run();
          }}
        />
      )}
      {makingButton && editor && (
        <TemplateButtonDialog
          initial={templateButtonSettings({})}
          pageSpace={page?.spaceKey ?? null}
          isNew
          onClose={() => setMakingButton(false)}
          onSave={(settings) => {
            setMakingButton(false);
            editor.chain().focus().insertTemplateButton(settings).run();
          }}
        />
      )}
      {makingReport && editor && (
        <PropertiesReportDialog
          initial={{ labels: [], space: null, columns: [] }}
          isNew
          onClose={() => setMakingReport(false)}
          onSave={(settings) => {
            setMakingReport(false);
            editor.chain().focus().insertPropertiesReport(settings).run();
          }}
        />
      )}
      {makingList && editor && (
        <IssueListDialog
          initial={{ query: "", columns: [...ARMATURE_DEFAULT_COLUMNS], limit: ARMATURE_LIST_DEFAULT_LIMIT }}
          isNew
          onClose={() => setMakingList(false)}
          onSave={(settings) => {
            setMakingList(false);
            editor.chain().focus().insertArmatureIssueList(settings).run();
          }}
        />
      )}
      {filing && editor && (
        <CreateIssuesDialog
          plan={filing}
          pageId={armature?.pageId?.() ?? ""}
          onClose={() => {
            setFiling(null);
            editor.commands.focus();
          }}
          onCreated={(keys) => {
            // The dialog holds the page still; a document that changed anyway
            // keeps its text, since the places no longer fit it.
            if (editor.state.doc.eq(filing.doc)) editor.view.dispatch(placeChips(editor.state.tr, filing, keys));
          }}
        />
      )}
    </div>
  );
}
