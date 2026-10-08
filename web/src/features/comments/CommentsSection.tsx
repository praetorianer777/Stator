import { localDateFormat } from "@/lib/format";
import { Suspense, lazy, useEffect, useRef, useState, type ReactNode } from "react";
import { useMe } from "@/api/auth";
import { useDeleteComment, useEditComment, useReply, useStartThread, useThreads, type Comment, type Thread } from "@/api/comments";
import type { Page } from "@/api/pages";
import { Avatar, Button, ErrorBanner, SectionTitle, Skeleton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { COMMENT_HIGHLIGHT_MS } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { CommentReactions } from "@/features/reactions/Reactions";
import { t } from "@/i18n";
import { useFocusWhenRendered } from "@/lib/focus";

const CommentEditor = lazy(() => import("./CommentEditor"));

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** Where the page's header count and a notification's link lead. */
export const COMMENTS_ID = "comments";

function authorOf(comment: Comment): string {
  return comment.authorName || t.comments.formerMember;
}

function messageOf(error: Error | null): string | undefined {
  return error?.message;
}

/** The editor, loaded on first use, with a line in its place while it comes. */
export function Composer(props: Parameters<typeof CommentEditor>[0]) {
  return (
    <Suspense fallback={<p className="text-sm text-ink-muted">{t.comments.loadingEditor}</p>}>
      <CommentEditor {...props} />
    </Suspense>
  );
}

/**
 * The discussion below a page: its threads, oldest first, each a first
 * comment and its replies. What a person may do follows the page's and each
 * comment's `can`; `thread` is a thread to bring into view, as a notification
 * links to it.
 */
export function CommentsSection({ page, thread }: { page: Page; thread?: string }) {
  const { data, error, isLoading, refetch } = useThreads(page.id, !page.unpublished);
  const start = useStartThread(page.id);
  const [composing, setComposing] = useState(false);
  const addRef = useRef<HTMLButtonElement>(null);
  const focusWhenRendered = useFocusWhenRendered();
  const [highlight, setHighlight] = useState<string>();
  const threads = data ?? [];
  const canComment = page.can.comment && !page.unpublished;

  // A thread named in the address is brought into view once, when the list
  // that holds it has arrived, and marked for a moment.
  const shown = useRef<string>(undefined);
  useEffect(() => {
    if (!thread || !data || shown.current === thread) return;
    shown.current = thread;
    const target = document.querySelector<HTMLElement>(`[data-thread="${CSS.escape(thread)}"]`);
    if (!target) return;
    target.scrollIntoView({ block: "start" });
    target.focus({ preventScroll: true });
    setHighlight(thread);
    const timer = window.setTimeout(() => setHighlight(undefined), COMMENT_HIGHLIGHT_MS);
    return () => window.clearTimeout(timer);
  }, [thread, data]);

  const count = page.comments.page;
  return (
    <section id={COMMENTS_ID} aria-labelledby="comments-title" className="mt-10 scroll-mt-4" data-comments="">
      <SectionTitle id="comments-title">
        {t.comments.title}
        {count > 0 && <span className="ml-1 text-ink-subtle normal-case">({count})</span>}
      </SectionTitle>
      {page.unpublished ? (
        <p className="mt-2 text-sm text-ink-muted" data-comments-unpublished="">
          {t.comments.unpublished}
        </p>
      ) : error ? (
        <div className="mt-2">
          <ErrorBanner onRetry={() => void refetch()}>{t.comments.failed}</ErrorBanner>
        </div>
      ) : isLoading ? (
        <Skeleton />
      ) : (
        <>
          {threads.length === 0 ? (
            <p className="mt-2 text-sm text-ink-muted" data-comments-empty="">
              {t.comments.empty}
            </p>
          ) : (
            <ol className="mt-3 space-y-4" aria-labelledby="comments-title">
              {threads.map((each) => (
                <li key={each.id}>
                  <ThreadView pageId={page.id} thread={each} highlighted={highlight === each.id} />
                </li>
              ))}
            </ol>
          )}
          <div className="mt-4">
            {!canComment ? (
              <p className="text-sm text-ink-muted" data-comments-read-only="">
                {t.comments.readOnly}
              </p>
            ) : composing ? (
              <Composer
                id="new-comment"
                pageId={page.id}
                label={t.comments.addLabel}
                placeholder={t.comments.addPlaceholder}
                submitLabel={t.comments.post}
                busy={start.isPending}
                error={messageOf(start.error)}
                onSubmit={(body) =>
                  start.mutate(body, {
                    onSuccess: (made) => {
                      setComposing(false);
                      focusWhenRendered(() => document.querySelector<HTMLElement>(`[data-thread="${CSS.escape(made.id)}"]`));
                    },
                  })
                }
                onCancel={() => {
                  start.reset();
                  setComposing(false);
                  focusWhenRendered(() => addRef.current);
                }}
              />
            ) : (
              <Button ref={addRef} variant="secondary" icon={<Icon.Comment />} onClick={() => setComposing(true)} data-action="add-comment">
                {t.comments.add}
              </Button>
            )}
          </div>
        </>
      )}
    </section>
  );
}

/** A first comment and its replies, with what the caller may do; `lead` goes above them and `actions` below. */
export function ThreadView({
  pageId,
  thread,
  highlighted,
  lead,
  actions,
}: {
  pageId: string;
  thread: Thread;
  highlighted: boolean;
  lead?: ReactNode;
  actions?: ReactNode;
}) {
  const reply = useReply(pageId);
  const [replying, setReplying] = useState(false);
  const replyRef = useRef<HTMLButtonElement>(null);
  const focusWhenRendered = useFocusWhenRendered();
  const [first, ...replies] = thread.comments;
  if (!first) return null;
  return (
    <article
      tabIndex={-1}
      aria-label={t.comments.thread(authorOf(first))}
      className={cx(
        "scroll-mt-4 rounded-overlay border border-border bg-surface p-3 outline-none focus-visible:ring-2 focus-visible:ring-accent",
        highlighted && "ring-2 ring-accent",
      )}
      data-thread={thread.id}
      data-highlighted={highlighted || undefined}
    >
      {lead}
      <CommentView pageId={pageId} comment={first} canReact={thread.can.reply} />
      {replies.length > 0 && (
        <ol className="mt-3 space-y-3 border-l-2 border-border pl-3 sm:ml-8" aria-label={t.comments.replies(replies.length)}>
          {replies.map((each) => (
            <li key={each.id}>
              <CommentView pageId={pageId} comment={each} canReact={thread.can.reply} />
            </li>
          ))}
        </ol>
      )}
      {(thread.can.reply || actions) && (
        <div className="mt-3 sm:ml-8">
          {replying ? (
            <Composer
              id={`reply-${thread.id}`}
              pageId={pageId}
              label={t.comments.replyLabel(authorOf(first))}
              placeholder={t.comments.replyPlaceholder}
              submitLabel={t.comments.postReply}
              busy={reply.isPending}
              error={messageOf(reply.error)}
              onSubmit={(body) =>
                reply.mutate(
                  { commentId: thread.id, body },
                  {
                    onSuccess: () => {
                      setReplying(false);
                      focusWhenRendered(() => replyRef.current);
                    },
                  },
                )
              }
              onCancel={() => {
                reply.reset();
                setReplying(false);
                focusWhenRendered(() => replyRef.current);
              }}
            />
          ) : (
            <div className="flex flex-wrap gap-1">
              {thread.can.reply && (
                <Button ref={replyRef} size="sm" variant="ghost" onClick={() => setReplying(true)} data-action="reply">
                  {t.comments.reply}
                </Button>
              )}
              {actions}
            </div>
          )}
        </div>
      )}
    </article>
  );
}

function CommentView({ pageId, comment, canReact }: { pageId: string; comment: Comment; canReact: boolean }) {
  const edit = useEditComment(pageId);
  const remove = useDeleteComment(pageId);
  const me = useMe().data?.user.id;
  const [editing, setEditing] = useState(false);
  const editRef = useRef<HTMLButtonElement>(null);
  const restoreFocus = useRef(false);
  const stopEditing = () => {
    restoreFocus.current = true;
    setEditing(false);
  };
  // The edit button is back only once the form is gone, which a save commits
  // outside any event; an effect runs after that commit, a frame may not.
  useEffect(() => {
    if (editing || !restoreFocus.current) return;
    restoreFocus.current = false;
    editRef.current?.focus();
  }, [editing]);
  const author = authorOf(comment);
  return (
    <div className="flex gap-2.5" data-comment={comment.id} data-deleted={comment.deleted || undefined}>
      <Avatar name={author} size="md" className="mt-0.5" />
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-baseline gap-x-1.5 text-xs text-ink-muted">
          <span className="font-medium text-ink" data-comment-author="">
            {author}
          </span>
          <time dateTime={comment.createdAt}>{when.format(new Date(comment.createdAt))}</time>
          {comment.editedAt && !comment.deleted && <span data-comment-edited="">· {t.comments.edited}</span>}
          {comment.originalAuthor && <span data-comment-original="">· {t.comments.originalAuthor(comment.originalAuthor)}</span>}
        </p>
        {comment.deleted || !comment.body ? (
          <p className="mt-1 text-sm text-ink-muted italic" data-comment-placeholder="">
            {t.comments.deleted}
          </p>
        ) : editing ? (
          <div className="mt-1">
            <Composer
              id={`edit-${comment.id}`}
              pageId={pageId}
              label={t.comments.editLabel}
              placeholder={t.comments.addPlaceholder}
              submitLabel={t.comments.save}
              initial={comment.body}
              busy={edit.isPending}
              error={messageOf(edit.error)}
              onSubmit={(body) =>
                edit.mutate(
                  { commentId: comment.id, body },
                  {
                    onSuccess: stopEditing,
                  },
                )
              }
              onCancel={() => {
                edit.reset();
                stopEditing();
              }}
            />
          </div>
        ) : (
          <DocView doc={comment.body} anchors={false} className="doc-comment mt-1 text-sm" />
        )}
        {!editing && <CommentReactions pageId={pageId} comment={comment} author={author} canReact={canReact} />}
        {!editing && (comment.can.edit || comment.can.delete) && (
          <div className="mt-1 flex gap-1">
            {comment.can.edit && (
              <Button ref={editRef} size="sm" variant="ghost" onClick={() => setEditing(true)} data-action="edit-comment">
                {t.comments.edit}
              </Button>
            )}
            {comment.can.delete && (
              <Button
                size="sm"
                variant="ghost"
                loading={remove.isPending}
                onClick={() => {
                  const mine = comment.authorId !== null && comment.authorId === me;
                  if (!window.confirm(mine ? t.comments.confirmDelete : t.comments.confirmDeleteOthers(author))) return;
                  remove.mutate(comment.id);
                }}
                data-action="delete-comment"
              >
                {t.comments.delete}
              </Button>
            )}
          </div>
        )}
        {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      </div>
    </div>
  );
}
