import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useBlog, useCreatePost, usePosts, useWatchBlog, type BlogMonth } from "@/api/posts";
import { useSpace } from "@/api/spaces";
import { templateTitle, type Template } from "@/api/templates";
import { Button, Dialog, EmptyState, ErrorBanner, Field, PageHeader, SelectInput, Skeleton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BLANK_TEMPLATE, PAGE_TITLE_MAX_LENGTH } from "@/config";
import { PageLink } from "@/features/pages/PageLink";
import { TemplatePicker } from "@/features/pages/TemplatePicker";
import { TemplateValues, initialValues, missingValues, wireValues, type Values } from "@/features/templates/TemplateValues";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { pageSlug } from "@/lib/slug";
import { postDay } from "./blogPosts";

const monthName = localDateFormat({ month: "long", timeZone: "UTC" });
const editedAt = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** Which stretch of a blog is shown: everything, a year, or a month of it, in UTC. */
export interface BlogFilter {
  year?: number;
  month?: number;
}

/** A month as the blog's address and data attributes write it, such as 2026-10. */
export function monthKey(year: number, month: number): string {
  return `${year}-${String(month).padStart(2, "0")}`;
}

function nameOfMonth(year: number, month: number): string {
  return monthName.format(Date.UTC(year, month - 1, 1));
}

/** The months with posts, grouped by year, newest first, with each year's total. */
export function groupByYear(months: BlogMonth[]): Array<{ year: number; count: number; months: BlogMonth[] }> {
  const years: Array<{ year: number; count: number; months: BlogMonth[] }> = [];
  for (const m of months) {
    let entry = years.find((y) => y.year === m.year);
    if (!entry) {
      entry = { year: m.year, count: 0, months: [] };
      years.push(entry);
    }
    entry.count += m.count;
    entry.months.push(m);
  }
  years.sort((a, b) => b.year - a.year);
  for (const y of years) y.months.sort((a, b) => b.month - a.month);
  return years;
}

/** Asks for a new post's title and what it starts from, makes the post and opens it to write. */
function NewPostDialog({ spaceKey, onClose }: { spaceKey: string; onClose: () => void }) {
  const create = useCreatePost(spaceKey);
  const { data: space } = useSpace(spaceKey);
  const navigate = useNavigate();
  const [title, setTitle] = useState("");
  // A title the writer typed is theirs; one a template filled in follows the choice.
  const [titleTyped, setTitleTyped] = useState(false);
  const [key, setKey] = useState(BLANK_TEMPLATE);
  const [template, setTemplate] = useState<Template>();
  const [values, setValues] = useState<Values>({});
  const [error, setError] = useState("");
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const variables = template?.variables ?? [];

  function choose(next: string, chosen: Template | undefined) {
    setKey(next);
    setTemplate(chosen);
    setValues(initialValues(chosen?.variables ?? []));
    setError("");
    create.reset();
    if (!titleTyped) setTitle(chosen?.title ? templateTitle(chosen.title) : "");
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!template && !title.trim()) {
      setError(t.blog.emptyTitle);
      return;
    }
    const missing = missingValues(variables, values);
    if (missing.length > 0) {
      setError(t.templates.missing(missing));
      return;
    }
    setError("");
    create.mutate(
      {
        title: title.trim(),
        ...(template ? { template: template.key } : {}),
        ...(variables.length > 0 ? { values: wireValues(values) } : {}),
      },
      {
        onSuccess: (made) =>
          void navigate({ to: "/s/$spaceKey/p/$pageId/$slug/edit", params: { spaceKey: made.spaceKey, pageId: made.id, slug: pageSlug(made.title) } }),
      },
    );
  }

  const banner = create.error && !Object.keys(fields).some((field) => field.startsWith("values.") || field === "title") ? create.error.message : "";
  return (
    <Dialog title={t.blog.newPostTitle} wide onClose={onClose} data-new-post-dialog="">
      <form onSubmit={submit} className="space-y-3" noValidate>
        {banner && <ErrorBanner>{fields.template ?? (banner || t.blog.createFailed)}</ErrorBanner>}
        <Field
          label={t.blog.postTitle}
          value={title}
          maxLength={PAGE_TITLE_MAX_LENGTH}
          onChange={(event) => {
            setTitle(event.target.value);
            setTitleTyped(event.target.value !== "");
          }}
          error={(template ? "" : error) || fields.title}
          hint={variables.length > 0 ? t.templates.titleHint : undefined}
          autoFocus
        />
        <TemplatePicker value={key} onChange={choose} spaceKey={spaceKey} />
        {space && <TemplateValues variables={variables} values={values} onChange={setValues} parentId={space.homePageId} errors={fields} />}
        {template && error && (
          <p role="alert" className="text-sm text-danger" data-template-missing="">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.page.cancel}
          </Button>
          <Button type="submit" loading={create.isPending} data-action="create-post">
            {t.blog.create}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/** The posts of the chosen stretch, newest first, a window at a time. */
function PostList({ spaceKey, filter, canPost }: { spaceKey: string; filter: BlogFilter; canPost: boolean }) {
  const posts = usePosts({ space: spaceKey, ...filter });
  if (posts.isPending) {
    return (
      <p className="text-sm text-ink-muted" role="status">
        {t.blog.loading}
      </p>
    );
  }
  if (posts.isError) return <ErrorBanner onRetry={() => void posts.refetch()}>{t.blog.failed}</ErrorBanner>;
  const all = posts.data.pages.flatMap((p) => p.posts);
  if (all.length === 0) {
    const filtered = filter.year !== undefined;
    return (
      <EmptyState icon={<Icon.Megaphone />} title={t.blog.title} description={filtered ? t.blog.emptyFiltered : canPost ? t.blog.empty : t.blog.emptyReader} />
    );
  }
  return (
    <>
      <ol className="divide-y divide-border" data-post-list="">
        {all.map((post) => (
          <li key={post.id} className="py-4 first:pt-0" data-post={post.id} data-post-title={post.title}>
            <h2 className="text-lg font-semibold text-ink">
              {post.icon && (
                <span className="mr-2" aria-hidden="true">
                  {post.icon}
                </span>
              )}
              <PageLink spaceKey={post.spaceKey} id={post.id} title={post.title} className="hover:text-accent hover:underline" />
            </h2>
            <p className="mt-0.5 text-sm text-ink-muted">
              <time dateTime={post.postedAt}>{postDay.format(new Date(post.postedAt))}</time>
              {post.authorName && ` · ${t.blog.by(post.authorName)}`}
            </p>
            {post.excerpt && <p className="mt-2 text-ink">{post.excerpt}</p>}
          </li>
        ))}
      </ol>
      {posts.hasNextPage && (
        <div className="mt-4">
          <Button variant="secondary" onClick={() => void posts.fetchNextPage()} loading={posts.isFetchingNextPage} data-action="more-posts">
            {t.blog.more}
          </Button>
        </div>
      )}
    </>
  );
}

/** The years and months with posts, as links that keep the blog to them. */
function DateNav({ spaceKey, months, filter }: { spaceKey: string; months: BlogMonth[]; filter: BlogFilter }) {
  const navigate = useNavigate();
  const years = groupByYear(months);
  const current = filter.year === undefined ? "" : filter.month === undefined ? String(filter.year) : monthKey(filter.year, filter.month);
  const linkClass = (on: boolean) =>
    cx("block rounded-control px-2 py-1 text-sm hover:bg-surface-raised", on ? "font-medium text-accent" : "text-ink-muted hover:text-ink");
  return (
    <nav aria-label={t.blog.dates} data-blog-dates="">
      {/* A phone has no room for the list beside the posts. */}
      <SelectInput
        className="w-full md:hidden"
        aria-label={t.blog.jump}
        value={current}
        onChange={(event) => {
          const [year, month] = event.target.value.split("-").map(Number);
          void navigate({ to: "/s/$spaceKey/blog", params: { spaceKey }, search: year ? (month ? { year, month } : { year }) : {} });
        }}
      >
        <option value="">{t.blog.allPosts}</option>
        {years.map((y) => [
          <option key={y.year} value={String(y.year)}>
            {t.blog.count(String(y.year), y.count)}
          </option>,
          ...y.months.map((m) => (
            <option key={monthKey(m.year, m.month)} value={monthKey(m.year, m.month)}>
              {t.blog.count(`${nameOfMonth(m.year, m.month)} ${m.year}`, m.count)}
            </option>
          )),
        ])}
      </SelectInput>
      <ul className="hidden space-y-1 md:block">
        <li>
          <Link
            to="/s/$spaceKey/blog"
            params={{ spaceKey }}
            search={{}}
            className={linkClass(current === "")}
            aria-current={current === "" ? "page" : undefined}
          >
            {t.blog.allPosts}
          </Link>
        </li>
        {years.map((y) => (
          <li key={y.year} data-blog-year={y.year}>
            <Link
              to="/s/$spaceKey/blog"
              params={{ spaceKey }}
              search={{ year: y.year }}
              className={linkClass(current === String(y.year))}
              aria-current={current === String(y.year) ? "page" : undefined}
            >
              {t.blog.count(String(y.year), y.count)}
            </Link>
            <ul className="ml-3">
              {y.months.map((m) => {
                const key = monthKey(m.year, m.month);
                return (
                  <li key={key} data-blog-month={key} data-count={m.count}>
                    <Link
                      to="/s/$spaceKey/blog"
                      params={{ spaceKey }}
                      search={{ year: m.year, month: m.month }}
                      className={linkClass(current === key)}
                      aria-current={current === key ? "page" : undefined}
                    >
                      {t.blog.count(nameOfMonth(m.year, m.month), m.count)}
                    </Link>
                  </li>
                );
              })}
            </ul>
          </li>
        ))}
      </ul>
    </nav>
  );
}

/** A space's blog: its posts by date, the reader's own posts still to go out, and a way to write and to watch. */
export function BlogScreen({ spaceKey, filter }: { spaceKey: string; filter: BlogFilter }) {
  const blog = useBlog(spaceKey);
  const watch = useWatchBlog(spaceKey);
  const [writing, setWriting] = useState(false);
  if (blog.error) return <ErrorBanner onRetry={() => void blog.refetch()}>{blog.error.message}</ErrorBanner>;
  if (blog.isPending) return <Skeleton />;
  const b = blog.data;
  const heading =
    filter.year === undefined ? t.blog.title : filter.month === undefined ? String(filter.year) : `${nameOfMonth(filter.year, filter.month)} ${filter.year}`;
  return (
    <div className="mx-auto max-w-5xl" data-blog={b.spaceKey}>
      <PageHeader
        crumb={b.spaceName}
        title={t.blog.title}
        actions={
          <>
            <Button
              variant="secondary"
              icon={<Icon.Eye />}
              title={b.watching ? t.blog.unwatchHint : t.blog.watchHint}
              aria-pressed={b.watching}
              loading={watch.isPending}
              onClick={() => watch.mutate(!b.watching)}
              data-action="watch-blog"
              data-watching={b.watching || undefined}
            >
              {b.watching ? t.blog.watching : t.blog.watch}
            </Button>
            {b.canPost && (
              <Button icon={<Icon.Plus />} onClick={() => setWriting(true)} data-action="new-post">
                {t.blog.newPost}
              </Button>
            )}
          </>
        }
      />
      {watch.isError && <ErrorBanner>{t.blog.watchFailed}</ErrorBanner>}
      {b.unpublished.length > 0 && (
        <section className="mb-6 rounded-control border border-border bg-surface-raised px-4 py-3" aria-label={t.blog.unpublished} data-blog-unpublished="">
          <h2 className="text-sm font-semibold text-ink">{t.blog.unpublished}</h2>
          <p className="text-xs text-ink-muted">{t.blog.unpublishedHint}</p>
          <ul className="mt-2 space-y-1">
            {b.unpublished.map((post) => (
              <li key={post.id} className="text-sm" data-unpublished-post={post.id}>
                <PageLink spaceKey={b.spaceKey} id={post.id} title={post.title} className="font-medium text-accent hover:underline" />
                <span className="text-ink-muted">
                  {" · "}
                  {post.publishAt ? t.blog.scheduled(editedAt.format(new Date(post.publishAt))) : t.blog.edited(editedAt.format(new Date(post.updatedAt)))}
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
      <div className="grid gap-6 md:grid-cols-[12rem_minmax(0,1fr)]">
        <DateNav spaceKey={b.spaceKey} months={b.months} filter={filter} />
        <section aria-label={heading} className="min-w-0">
          {filter.year !== undefined && <h2 className="mb-3 text-base font-semibold text-ink">{heading}</h2>}
          <PostList spaceKey={b.spaceKey} filter={filter} canPost={b.canPost} />
        </section>
      </div>
      {writing && <NewPostDialog spaceKey={b.spaceKey} onClose={() => setWriting(false)} />}
    </div>
  );
}
