import { Link } from "@tanstack/react-router";
import { usePageReaders, usePageViews } from "@/api/pageviews";
import { Avatar, Button, Dialog, ErrorBanner, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PROFILE_PATH } from "@/config";
import { t } from "@/i18n";
import { formatNumber, localDateFormat } from "@/lib/format";

const day = localDateFormat({ dateStyle: "medium" });

const views = (n: number) => t.views.views(n, formatNumber(n));
const people = (n: number) => t.views.people(n, formatNumber(n));

/** How often the page was read, in the line under its title, as the way into its views; nothing until the counts arrive. */
export function PageViewsButton({ pageId, onOpen }: { pageId: string; onOpen: () => void }) {
  const { data } = usePageViews(pageId);
  if (!data) return null;
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={t.views.open(views(data.views), people(data.readers))}
      className="inline-flex h-6 items-center gap-1 rounded-control px-1 text-ink-muted hover:text-ink hover:underline"
      data-page-views={data.views}
    >
      <Icon.Eye />
      {views(data.views)}
    </button>
  );
}

function Figures({ heading, viewCount, readerCount, ...rest }: { heading: string; viewCount: number; readerCount: number; [attr: `data-${string}`]: string }) {
  return (
    <div {...rest} className="rounded-control border border-border bg-surface-raised p-3">
      <dt className="text-xs text-ink-muted">{heading}</dt>
      <dd className="mt-1 text-sm font-medium text-ink">{views(viewCount)}</dd>
      <dd className="text-sm text-ink-muted">{people(readerCount)}</dd>
    </div>
  );
}

/** The page's counts for anybody who may read it, and who read it for those who may edit it. */
export function PageViewsDialog({ pageId, onClose }: { pageId: string; onClose: () => void }) {
  const { data, error, refetch } = usePageViews(pageId);
  return (
    <Dialog title={t.views.title} onClose={onClose} data-page-views-dialog="">
      {error && <ErrorBanner onRetry={() => void refetch()}>{t.views.failed}</ErrorBanner>}
      {!data && !error && <Skeleton />}
      {data && (
        <div className="space-y-4">
          <dl className="grid grid-cols-2 gap-2">
            <Figures heading={t.views.inAll} viewCount={data.views} readerCount={data.readers} data-views-in-all="" />
            <Figures heading={t.views.lately(data.days)} viewCount={data.recentViews} readerCount={data.recentReaders} data-views-lately="" />
          </dl>
          <p className="text-xs text-ink-muted">{t.views.howCounted}</p>
          {data.canListReaders ? (
            <Readers pageId={pageId} />
          ) : (
            <p className="text-sm text-ink-muted" data-readers-editors-only="">
              {t.views.editorsOnly}
            </p>
          )}
          <p className="text-xs text-ink-muted">
            <Link to={PROFILE_PATH} className="underline hover:text-ink" onClick={onClose}>
              {t.views.yourChoice}
            </Link>
          </p>
        </div>
      )}
    </Dialog>
  );
}

function Readers({ pageId }: { pageId: string }) {
  const { data, error, refetch, fetchNextPage, hasNextPage, isFetchingNextPage } = usePageReaders(pageId);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{t.views.failed}</ErrorBanner>;
  const first = data?.pages[0];
  if (!data || !first) return <Skeleton />;
  const readers = data.pages.flatMap((each) => each.readers);
  return (
    <section aria-labelledby="page-readers" className="space-y-2" data-page-readers="">
      <h3 id="page-readers" className="text-sm font-semibold text-ink">
        {t.views.readers}
      </h3>
      <p className="text-xs text-ink-muted">{first.retentionDays > 0 ? t.views.readersIntro(first.retentionDays) : t.views.readersIntroForever}</p>
      {readers.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.views.noReaders}</p>
      ) : (
        <ul className="divide-y divide-border">
          {readers.map((reader) => (
            <li key={reader.id} className="flex items-center gap-3 py-2" data-reader={reader.name}>
              <Avatar name={reader.name} src={reader.avatarUrl} size="sm" />
              <div className="min-w-0 text-sm">
                <p className="truncate text-ink">{reader.name}</p>
                <p className="text-xs text-ink-muted">
                  {t.views.lastOpened(day.format(new Date(reader.viewedAt)))} · {t.views.onDays(reader.days)}
                </p>
              </div>
            </li>
          ))}
        </ul>
      )}
      {first.unnamed > 0 && (
        <p className="text-xs text-ink-muted" data-readers-unnamed={first.unnamed}>
          {t.views.unnamed(first.unnamed)}
        </p>
      )}
      {hasNextPage && (
        <Button size="sm" variant="secondary" onClick={() => void fetchNextPage()} disabled={isFetchingNextPage} data-action="more-readers">
          {t.views.more}
        </Button>
      )}
    </section>
  );
}
