import { BLOG_POSTS_DEFAULT_LIMIT, PAGE_LIST_MAX_LIMIT } from "@/config";
import { localDateFormat } from "@/lib/format";

// A post's date is its day in UTC, the day the blog files it under.
export const postDay = localDateFormat({ dateStyle: "medium", timeZone: "UTC" });

export const BLOG_POSTS_NODE = "blogPosts";

/** What a latest blog posts block stores: whose posts and how many, never the posts. */
export interface BlogPostsSettings {
  /** A space's key, or null for every space the reader may read. */
  space: string | null;
  limit: number;
}

const SPACE = /^[A-Z][A-Z0-9]{1,9}$/;

/** A block's attributes as the server takes them, each one wrong put right. */
export function blogPostsSettings(attrs: Record<string, unknown> | undefined): BlogPostsSettings {
  const space = typeof attrs?.space === "string" && SPACE.test(attrs.space) ? attrs.space : null;
  const n = Number(attrs?.limit);
  const limit = Number.isInteger(n) && n >= 1 && n <= PAGE_LIST_MAX_LIMIT ? n : BLOG_POSTS_DEFAULT_LIMIT;
  return { space, limit };
}
