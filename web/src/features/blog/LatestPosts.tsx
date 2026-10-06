import type { ReactNode } from "react";
import { useLatestPosts } from "@/api/posts";
import { BLOG_BLOCK_EXCERPT_LENGTH } from "@/config";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { postDay, type BlogPostsSettings } from "./blogPosts";

/** Cuts an excerpt after a whole word, for a block that shows a line or two. */
export function shortExcerpt(text: string, max = BLOG_BLOCK_EXCERPT_LENGTH): string {
  if (text.length <= max) return text;
  const cut = text.slice(0, max);
  const space = cut.lastIndexOf(" ");
  return `${(space > 0 ? cut.slice(0, space) : cut).replace(/[\s.,;:!?]+$/, "")}...`;
}

/**
 * The newest posts of a space's blog or of every space, as the reader may read
 * them. In the editor a title is no link, so a click selects the block.
 */
export function LatestPosts({ settings, inEditor = false }: { settings: BlogPostsSettings; inEditor?: boolean }) {
  const b = t.blogPosts;
  const query = useLatestPosts(settings);
  let state: string;
  let body: ReactNode;
  if (query.isPending) {
    state = "loading";
    body = (
      <p className="doc-block-empty" role="status">
        {b.loading}
      </p>
    );
  } else if (query.isError) {
    state = "failed";
    body = <p className="doc-block-empty">{b.failed}</p>;
  } else if (query.data.length === 0) {
    state = "empty";
    body = <p className="doc-block-empty">{b.empty}</p>;
  } else {
    state = "list";
    body = (
      <ul className="doc-page-list">
        {query.data.map((post) => (
          <li key={post.id} data-listed-post={post.title}>
            {inEditor ? (
              <span className="doc-page-list-title">{post.title}</span>
            ) : (
              <PageLink spaceKey={post.spaceKey} id={post.id} title={post.title} className="doc-page-list-title" />
            )}
            <span className="doc-page-list-meta">
              {[settings.space ? "" : post.spaceName, post.authorName ? t.blog.by(post.authorName) : ""]
                .filter(Boolean)
                .map((part) => `${part} · `)
                .join("")}
              <time dateTime={post.postedAt}>{postDay.format(new Date(post.postedAt))}</time>
            </span>
            {post.excerpt && <span className="doc-page-list-meta block">{shortExcerpt(post.excerpt)}</span>}
          </li>
        ))}
      </ul>
    );
  }
  return (
    <section className="doc-page-list-block" aria-label={b.title(settings.space)} data-blog-posts={settings.space ?? ""} data-state={state}>
      <p className="doc-chart-title">{b.title(settings.space)}</p>
      {body}
    </section>
  );
}
