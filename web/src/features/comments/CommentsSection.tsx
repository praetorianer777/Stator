import { Suspense, lazy, useEffect, useRef, useState } from "react";
import { useMe } from "@/api/auth";
import { useDeleteComment, useEditComment, useReply, useStartThread, useThreads, type Comment, type Thread } from "@/api/comments";
import type { Page } from "@/api/pages";
import { Avatar, Button, ErrorBanner, SectionTitle, Skeleton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { COMMENT_HIGHLIGHT_MS } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { t } from "@/i18n";

const CommentEditor = lazy(() => import("./CommentEditor"));

const when = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

/** Where the page's header count and a notification's link lead. */
export const COMMENTS_ID = "comments";

function authorOf(comment: Comment): string {
  return comment.authorName || t.comments.formerMember;
}

function messageOf(error: Error | null): string | undefined {
  return error?.message;
}

/** The editor, loaded on first use, with a line in its place while it comes. */
function Composer(props: Parameters<typeof CommentEditor>[0]) {
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
                label={t.comments.addLabel}
                placeholder={t.comments.addPlaceholder}
                submitLabel={t.comments.post}
                busy={start.isPending}
                error={messageOf(start.error)}
                onSubmit={(body) =>
                  start.mutate(body, {
                    onSuccess: (made) => {
                      setComposing(false);
                      requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-thread="${CSS.escape(made.id)}"]`)?.focus());
                    },
                  })
                }
                onCancel={() => {
                  start.reset();
                  setComposing(false);
                  requestAnimationFrame(() => addRef.current?.focus());
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

function ThreadView({ pageId, thread, highlighted }: { pageId: string; thread: Thread; highlighted: boolean }) {
  const reply = useReply(pageId);
  const [replying, setReplying] = useState(false);
  const replyRef = useRef<HTMLButtonElement>(null);
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
      <CommentView pageId={pageId} comment={first} />
      {replies.length > 0 && (
        <ol className="mt-3 space-y-3 border-l-2 border-border pl-3 sm:ml-8" aria-label={t.comments.replies(replies.length)}>
          {replies.map((each) => (
            <li key={each.id}>
              <CommentView pageId={pageId} comment={each} />
            </li>
          ))}
        </ol>
      )}
      {thread.can.reply && (
        <div className="mt-3 sm:ml-8">
          {replying ? (
            <Composer
              id={`reply-${thread.id}`}
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
                      requestAnimationFrame(() => replyRef.current?.focus());
                    },
                  },
                )
              }
              onCancel={() => {
                reply.reset();
                setReplying(false);
                requestAnimationFrame(() => replyRef.current?.focus());
              }}
            />
          ) : (
            <Button ref={replyRef} size="sm" variant="ghost" onClick={() => setReplying(true)} data-action="reply">
              {t.comments.reply}
            </Button>
          )}
        </div>
      )}
    </article>
  );
}

function CommentView({ pageId, comment }: { pageId: string; comment: Comment }) {
  const edit = useEditComment(pageId);
  const remove = useDeleteComment(pageId);
  const me = useMe().data?.user.id;
  const [editing, setEditing] = useState(false);
  const editRef = useRef<HTMLButtonElement>(null);
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
        </p>
        {comment.deleted || !comment.body ? (
          <p className="mt-1 text-sm text-ink-muted italic" data-comment-placeholder="">
            {t.comments.deleted}
          </p>
        ) : editing ? (
          <div className="mt-1">
            <Composer
              id={`edit-${comment.id}`}
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
                    onSuccess: () => {
                      setEditing(false);
                      requestAnimationFrame(() => editRef.current?.focus());
                    },
                  },
                )
              }
              onCancel={() => {
                edit.reset();
                setEditing(false);
                requestAnimationFrame(() => editRef.current?.focus());
              }}
            />
          </div>
        ) : (
          <DocView doc={comment.body} anchors={false} className="doc-comment mt-1 text-sm" />
        )}
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
