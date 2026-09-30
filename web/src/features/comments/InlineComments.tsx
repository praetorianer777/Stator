import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useInlineThreads, useResolveThread, useStartInlineThread, type Thread } from "@/api/comments";
import type { Page } from "@/api/pages";
import { Button, ErrorBanner, IconButton, SectionTitle } from "@/components/ui";
import { Icon } from "@/components/icons";
import { INLINE_COMMENT_BUTTON_GAP_PX, INLINE_COMMENT_KEY_CODE, INLINE_COMMENT_SHORTCUT, INLINE_QUOTE_SHOWN_LENGTH } from "@/config";
import { PassagesContext, type Passages } from "@/features/editor/passages";
import { t } from "@/i18n";
import { Composer, ThreadView } from "./CommentsSection";
import { markPassage, relocate, selectedPassage, type Selected } from "./passages";

const PANEL_ID = "passage-panel";
const SECTION_ID = "passages";

function shortQuote(quote: string): string {
  return quote.length > INLINE_QUOTE_SHOWN_LENGTH ? `${quote.slice(0, INLINE_QUOTE_SHOWN_LENGTH)}...` : quote;
}

function Quote({ thread }: { thread: Thread }) {
  return (
    <figure className="mb-2" data-thread-quote="">
      <figcaption className="text-2xs font-medium text-ink-subtle">{t.comments.inline.quote}</figcaption>
      <blockquote className="border-l-2 border-warning pl-2 text-sm text-ink-muted italic">{shortQuote(thread.anchor?.quote ?? "")}</blockquote>
      {thread.resolved && (
        <p className="mt-1 text-2xs text-ink-muted" data-resolved-by="">
          {t.comments.inline.resolvedBy(thread.resolvedByName)}
        </p>
      )}
    </figure>
  );
}

function focusSoon(selector: string) {
  requestAnimationFrame(() => document.querySelector<HTMLElement>(selector)?.focus());
}

/**
 * A page's document with its threads on passages: the passages highlighted,
 * a button and a shortcut to comment on selected text, the open thread in a
 * panel beside the page (below the text on narrow screens), and, after
 * `below`, the passages' threads listed for the keyboard, detached and
 * resolved ones included.
 */
export function InlineComments({ page, thread, children, below }: { page: Page; thread?: string; children: ReactNode; below: ReactNode }) {
  const enabled = !page.unpublished;
  const { data, error, refetch } = useInlineThreads(page.id, enabled);
  const threads = useMemo(() => (data ?? []).filter((each) => each.kind === "inline"), [data]);
  const canComment = page.can.comment && enabled;
  const [active, setActive] = useState<string>();
  const [showResolved, setShowResolved] = useState(false);
  const [selected, setSelected] = useState<{ passage: Selected; top: number; left: number } | null>(null);
  const [composing, setComposing] = useState<{ passage: Selected; threadId: string } | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const body = page.body;
  const start = useStartInlineThread(page.id, () => body);
  const resolve = useResolveThread(page.id);

  const anchored = threads.filter((each) => each.anchor?.state === "anchored");
  const open = anchored.filter((each) => !each.resolved);
  const detached = threads.filter((each) => each.anchor?.state === "detached").sort((a, b) => Number(a.resolved) - Number(b.resolved));
  const resolved = threads.filter((each) => each.resolved);
  const activeThread = open.find((each) => each.id === active);

  const openThread = useCallback(
    (id: string) => {
      const found = threads.find((each) => each.id === id);
      if (!found) return;
      if (found.anchor?.state === "anchored" && !found.resolved) {
        setComposing(null);
        setActive(id);
        focusSoon(`#${PANEL_ID}`);
        return;
      }
      if (found.resolved) setShowResolved(true);
      focusSoon(`#${SECTION_ID} [data-thread="${CSS.escape(id)}"]`);
    },
    [threads],
  );

  const shownIds = useMemo(
    () => new Set(threads.filter((each) => each.anchor?.state === "anchored" && (showResolved || !each.resolved)).map((each) => each.id)),
    [threads, showResolved],
  );
  const passages: Passages = useMemo(() => ({ shown: shownIds, active, open: openThread }), [shownIds, active, openThread]);

  // The selection is read after the browser has settled it, once per frame.
  useEffect(() => {
    if (!canComment) return;
    let frame = 0;
    const read = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const root = rootRef.current;
        const selection = window.getSelection();
        const passage = root ? selectedPassage(root, body, selection) : null;
        if (!root || !passage || !selection) {
          setSelected(null);
          return;
        }
        const box = selection.getRangeAt(0).getBoundingClientRect();
        const frameBox = root.getBoundingClientRect();
        setSelected({ passage, top: box.bottom - frameBox.top + INLINE_COMMENT_BUTTON_GAP_PX, left: Math.max(0, box.right - frameBox.left) });
      });
    };
    document.addEventListener("selectionchange", read);
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener("selectionchange", read);
    };
  }, [canComment, body]);

  const compose = useCallback(
    (passage: Selected) => {
      start.reset();
      setActive(undefined);
      setComposing({ passage, threadId: crypto.randomUUID() });
      setSelected(null);
    },
    [start],
  );

  useEffect(() => {
    if (!canComment) return;
    const onKey = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || !event.altKey || event.code !== INLINE_COMMENT_KEY_CODE || !selected) return;
      event.preventDefault();
      compose(selected.passage);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [canComment, selected, compose]);

  // A thread named in the address opens once its list has arrived.
  const led = useRef<string>(undefined);
  useEffect(() => {
    if (!thread || !data || led.current === thread) return;
    if (!data.some((each) => each.id === thread)) return;
    led.current = thread;
    openThread(thread);
  }, [thread, data, openThread]);

  const closePanel = () => {
    setActive(undefined);
    setComposing(null);
    start.reset();
  };

  const resolveButton = (each: Thread) =>
    each.can.resolve && (
      <Button
        size="sm"
        variant="ghost"
        loading={resolve.isPending && resolve.variables?.commentId === each.id}
        onClick={() =>
          resolve.mutate(
            { commentId: each.id, resolved: !each.resolved },
            {
              onSuccess: () => {
                if (each.id === active) {
                  setActive(undefined);
                  focusSoon(`#${SECTION_ID}`);
                }
              },
            },
          )
        }
        data-action={each.resolved ? "reopen-thread" : "resolve-thread"}
      >
        {each.resolved ? t.comments.inline.reopen : t.comments.inline.resolve}
      </Button>
    );

  const panel =
    composing || activeThread ? (
      <section
        id={PANEL_ID}
        tabIndex={-1}
        aria-label={t.comments.inline.panel}
        className="mt-6 rounded-overlay border border-border bg-surface-raised p-3 shadow-overlay outline-none focus-visible:ring-2 focus-visible:ring-accent"
        data-passage-panel=""
        onKeyDown={(event) => {
          if (event.key === "Escape" && !event.defaultPrevented && !composing) {
            event.preventDefault();
            closePanel();
          }
        }}
      >
        <div className="mb-2 flex items-center justify-between gap-2">
          <h2 className="text-sm font-medium text-ink">{composing ? t.comments.inline.commentOnSelection : t.comments.inline.panel}</h2>
          <IconButton icon={<Icon.X />} label={t.comments.inline.close} size="sm" onClick={closePanel} data-action="close-passage-panel" />
        </div>
        {composing ? (
          <>
            <figure className="mb-2" data-thread-quote="">
              <figcaption className="text-2xs font-medium text-ink-subtle">{t.comments.inline.quote}</figcaption>
              <blockquote className="border-l-2 border-warning pl-2 text-sm text-ink-muted italic">{shortQuote(composing.passage.quote)}</blockquote>
            </figure>
            <Composer
              id="new-passage-comment"
              label={t.comments.inline.newLabel}
              placeholder={t.comments.inline.newPlaceholder}
              submitLabel={t.comments.post}
              busy={start.isPending}
              error={start.error?.message}
              onSubmit={(comment) => {
                const { passage, threadId } = composing;
                start.mutate(
                  {
                    threadId,
                    body: comment,
                    pageBody: (doc) => {
                      const at = relocate(doc, passage);
                      return at ? markPassage(doc, at, threadId) : null;
                    },
                  },
                  {
                    onSuccess: ({ thread: made }) => {
                      setComposing(null);
                      setActive(made.id);
                      window.getSelection()?.removeAllRanges();
                      focusSoon(`#${PANEL_ID}`);
                    },
                  },
                );
              }}
              onCancel={closePanel}
            />
          </>
        ) : (
          activeThread && (
            <ThreadView
              pageId={page.id}
              thread={activeThread}
              highlighted={false}
              lead={<Quote thread={activeThread} />}
              actions={resolveButton(activeThread)}
            />
          )
        )}
      </section>
    ) : null;

  const listed = [
    ...detached.filter((each) => showResolved || !each.resolved),
    ...(showResolved ? resolved.filter((each) => each.anchor?.state === "anchored") : []),
  ];
  return (
    <PassagesContext value={passages}>
      <div ref={rootRef} className="relative" data-passage-root="">
        {children}
        {selected && !composing && (
          <Button
            size="sm"
            icon={<Icon.Comment />}
            className="absolute z-10 shadow-overlay"
            style={{ top: selected.top, left: selected.left }}
            aria-keyshortcuts={INLINE_COMMENT_SHORTCUT}
            title={t.comments.inline.commentOnSelection}
            // Pressing the button would otherwise take the selection away first.
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => compose(selected.passage)}
            data-action="comment-on-selection"
          >
            {t.comments.inline.comment}
          </Button>
        )}
      </div>
      {panel}
      {below}
      {enabled && (error || threads.length > 0) && (
        <section id={SECTION_ID} tabIndex={-1} aria-labelledby="passages-title" className="mt-10 scroll-mt-4 outline-none" data-passages="">
          <SectionTitle id="passages-title">
            {t.comments.inline.title}
            {open.length > 0 && <span className="ml-1 text-ink-subtle normal-case">({open.length})</span>}
          </SectionTitle>
          {error ? (
            <div className="mt-2">
              <ErrorBanner onRetry={() => void refetch()}>{t.comments.inline.failed}</ErrorBanner>
            </div>
          ) : (
            <>
              {open.length > 0 && (
                <ul className="mt-3 space-y-1" data-open-passages="">
                  {open.map((each) => (
                    <li key={each.id}>
                      <button
                        type="button"
                        className="w-full truncate rounded-control px-2 py-1 text-left text-sm text-ink hover:bg-surface-sunken"
                        aria-controls={each.id === active ? PANEL_ID : undefined}
                        aria-expanded={each.id === active}
                        onClick={() => {
                          openThread(each.id);
                          document.querySelector(`[data-passage="${CSS.escape(each.id)}"]`)?.scrollIntoView({ block: "center" });
                        }}
                        data-open-passage={each.id}
                      >
                        <Icon.Comment className="mr-1.5 inline align-[-2px] text-ink-subtle" />
                        {t.comments.inline.open(shortQuote(each.anchor?.quote ?? ""), each.comments.filter((c) => !c.deleted).length)}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {detached.some((each) => showResolved || !each.resolved) && (
                <>
                  <h3 className="mt-5 text-sm font-medium text-ink">{t.comments.inline.detachedTitle}</h3>
                  <p className="mt-1 text-sm text-ink-muted">{t.comments.inline.detachedNote}</p>
                </>
              )}
              {listed.length > 0 && (
                <ol className="mt-3 space-y-4" aria-label={t.comments.inline.title}>
                  {listed.map((each) => (
                    <li key={each.id} data-anchor-state={each.anchor?.state} data-resolved={each.resolved || undefined}>
                      <ThreadView pageId={page.id} thread={each} highlighted={false} lead={<Quote thread={each} />} actions={resolveButton(each)} />
                    </li>
                  ))}
                </ol>
              )}
              {resolved.length > 0 && (
                <Button
                  className="mt-3"
                  size="sm"
                  variant="secondary"
                  aria-pressed={showResolved}
                  onClick={() => setShowResolved((shown) => !shown)}
                  data-action="toggle-resolved"
                >
                  {showResolved ? t.comments.inline.hideResolved : t.comments.inline.showResolved(resolved.length)}
                </Button>
              )}
            </>
          )}
        </section>
      )}
    </PassagesContext>
  );
}
