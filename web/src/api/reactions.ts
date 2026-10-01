import { useMutation, useQueryClient } from "@tanstack/react-query";
import { threadsQueryKey, type Thread } from "./comments";
import { api } from "./client";
import { pageQueryKey, type PageInSpace } from "./pages";
import type { components } from "./schema";

/** One emoji on a page or comment: how many, whether the caller is among them, and the first of them by name. */
export type Reaction = components["schemas"]["Reaction"];

/** Puts an emoji on, or takes it off. */
export interface ReactionChange {
  emoji: string;
  on: boolean;
}

/** Reacts to a page; the answer is the page's reactions, which replace those read. */
export function usePageReaction(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ emoji, on }: ReactionChange): Promise<Reaction[]> => {
      const path = { pageID: pageId };
      const data = on
        ? (await api.POST("/pages/{pageID}/reactions", { params: { path }, body: { emoji } })).data!
        : (await api.DELETE("/pages/{pageID}/reactions", { params: { path, query: { emoji } } })).data!;
      return data.reactions;
    },
    onSuccess: (reactions) =>
      queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (current) => (current ? { ...current, page: { ...current.page, reactions } } : current)),
  });
}

/** Reacts to a comment on a page; the comment's reactions are replaced in every list of the page's threads. */
export function useCommentReaction(pageId: string, commentId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ emoji, on }: ReactionChange): Promise<Reaction[]> => {
      const path = { commentID: commentId };
      const data = on
        ? (await api.POST("/comments/{commentID}/reactions", { params: { path }, body: { emoji } })).data!
        : (await api.DELETE("/comments/{commentID}/reactions", { params: { path, query: { emoji } } })).data!;
      return data.reactions;
    },
    onSuccess: (reactions) =>
      queryClient.setQueriesData<Thread[]>({ queryKey: threadsQueryKey(pageId) }, (threads) =>
        threads?.map((thread) => ({
          ...thread,
          comments: thread.comments.map((comment) => (comment.id === commentId ? { ...comment, reactions } : comment)),
        })),
      ),
  });
}
