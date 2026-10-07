import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Doc } from "@/features/editor/schema";
import { BLOG_PAGE_SIZE } from "@/config";
import { api } from "./client";
import { pagesQueryKey, type Page } from "./pages";
import type { components } from "./schema";
import { watchesQueryKey } from "./watching";

type Wire = components["schemas"];

/** A published blog post as a list shows it. */
export type Post = Wire["Post"];
/** A space's blog: its months with posts, the caller's unpublished posts, and what they may do. */
export type Blog = Wire["Blog"];
export type BlogMonth = Wire["BlogMonth"];

// Under the pages' key, so whatever reads pages again after a publish, a
// trash or a watch reads the blogs and their lists again too.
const blogsQueryKey = [...pagesQueryKey, "blog"] as const;
const postsQueryKey = [...pagesQueryKey, "posts"] as const;

export function blogQueryKey(spaceKey: string) {
  return [...blogsQueryKey, spaceKey.toUpperCase()] as const;
}

/** A space's blog as the reader finds it. */
export function useBlog(spaceKey: string) {
  return useQuery({
    queryKey: blogQueryKey(spaceKey),
    queryFn: async (): Promise<Blog> => (await api.GET("/spaces/{spaceKey}/blog", { params: { path: { spaceKey } } })).data!.blog,
  });
}

/** Which posts a blog lists: a space, and a year or a month of it, in UTC. */
export interface PostFilter {
  space: string;
  year?: number;
  month?: number;
}

/** A blog's posts the reader may read, newest first, a window at a time. */
export function usePosts(filter: PostFilter) {
  return useInfiniteQuery({
    queryKey: [...postsQueryKey, "blog", filter.space.toUpperCase(), filter.year ?? 0, filter.month ?? 0],
    initialPageParam: "",
    queryFn: async ({ pageParam }) =>
      (
        await api.GET("/posts", {
          params: { query: { space: filter.space, year: filter.year, month: filter.month, limit: BLOG_PAGE_SIZE, cursor: pageParam || undefined } },
        })
      ).data!,
    getNextPageParam: (last) => last.next ?? undefined,
  });
}

/** A latest blog posts block's posts, as the reader may read them. */
export function useLatestPosts(settings: { space: string | null; limit: number }) {
  return useQuery({
    queryKey: [...postsQueryKey, "latest", settings],
    queryFn: async () => (await api.GET("/posts", { params: { query: { space: settings.space ?? undefined, limit: settings.limit } } })).data!.posts,
  });
}

/** Starts a post in a space's blog, unpublished and the caller's own until they publish it. */
export function useCreatePost(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { title: string; body?: Doc }): Promise<Page> =>
      (await api.POST("/spaces/{spaceKey}/posts", { params: { path: { spaceKey } }, body: input })).data!.page as Page,
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: blogQueryKey(spaceKey) }),
  });
}

/** Hears of every new post in a space's blog, or stops. */
export function useWatchBlog(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (watch: boolean) => {
      const params = { params: { path: { spaceKey } } };
      if (watch) await api.PUT("/spaces/{spaceKey}/blog/watch", params);
      else await api.DELETE("/spaces/{spaceKey}/blog/watch", params);
      return watch;
    },
    onSuccess: (watching) => {
      queryClient.setQueryData<Blog>(blogQueryKey(spaceKey), (current) => (current ? { ...current, watching } : current));
      void queryClient.invalidateQueries({ queryKey: watchesQueryKey });
    },
  });
}
