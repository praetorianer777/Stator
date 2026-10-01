import { useId, useState, type ReactNode } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import type { InfiniteData, UseInfiniteQueryResult } from "@tanstack/react-query";
import { useRecentPages } from "@/api/search";
import { useEdited, useStars, useUnstar, useUpdates, type Star, type UpdateScope } from "@/api/stars";
import { Button, EmptyState, ErrorBanner, IconButton, PageHeader, SectionTitle, Skeleton, TabPanel, Tabs, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PageLink } from "@/features/pages/PageLink";
import { StarGlyph } from "@/features/stars/StarButton";
import { VerifiedMark } from "@/features/stewardship/VerificationBadge";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const day = localDateFormat({ dateStyle: "medium" });
const moment = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

type Windows<T> = UseInfiniteQueryResult<InfiniteData<T>>;

function rowsOf<T, R>(query: Windows<T>, pick: (window: T) => R[]): R[] {
  return query.data?.pages.flatMap(pick) ?? [];
}

/** A list's way to the next window, while there is one. */
function More({ query, list }: { query: Windows<unknown>; list: string }) {
  if (!query.hasNextPage) return null;
  return (
    <Button
      variant="ghost"
      size="sm"
      className="mt-1 self-start"
      loading={query.isFetchingNextPage}
      onClick={() => void query.fetchNextPage()}
      data-more={list}
    >
      {t.home.more}
    </Button>
  );
}

/** One of the side lists: a heading, then the rows, a sentence when there are none, or what went wrong. */
function SideList({
  title,
  list,
  query,
  count,
  empty,
  headingId,
  children,
}: {
  /** Lets the list's heading take focus, for when the row that had it goes. */
  headingId?: string;
  title: string;
  list: string;
  query: { isLoading: boolean; isError: boolean; refetch: () => unknown };
  count: number;
  empty: string;
  children: ReactNode;
}) {
  const ownId = useId();
  const id = headingId ?? ownId;
  return (
    <section aria-labelledby={id} className="flex flex-col gap-2" data-home-list={list}>
      <SectionTitle id={id} tabIndex={headingId ? -1 : undefined} className="outline-none">
        {title}
      </SectionTitle>
      {query.isError ? (
        <ErrorBanner onRetry={() => void query.refetch()}>{t.home.loadFailed}</ErrorBanner>
      ) : query.isLoading ? (
        <Skeleton />
      ) : count === 0 ? (
        <p className="text-sm text-ink-muted">{empty}</p>
      ) : (
        <ul className="flex flex-col">{children}</ul>
      )}
    </section>
  );
}

const rowClass = "flex min-w-0 items-center gap-2 rounded-control px-2 py-1.5 hover:bg-surface-raised";
const linkClass = "min-w-0 truncate text-sm font-medium text-ink hover:text-accent hover:underline";

function starName(star: Star): string {
  return star.page ? star.page.title : t.home.spaceNamed(star.spaceName);
}

function Starred({ onNotice }: { onNotice: (notice: string) => void }) {
  const query = useStars();
  const unstar = useUnstar();
  const stars = rowsOf(query, (window) => window.stars);
  const headingId = useId();
  const done = (star: Star) => {
    onNotice(t.home.unstarred(starName(star)));
    document.getElementById(headingId)?.focus();
  };
  return (
    <SideList title={t.home.starredTitle} list="starred" query={query} count={stars.length} empty={t.home.starredEmpty} headingId={headingId}>
      {stars.map((star) => (
        <li key={star.page?.id ?? star.spaceKey} className={rowClass} data-star-row={starName(star)}>
          <span className="shrink-0 text-ink-subtle">{star.page ? <Icon.Page /> : <Icon.Space />}</span>
          <span className="flex min-w-0 flex-1 flex-col">
            {star.page ? (
              <PageLink spaceKey={star.spaceKey} id={star.page.id} title={star.page.title} className={linkClass} />
            ) : (
              <Link to="/s/$spaceKey" params={{ spaceKey: star.spaceKey }} className={linkClass}>
                {star.spaceName}
              </Link>
            )}
            {star.page && <span className="truncate text-xs text-ink-subtle">{star.spaceName}</span>}
          </span>
          <IconButton
            icon={<StarGlyph on />}
            label={t.home.unstar(starName(star))}
            size="sm"
            onClick={() => unstar.mutate(star, { onSuccess: () => done(star) })}
            data-action="unstar"
          />
        </li>
      ))}
      <More query={query} list="starred" />
    </SideList>
  );
}

function Recent() {
  const query = useRecentPages();
  const pages = query.data ?? [];
  return (
    <SideList title={t.home.recentTitle} list="recent" query={query} count={pages.length} empty={t.home.recentEmpty}>
      {pages.map((page) => (
        <li key={page.id} className={rowClass} data-recent-row={page.title}>
          <span className="flex min-w-0 flex-1 flex-col">
            <PageLink spaceKey={page.spaceKey} id={page.id} title={page.title} className={linkClass} />
            <span className="truncate text-xs text-ink-subtle">
              {page.spaceName} · {day.format(new Date(page.visitedAt))}
            </span>
          </span>
        </li>
      ))}
    </SideList>
  );
}

function Edited() {
  const query = useEdited();
  const pages = rowsOf(query, (window) => window.pages);
  return (
    <SideList title={t.home.editedTitle} list="edited" query={query} count={pages.length} empty={t.home.editedEmpty}>
      {pages.map((page) => (
        <li key={page.id} className={rowClass} data-edited-row={page.title}>
          <span className="flex min-w-0 flex-1 flex-col">
            <PageLink spaceKey={page.spaceKey} id={page.id} title={page.title} className={linkClass} />
            <span className="flex flex-wrap items-center gap-1.5 text-xs text-ink-subtle">
              {page.spaceName} · {day.format(new Date(page.editedAt))}
              {page.unpublished ? <Tag>{t.home.unpublished}</Tag> : page.draft && <Tag>{t.home.draft}</Tag>}
            </span>
          </span>
        </li>
      ))}
      <More query={query} list="edited" />
    </SideList>
  );
}

function Updates({ scope }: { scope: UpdateScope }) {
  const query = useUpdates(scope);
  const updates = rowsOf(query, (window) => window.updates);
  if (query.isError) return <ErrorBanner onRetry={() => void query.refetch()}>{t.home.loadFailed}</ErrorBanner>;
  if (query.isLoading) return <Skeleton />;
  if (updates.length === 0) {
    return <p className="py-4 text-sm text-ink-muted">{scope === "watched" ? t.home.updatesEmptyWatched : t.home.updatesEmptyAll}</p>;
  }
  return (
    <div className="flex flex-col gap-1">
      <ul className="flex flex-col divide-y divide-border">
        {updates.map((update) => (
          <li key={update.id} className="flex min-w-0 flex-col gap-0.5 py-3" data-update-row={update.title}>
            <span className="flex min-w-0 items-center gap-2">
              <PageLink
                spaceKey={update.spaceKey}
                id={update.id}
                title={update.title}
                className="truncate font-medium text-ink hover:text-accent hover:underline"
              />
              {update.verified && <VerifiedMark />}
            </span>
            <span className="text-sm text-ink-muted">{t.home.published(update.authorName || t.home.somebody, update.version)}</span>
            {update.comment && <span className="text-sm break-words text-ink">{update.comment}</span>}
            <span className="text-xs text-ink-subtle">
              {update.spaceName} · <time dateTime={update.publishedAt}>{moment.format(new Date(update.publishedAt))}</time>
            </span>
          </li>
        ))}
      </ul>
      <More query={query} list="updates" />
    </div>
  );
}

/** The home page: what changed that the reader may read, beside their stars, their recent pages and their own edits. */
export function HomeScreen() {
  const navigate = useNavigate();
  const [scope, setScope] = useState<UpdateScope>("all");
  const [notice, setNotice] = useState("");
  const panelId = useId();
  const headingId = useId();
  const welcome = useWelcome();

  return (
    <div className="mx-auto max-w-5xl" data-home="">
      <PageHeader title={t.home.title} />
      {welcome && (
        <div className="mb-8">
          <EmptyState
            icon={<Icon.Home />}
            title={t.home.emptyTitle}
            description={t.home.emptyBody}
            action={
              <Button variant="secondary" onClick={() => navigate({ to: "/spaces" })}>
                {t.home.emptyAction}
              </Button>
            }
          />
        </div>
      )}
      <p role="status" className="sr-only">
        {notice}
      </p>
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <section aria-labelledby={headingId} className="min-w-0" data-home-list="updates">
          <SectionTitle id={headingId} className="mb-2">
            {t.home.updatesTitle}
          </SectionTitle>
          <Tabs
            label={t.home.updatesTitle}
            value={scope}
            onChange={setScope}
            panelId={panelId}
            tabs={[
              { value: "all", label: t.home.updatesAll, attrs: { "data-scope": "all" } },
              { value: "watched", label: t.home.updatesWatched, attrs: { "data-scope": "watched" } },
            ]}
          />
          <TabPanel id={panelId} label={scope === "watched" ? t.home.updatesWatched : t.home.updatesAll}>
            <Updates scope={scope} />
          </TabPanel>
        </section>
        <div className="flex min-w-0 flex-col gap-8">
          <Starred onNotice={setNotice} />
          <Recent />
          <Edited />
        </div>
      </div>
    </div>
  );
}

// A first visit has nothing in any list; it is told what the home page is for
// once every list has answered, so the welcome never flashes past a slow one.
function useWelcome(): boolean {
  const stars = useStars();
  const recent = useRecentPages();
  const edited = useEdited();
  const updates = useUpdates("all");
  if ([stars, recent, edited, updates].some((each) => each.isLoading)) return false;
  return (
    (stars.data?.pages[0]?.stars.length ?? 0) === 0 &&
    (recent.data?.length ?? 0) === 0 &&
    (edited.data?.pages[0]?.pages.length ?? 0) === 0 &&
    (updates.data?.pages[0]?.updates.length ?? 0) === 0
  );
}
