import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { INLINE_ANCHOR_RETRIES } from "@/config";
import type { Doc } from "@/features/editor/schema";
import { t } from "@/i18n";
import { ApiError, api } from "./client";
import { pageQueryKey, type Page, type PageInSpace } from "./pages";
import type { components } from "./schema";
import { searchQueryKey } from "./search";

type Wire = components["schemas"];

/** One comment; the wire leaves the body untyped, the editor's schema types it. Null once deleted. */
export type Comment = Omit<Wire["Comment"], "body"> & { body: Doc | null };
/** A first comment and its replies, oldest first. */
export type Thread = Omit<Wire["Thread"], "comments"> & { comments: Comment[] };

export function threadsQueryKey(pageId: string) {
  return ["comments", pageId] as const;
}

/** The threads below a page, oldest first; inline threads are the reader's highlights, not this list's. */
export function useThreads(pageId: string, enabled = true) {
  return useQuery({
    queryKey: threadsQueryKey(pageId),
    queryFn: async (): Promise<Thread[]> =>
      (await api.GET("/pages/{pageID}/comments", { params: { path: { pageID: pageId }, query: { kind: "page" } } })).data!.threads as Thread[],
    enabled,
  });
}

/** The threads on passages of a page, resolved and detached ones included. */
export function useInlineThreads(pageId: string, enabled = true) {
  return useQuery({
    queryKey: [...threadsQueryKey(pageId), "inline"],
    queryFn: async (): Promise<Thread[]> =>
      (await api.GET("/pages/{pageID}/comments", { params: { path: { pageID: pageId }, query: { kind: "inline" } } })).data!.threads as Thread[],
    enabled,
  });
}

/** After any change the list is read again, and the page for its count. */
function useRefresh(pageId: string) {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: threadsQueryKey(pageId) }),
      queryClient.invalidateQueries({ queryKey: pageQueryKey(pageId) }),
      queryClient.invalidateQueries({ queryKey: searchQueryKey }),
    ]);
}

export function useStartThread(pageId: string) {
  const refresh = useRefresh(pageId);
  return useMutation({
    mutationFn: async (body: Doc): Promise<Thread> =>
      (await api.POST("/pages/{pageID}/comments", { params: { path: { pageID: pageId } }, body: { body } })).data!.thread as Thread,
    onSuccess: refresh,
  });
}

export interface InlineStart {
  threadId: string;
  body: Doc;
  /** The page's body as read, with the passage marked for threadId. */
  pageBody: (current: Doc) => Doc | null;
}

/** Starts a thread on a passage; after an anchor_conflict the page is read again and the passage marked afresh. */
export function useStartInlineThread(pageId: string, current: () => Doc | null) {
  const queryClient = useQueryClient();
  const refresh = useRefresh(pageId);
  return useMutation({
    mutationFn: async ({ threadId, body, pageBody }: InlineStart): Promise<{ thread: Thread; page: Page }> => {
      let doc = current();
      for (let attempt = 0; ; attempt += 1) {
        const marked = doc && pageBody(doc);
        if (!marked) throw new Error(t.comments.inline.passageGone);
        try {
          const data = (await api.POST("/pages/{pageID}/inline-comments", { params: { path: { pageID: pageId } }, body: { threadId, body, pageBody: marked } }))
            .data!;
          return { thread: data.thread as Thread, page: data.page as Page };
        } catch (error) {
          if (!(error instanceof ApiError) || error.code !== "anchor_conflict" || attempt >= INLINE_ANCHOR_RETRIES) throw error;
          doc = (await api.GET("/pages/{pageID}", { params: { path: { pageID: pageId } } })).data!.page.body as Doc;
        }
      }
    },
    onSuccess: ({ page }) => {
      queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (old) => (old ? { ...old, page } : old));
      return refresh();
    },
  });
}

/** Resolves or reopens the thread a comment is in. */
export function useResolveThread(pageId: string) {
  const refresh = useRefresh(pageId);
  return useMutation({
    mutationFn: async ({ commentId, resolved }: { commentId: string; resolved: boolean }): Promise<Thread> => {
      const path = { params: { path: { commentID: commentId } } };
      const data = resolved ? (await api.POST("/comments/{commentID}/resolve", path)).data! : (await api.POST("/comments/{commentID}/reopen", path)).data!;
      return data.thread as Thread;
    },
    onSuccess: refresh,
  });
}

/** Answers at the end of the thread the comment is in. */
export function useReply(pageId: string) {
  const refresh = useRefresh(pageId);
  return useMutation({
    mutationFn: async ({ commentId, body }: { commentId: string; body: Doc }): Promise<Comment> =>
      (await api.POST("/comments/{commentID}/replies", { params: { path: { commentID: commentId } }, body: { body } })).data!.comment as Comment,
    onSuccess: refresh,
  });
}

export function useEditComment(pageId: string) {
  const refresh = useRefresh(pageId);
  return useMutation({
    mutationFn: async ({ commentId, body }: { commentId: string; body: Doc }): Promise<Comment> =>
      (await api.PATCH("/comments/{commentID}", { params: { path: { commentID: commentId } }, body: { body } })).data!.comment as Comment,
    onSuccess: refresh,
  });
}

export function useDeleteComment(pageId: string) {
  const refresh = useRefresh(pageId);
  return useMutation({
    mutationFn: async (commentId: string) => {
      await api.DELETE("/comments/{commentID}", { params: { path: { commentID: commentId } } });
    },
    onSuccess: refresh,
  });
}
