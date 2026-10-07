import { useState, type ReactNode } from "react";
import { useEditorState, type Editor } from "@tiptap/react";
import { Button, Field, IconButton, Menu, SelectInput } from "@/components/ui";
import { Icon } from "@/components/icons";
import { CODE_LANGUAGES, COLUMN_LAYOUTS, FIND_SHORTCUT, TABLE_DEFAULT_COLS, TABLE_DEFAULT_ROWS } from "@/config";
import { t } from "@/i18n";
import { layoutAround, type ColumnLayoutKey } from "./columns";
import { languageLabel } from "./languages";
import type { EditorVariant } from "./extensions";
import { RovingToolbar } from "./RovingToolbar";
import { AttachButton, ImageTools } from "./AttachmentTools";
import { CELL_BACKGROUNDS, HEADING_LEVELS, PANEL_KINDS, safeHref, type PanelKind } from "./schema";
import { planSelection } from "./issueSelection";

/** The block controls a comment's toolbar keeps: those of the comment allowlist. */
const COMMENT_BLOCKS = ["bullet-list", "ordered-list", "quote", "code-block"];

// A press on a toolbar control must not take the selection out of the editor.
const keepSelection = (e: { preventDefault: () => void }) => e.preventDefault();

/**
 * The formatting bar over the editor: one control per construct the
 * allowlist names. Markdown shortcuts and the slash menu do the same from
 * the keyboard; the bar is for people who do not know them.
 */
export function EditorToolbar({
  editor,
  onCopyHeadingLink,
  variant = "page",
  onCreateIssues,
  finding = false,
  onFind,
}: {
  editor: Editor;
  onCopyHeadingLink: (anchor: string) => void;
  variant?: EditorVariant;
  /** Turns the selection into Armature issues; without it the action is not offered. */
  onCreateIssues?: () => void;
  /** Whether the find bar is open. */
  finding?: boolean;
  /** Opens or closes the find bar; without it the control is not offered. */
  onFind?: () => void;
}) {
  const state = useEditorState({
    editor,
    selector: ({ editor: e }) => ({
      bold: e.isActive("bold"),
      italic: e.isActive("italic"),
      strike: e.isActive("strike"),
      code: e.isActive("code"),
      link: e.isActive("link"),
      href: (e.getAttributes("link").href as string | undefined) ?? "",
      level: HEADING_LEVELS.find((level) => e.isActive("heading", { level })) ?? 0,
      anchor: (e.getAttributes("heading").id as string | undefined) ?? "",
      bullet: e.isActive("bulletList"),
      ordered: e.isActive("orderedList"),
      task: e.isActive("taskList"),
      quote: e.isActive("blockquote"),
      codeBlock: e.isActive("codeBlock"),
      language: (e.getAttributes("codeBlock").language as string | null | undefined) ?? "",
      table: e.isActive("table"),
      // A chart's table goes with its chart; there is no chart without one.
      charted: variant !== "comment" && e.isActive("tableChart"),
      panel: e.isActive("panel"),
      panelKind: (e.getAttributes("panel").kind as PanelKind | undefined) ?? "info",
      expand: variant !== "comment" && e.isActive("expand"),
      columnLayout: variant !== "comment" ? layoutAround(e.state) : null,
      // A comment's editor has no tables, and so neither command.
      canMerge: variant !== "comment" && e.can().mergeCells(),
      canSplit: variant !== "comment" && e.can().splitCell(),
      issueItems: onCreateIssues ? (planSelection(e.state)?.items.length ?? 0) : 0,
    }),
  });
  const [linking, setLinking] = useState(false);
  const chain = () => editor.chain().focus();

  const marks: Array<{ action: string; label: string; icon: ReactNode; on: boolean; run: () => void }> = [
    { action: "bold", label: t.editor.bold, icon: <Icon.Bold />, on: state.bold, run: () => chain().toggleBold().run() },
    { action: "italic", label: t.editor.italic, icon: <Icon.Italic />, on: state.italic, run: () => chain().toggleItalic().run() },
    { action: "strike", label: t.editor.strike, icon: <Icon.Strike />, on: state.strike, run: () => chain().toggleStrike().run() },
    { action: "code", label: t.editor.code, icon: <Icon.Code />, on: state.code, run: () => chain().toggleCode().run() },
  ];
  const allBlocks: Array<{ action: string; label: string; icon: ReactNode; on?: boolean; run: () => void }> = [
    { action: "bullet-list", label: t.editor.bulletList, icon: <Icon.Lines />, on: state.bullet, run: () => chain().toggleBulletList().run() },
    { action: "ordered-list", label: t.editor.orderedList, icon: <Icon.OrderedList />, on: state.ordered, run: () => chain().toggleOrderedList().run() },
    { action: "task-list", label: t.editor.taskList, icon: <Icon.Checklist />, on: state.task, run: () => chain().toggleTaskList().run() },
    { action: "quote", label: t.editor.quote, icon: <Icon.Quote />, on: state.quote, run: () => chain().toggleBlockquote().run() },
    { action: "code-block", label: t.editor.codeBlock, icon: <Icon.CodeBlock />, on: state.codeBlock, run: () => chain().toggleCodeBlock().run() },
    { action: "divider", label: t.editor.divider, icon: <Icon.Divider />, run: () => chain().setHorizontalRule().run() },
    {
      action: "table",
      label: t.editor.table,
      icon: <Icon.Table />,
      on: state.table,
      run: () => chain().insertTable({ rows: TABLE_DEFAULT_ROWS, cols: TABLE_DEFAULT_COLS, withHeaderRow: true }).run(),
    },
    { action: "panel", label: t.editor.panel, icon: <Icon.Panel />, on: state.panel, run: () => chain().setPanel("info").run() },
  ];

  const blocks = variant === "comment" ? allBlocks.filter((b) => COMMENT_BLOCKS.includes(b.action)) : allBlocks;
  const styleLabel = state.level ? t.editor.heading(state.level) : t.editor.paragraph;

  return (
    <div className="space-y-1 border-b border-border bg-surface px-1.5 py-1" data-editor-toolbar>
      <RovingToolbar label={t.editor.toolbar}>
        <Menu
          label={t.editor.textStyle}
          items={[
            { label: t.editor.paragraph, onSelect: () => chain().setParagraph().run(), attrs: { "data-text-style": "paragraph" } },
            ...HEADING_LEVELS.map((level) => ({
              label: t.editor.heading(level),
              onSelect: () => chain().setHeading({ level }).run(),
              attrs: { "data-text-style": `heading-${level}` },
            })),
          ]}
          trigger={(props) => (
            <Button
              variant="ghost"
              size="sm"
              aria-haspopup={props["aria-haspopup"]}
              aria-expanded={props["aria-expanded"]}
              aria-controls={props["aria-controls"]}
              aria-label={`${t.editor.textStyle}: ${styleLabel}`}
              onMouseDown={keepSelection}
              onClick={props.toggle}
              iconRight={<Icon.ChevronDown />}
              className="w-28 justify-between"
              data-editor-action="text-style"
            >
              {styleLabel}
            </Button>
          )}
        />
        <Separator />
        {marks.map((b) => (
          <IconButton
            key={b.action}
            icon={b.icon}
            label={b.label}
            size="sm"
            aria-pressed={b.on}
            data-editor-action={b.action}
            onMouseDown={keepSelection}
            onClick={b.run}
          />
        ))}
        <IconButton
          icon={<Icon.Link />}
          label={t.editor.link}
          size="sm"
          aria-pressed={state.link}
          aria-expanded={linking}
          data-editor-action="link"
          onMouseDown={keepSelection}
          onClick={() => setLinking((open) => !open)}
        />
        <Separator />
        {blocks.map((b) => (
          <IconButton
            key={b.action}
            icon={b.icon}
            label={b.label}
            size="sm"
            aria-pressed={b.on}
            data-editor-action={b.action}
            onMouseDown={keepSelection}
            onClick={b.run}
          />
        ))}
        {variant !== "comment" && <AttachButton editor={editor} />}
        {onFind && (
          <>
            <Separator />
            <IconButton
              icon={<Icon.Search />}
              label={t.editor.find.open}
              size="sm"
              aria-expanded={finding}
              aria-keyshortcuts={FIND_SHORTCUT}
              data-editor-action="find"
              onMouseDown={keepSelection}
              onClick={onFind}
            />
          </>
        )}
      </RovingToolbar>
      {linking && (
        <LinkForm
          initial={state.href}
          active={state.link}
          onDone={(href) => {
            if (href === null) chain().extendMarkRange("link").unsetLink().run();
            else if (href !== undefined) chain().extendMarkRange("link").setLink({ href }).run();
            else editor.commands.focus();
            setLinking(false);
          }}
        />
      )}
      {state.table && (
        <RovingToolbar label={t.editor.tableTools} data-editor-tools="table">
          <ToolButton label={t.editor.headerRow} action="header-row" run={() => chain().toggleHeaderRow().run()} />
          <ToolButton label={t.editor.headerColumn} action="header-column" run={() => chain().toggleHeaderColumn().run()} />
          <Separator />
          <ToolButton label={t.editor.rowAbove} action="row-above" run={() => chain().addRowBefore().run()} />
          <ToolButton label={t.editor.rowBelow} action="row-below" run={() => chain().addRowAfter().run()} />
          <ToolButton label={t.editor.deleteRow} action="delete-row" run={() => chain().deleteRow().run()} />
          <Separator />
          <ToolButton label={t.editor.columnBefore} action="column-before" run={() => chain().addColumnBefore().run()} />
          <ToolButton label={t.editor.columnAfter} action="column-after" run={() => chain().addColumnAfter().run()} />
          <ToolButton label={t.editor.deleteColumn} action="delete-column" run={() => chain().deleteColumn().run()} />
          <Separator />
          <ToolButton label={t.editor.mergeCells} action="merge-cells" disabled={!state.canMerge} run={() => chain().mergeCells().run()} />
          <ToolButton label={t.editor.splitCell} action="split-cell" disabled={!state.canSplit} run={() => chain().splitCell().run()} />
          <Menu
            label={t.editor.cellBackground}
            items={[
              {
                label: t.editor.backgrounds.none ?? "",
                onSelect: () => chain().setCellAttribute("background", null).run(),
                attrs: { "data-background-option": "none" },
              },
              ...CELL_BACKGROUNDS.map((bg) => ({
                label: t.editor.backgrounds[bg] ?? bg,
                icon: <span className="block size-3 rounded-sm border border-border-strong" data-background={bg} />,
                onSelect: () => chain().setCellAttribute("background", bg).run(),
                attrs: { "data-background-option": bg },
              })),
            ]}
            trigger={(props) => (
              <Button
                variant="ghost"
                size="sm"
                aria-haspopup={props["aria-haspopup"]}
                aria-expanded={props["aria-expanded"]}
                aria-controls={props["aria-controls"]}
                onMouseDown={keepSelection}
                onClick={props.toggle}
                iconRight={<Icon.ChevronDown />}
                data-editor-action="cell-background"
              >
                {t.editor.cellBackground}
              </Button>
            )}
          />
          <Separator />
          {variant !== "comment" &&
            (state.charted ? (
              <ToolButton label={t.tableChart.unchart} action="unchart-table" run={() => chain().unchartTable().run()} />
            ) : (
              <ToolButton label={t.tableChart.chart} action="chart-table" run={() => chain().chartTable().run()} />
            ))}
          <ToolButton
            label={state.charted ? t.tableChart.deleteChart : t.editor.deleteTable}
            action="delete-table"
            run={() => (state.charted ? chain().unchartTable().deleteTable().run() : chain().deleteTable().run())}
          />
        </RovingToolbar>
      )}
      {state.codeBlock && (
        <RovingToolbar label={t.editor.codeTools} data-editor-tools="code">
          <SelectInput
            controlSize="sm"
            aria-label={t.editor.language}
            value={state.language}
            onChange={(e) =>
              chain()
                .updateAttributes("codeBlock", { language: e.target.value || null })
                .run()
            }
            data-editor-action="language"
          >
            <option value="">{t.editor.plainText}</option>
            {CODE_LANGUAGES.map((language) => (
              <option key={language.id} value={language.id}>
                {language.label}
              </option>
            ))}
            {state.language && !CODE_LANGUAGES.some((language) => language.id === state.language) && (
              <option value={state.language}>{languageLabel(state.language)}</option>
            )}
          </SelectInput>
        </RovingToolbar>
      )}
      {state.panel && (
        <RovingToolbar label={t.editor.panelTools} data-editor-tools="panel">
          <SelectInput
            controlSize="sm"
            aria-label={t.editor.panelKind}
            value={state.panelKind}
            onChange={(e) =>
              chain()
                .setPanelKind(e.target.value as PanelKind)
                .run()
            }
            data-editor-action="panel-kind"
          >
            {PANEL_KINDS.map((kind) => (
              <option key={kind} value={kind}>
                {t.editor.panels[kind]}
              </option>
            ))}
          </SelectInput>
          <ToolButton label={t.editor.removePanel} action="remove-panel" run={() => chain().unsetPanel().run()} />
        </RovingToolbar>
      )}
      {state.expand && (
        <RovingToolbar label={t.editor.expandTools} data-editor-tools="expand">
          <ToolButton label={t.editor.removeExpand} action="remove-expand" run={() => chain().unsetExpand().run()} />
        </RovingToolbar>
      )}
      {state.columnLayout && (
        <RovingToolbar label={t.editor.columnTools} data-editor-tools="columns">
          <SelectInput
            controlSize="sm"
            aria-label={t.editor.columnLayout}
            value={state.columnLayout}
            onChange={(e) =>
              chain()
                .setColumnLayout(e.target.value as ColumnLayoutKey)
                .run()
            }
            data-editor-action="column-layout"
          >
            {COLUMN_LAYOUTS.map((layout) => (
              <option key={layout.key} value={layout.key}>
                {t.editor.columnLayouts[layout.key]}
              </option>
            ))}
            {state.columnLayout === "custom" && (
              <option value="custom" disabled>
                {t.editor.columnLayouts.custom}
              </option>
            )}
          </SelectInput>
          <ToolButton label={t.editor.removeColumns} action="remove-columns" run={() => chain().unsetColumns().run()} />
        </RovingToolbar>
      )}
      {onCreateIssues && state.issueItems > 0 && (
        <RovingToolbar label={t.armature.create.tools} data-editor-tools="selection">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            icon={<Icon.Task />}
            onMouseDown={keepSelection}
            onClick={onCreateIssues}
            data-editor-action="create-issues"
          >
            {t.armature.create.action(state.issueItems)}
          </Button>
        </RovingToolbar>
      )}
      <ImageTools editor={editor} />
      {state.level > 0 && state.anchor && (
        <RovingToolbar label={t.editor.headingTools} data-editor-tools="heading">
          <Button
            variant="ghost"
            size="sm"
            icon={<Icon.Hash />}
            onMouseDown={keepSelection}
            onClick={() => onCopyHeadingLink(state.anchor)}
            data-editor-action="copy-heading-link"
          >
            {t.editor.copyHeadingLink}
          </Button>
        </RovingToolbar>
      )}
    </div>
  );
}

function Separator() {
  return <span aria-hidden="true" className="mx-1 h-4 w-px bg-border" />;
}

function ToolButton({ label, action, run, disabled }: { label: string; action: string; run: () => void; disabled?: boolean }) {
  return (
    <Button variant="ghost" size="sm" disabled={disabled} onMouseDown={keepSelection} onClick={run} data-editor-action={action}>
      {label}
    </Button>
  );
}

/** Sets, changes or removes the link on the selection: undefined cancels, null removes. */
function LinkForm({ initial, active, onDone }: { initial: string; active: boolean; onDone: (href: string | null | undefined) => void }) {
  const [href, setHref] = useState(initial);
  const trimmed = href.trim();
  const safe = safeHref(trimmed);
  return (
    <form
      className="flex flex-wrap items-end gap-2 px-1 py-1"
      data-link-form
      onSubmit={(event) => {
        event.preventDefault();
        if (safe) onDone(safe);
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          onDone(undefined);
        }
      }}
    >
      <Field
        label={t.editor.linkAddress}
        id="editor-link-address"
        value={href}
        onChange={(e) => setHref(e.target.value)}
        placeholder={t.editor.linkPlaceholder}
        hint={trimmed && !safe ? undefined : t.editor.linkHint}
        error={trimmed && !safe ? t.editor.linkInvalid : undefined}
        className="w-72"
        // biome-ignore lint/a11y/noAutofocus: the form opens because the reader asked to type an address
        autoFocus
      />
      <Button type="submit" size="sm" disabled={!safe}>
        {t.editor.setLink}
      </Button>
      {active && (
        <Button type="button" size="sm" variant="ghost" onClick={() => onDone(null)}>
          {t.editor.removeLink}
        </Button>
      )}
    </form>
  );
}
