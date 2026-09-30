import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Doc } from "@/features/editor/schema";
import { api } from "./client";
import { pageQueryKey } from "./pages";
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
