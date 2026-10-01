import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useEditorState, type Editor } from "@tiptap/react";
import type { Transaction } from "@tiptap/pm/state";
import { Button, Checkbox, IconButton, Input } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { findKey, replaceAll, replaceCurrent, selectCurrent, setFindQuery, stepMatch } from "./findReplace";

/**
 * The find and replace bar over the editor. The editor keeps the matches;
 * the bar asks it to look, step and replace, and says how many it found.
 */
export function FindBar({ editor, seed, focusToken, onClose }: { editor: Editor; seed: string; focusToken: number; onClose: () => void }) {
  const [query, setQuery] = useState(seed);
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [replacement, setReplacement] = useState("");
  const [replaced, setReplaced] = useState<number | null>(null);
  const queryRef = useRef<HTMLInputElement>(null);
  const found = useEditorState({
    editor,
    selector: ({ editor: e }) => {
      const state = findKey.getState(e.state);
      return { count: state?.matches.length ?? 0, current: state?.current ?? -1 };
    },
  });

  // A seed or a second Ctrl or Cmd+F brings the reader back to the query.
  // biome-ignore lint/correctness/useExhaustiveDependencies: focusToken changes on each press, which is the point
  useEffect(() => {
    if (seed) setQuery(seed);
    queryRef.current?.focus();
    queryRef.current?.select();
  }, [seed, focusToken]);

  useEffect(() => {
    editor.view.dispatch(setFindQuery(editor.state, query, caseSensitive));
    setReplaced(null);
  }, [editor, query, caseSensitive]);

  useEffect(
    () => () => {
      // A Suspense boundary that hides the editor takes its view away first.
      if (editor.isInitialized && !editor.isDestroyed) editor.view.dispatch(setFindQuery(editor.state, "", false));
    },
    [editor],
  );

  function run(tr: Transaction | null) {
    if (!tr) return;
    editor.view.dispatch(tr);
    // The view scrolls to a selection only while it has focus, and the reader
    // is typing in the bar; the current match may also be split across marks.
    editor.view.dom.querySelector('[data-find-match="current"]')?.scrollIntoView?.({ block: "center" });
  }

  function step(direction: 1 | -1) {
    run(stepMatch(editor.state, direction));
  }

  // A replace that leaves nothing to find disables the button just pressed,
  // which would drop focus to the page; the query is where to go on from.
  function keepFocus() {
    if (!findKey.getState(editor.state)?.matches.length) queryRef.current?.focus();
  }

  function replaceOne() {
    run(replaceCurrent(editor.state, replacement));
    run(selectCurrent(editor.state));
    keepFocus();
  }

  function replaceEvery() {
    const done = replaceAll(editor.state, replacement);
    if (!done) return;
    editor.view.dispatch(done.tr);
    setReplaced(done.count);
    keepFocus();
  }

  function close() {
    const tr = selectCurrent(editor.state);
    if (tr) editor.view.dispatch(tr);
    onClose();
    editor.view.focus();
  }

  function onKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      close();
    } else if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
      event.preventDefault();
      queryRef.current?.focus();
      queryRef.current?.select();
    }
  }

  const status =
    replaced !== null
      ? t.editor.find.replaced(replaced)
      : !query
        ? ""
        : found.count === 0
          ? caseSensitive
            ? t.editor.find.noneMatchingCase
            : t.editor.find.none
          : t.editor.find.count(found.current + 1, found.count);
  const none = found.count === 0;

  return (
    <search
      // Spelled out for the browsers and readers that predate the element.
      // biome-ignore lint/a11y/noRedundantRoles: see above
      role="search"
      aria-label={t.editor.find.label}
      className="flex flex-wrap items-center gap-x-2 gap-y-1.5 border-b border-border bg-surface px-2 py-1.5"
      data-find-bar
      onKeyDown={onKeyDown}
    >
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <Input
          ref={queryRef}
          controlSize="sm"
          type="search"
          aria-label={t.editor.find.query}
          placeholder={t.editor.find.query}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              step(e.shiftKey ? -1 : 1);
            }
          }}
          className="w-44"
          data-find-query
        />
        <IconButton icon={<Icon.ChevronUp />} label={t.editor.find.previous} size="sm" disabled={none} onClick={() => step(-1)} data-find-action="previous" />
        <IconButton icon={<Icon.ChevronDown />} label={t.editor.find.next} size="sm" disabled={none} onClick={() => step(1)} data-find-action="next" />
        <Checkbox label={t.editor.find.matchCase} checked={caseSensitive} onChange={(e) => setCaseSensitive(e.target.checked)} data-find-case />
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <Input
          controlSize="sm"
          aria-label={t.editor.find.replacement}
          placeholder={t.editor.find.replacement}
          value={replacement}
          onChange={(e) => setReplacement(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              replaceOne();
            }
          }}
          className="w-44"
          data-find-replacement
        />
        <Button size="sm" variant="secondary" disabled={none} onClick={replaceOne} data-find-action="replace">
          {t.editor.find.replace}
        </Button>
        <Button size="sm" variant="secondary" disabled={none} onClick={replaceEvery} data-find-action="replace-all">
          {t.editor.find.replaceAll}
        </Button>
      </div>
      <p role="status" className="min-w-0 flex-1 text-sm text-ink-muted" data-find-status>
        {status}
      </p>
      <IconButton icon={<Icon.X />} label={t.editor.find.close} size="sm" onClick={close} data-find-action="close" />
    </search>
  );
}
