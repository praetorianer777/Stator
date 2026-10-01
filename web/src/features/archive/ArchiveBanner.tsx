import { Link } from "@tanstack/react-router";
import { useArchivePage } from "@/api/archive";
import type { Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { Button, ErrorBanner, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const archivedOn = localDateFormat({ dateStyle: "medium" });

const shape = "archive-mark inline-flex items-center gap-1 rounded-control font-medium whitespace-nowrap";

/** The small mark a list or a header puts beside something archived; the tint comes from index.css. */
export function ArchivedMark({ className }: { className?: string }) {
  return (
    <span className={cx(shape, "h-5 px-1 text-2xs", className)} data-archived-mark="">
      <Icon.Archive />
      {t.archive.tag}
    </span>
  );
}

/** Says a page is archived, how and since when, and offers the way back to whoever may take it. */
export function ArchiveBanner({ page, space }: { page: Page; space: Space }) {
  const unarchive = useArchivePage();
  const archived = page.archived;
  if (!archived) return null;
  const item = archived.page;
  const own = item?.id === page.id;
  const link = "font-medium underline hover:no-underline";
  return (
    <>
      {unarchive.error && <ErrorBanner>{unarchive.error.message}</ErrorBanner>}
      <div
        className="archive-mark mb-4 flex flex-wrap items-center gap-3 rounded-control px-3 py-2 text-sm"
        data-archived-banner={own ? "page" : item ? "with" : "space"}
      >
        <Icon.Archive />
        <p className="min-w-0 flex-1">
          {item ? t.archive.bannerPage : t.archive.bannerSpace}
          {item && !own && ` ${t.archive.bannerWith(item.title)}`} {t.archive.by(archived.archivedByName, archivedOn.format(new Date(archived.archivedAt)))}
        </p>
        {item && own && page.can.archive && (
          <Button
            size="sm"
            variant="secondary"
            loading={unarchive.isPending}
            onClick={() => unarchive.mutate({ id: page.id, archived: false })}
            data-action="unarchive-page"
          >
            {t.archive.unarchive}
          </Button>
        )}
        {item && !own && (
          <PageLink spaceKey={space.key} id={item.id} title={item.title} className={link}>
            {t.archive.goTo(item.title)}
          </PageLink>
        )}
        {!item && space.can.administer && (
          <Link to="/s/$spaceKey/settings" params={{ spaceKey: space.key }} className={link}>
            {t.archive.spaceSettings}
          </Link>
        )}
      </div>
    </>
  );
}
