import type { Page } from "@/api/pages";
import { useWatchers, type Watcher } from "@/api/watching";
import { Avatar, Dialog, ErrorBanner, Skeleton } from "@/components/ui";
import { t } from "@/i18n";

function via(watcher: Watcher): string {
  if (watcher.via === "space") return t.watch.via.space;
  if (watcher.via === "subtree" && watcher.viaPage) return t.watch.via.subtree(watcher.viaPage.title);
  return t.watch.via.page;
}

/** Everybody who hears about a new version of the page, by name, and through which watch. */
export function WatchersDialog({ page, onClose }: { page: Page; onClose: () => void }) {
  const { data, error, isLoading, refetch } = useWatchers(page.id);
  return (
    <Dialog title={t.watch.watchersTitle(page.title)} onClose={onClose} data-watchers-dialog="">
      <p className="mb-3 text-sm text-ink-muted">{t.watch.watchersIntro}</p>
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : isLoading || !data ? (
        <Skeleton />
      ) : data.watchers.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.watch.watchersNone}</p>
      ) : (
        <>
          <ul className="space-y-2" aria-label={t.watch.watchers}>
            {data.watchers.map((watcher) => (
              <li key={watcher.userId} className="flex items-center gap-2" data-watcher={watcher.name}>
                <Avatar name={watcher.name} size="sm" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm text-ink">{watcher.name}</span>
                  <span className="block truncate text-xs text-ink-muted">{via(watcher)}</span>
                </span>
              </li>
            ))}
          </ul>
          {data.total > data.watchers.length && <p className="mt-3 text-xs text-ink-muted">{t.watch.watchersMore(data.watchers.length, data.total)}</p>}
        </>
      )}
    </Dialog>
  );
}
