import { localDateFormat } from "@/lib/format";
import { Link } from "@tanstack/react-router";
import { labelPath, useLabelPages, type LabeledPage } from "@/api/labels";
import type { Space } from "@/api/spaces";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { LABEL_PAGE_SIZE } from "@/config";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { LabelLink } from "./PageLabels";

const changedOn = localDateFormat({ dateStyle: "medium" });

/** The pages that carry a label, in one space or in all of them, a page of the list at a time. */
export function LabelScreen({ name, space, page, onPage }: { name: string; space?: Space; page: number; onPage: (page: number) => void }) {
  const { data, error, isLoading, isPlaceholderData, refetch } = useLabelPages(name, space?.key, (page - 1) * LABEL_PAGE_SIZE);
  const pages = data ? Math.max(1, Math.ceil(data.total / LABEL_PAGE_SIZE)) : 1;
  const rows = data?.pages ?? [];

  return (
    <div data-label-screen={name}>
      <PageHeader
        crumbs={
          space
            ? [
                {
                  label: space.name,
                  render: (label) => (
                    <Link to="/s/$spaceKey" params={{ spaceKey: space.key }}>
                      {label}
                    </Link>
                  ),
                },
              ]
            : undefined
        }
        title={
          <span className="inline-flex items-center gap-2">
            <Icon.Label />
            <span data-label-heading>{name}</span>
          </span>
        }
        meta={
          <span className="flex flex-wrap items-center gap-3">
            {space ? t.labels.inSpace(space.name) : t.labels.everywhere}
            {space && (
              <Link {...labelPath(name)} className="text-accent hover:underline" data-action="label-everywhere">
                {t.labels.showEverywhere}
              </Link>
            )}
          </span>
        }
      />
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message || t.labels.failed}</ErrorBanner>
      ) : isLoading || !data ? (
        <Skeleton rows={5} />
      ) : rows.length === 0 ? (
        <EmptyState icon={<Icon.Label />} title={t.labels.emptyTitle} description={t.labels.emptyBody} />
      ) : (
        <>
          <p className="mb-2 text-sm text-ink-subtle tabular-nums" role="status" data-label-count>
            {t.labels.count(data.total, data.offset + 1, data.offset + rows.length)}
          </p>
          <ol className="divide-y divide-border" aria-label={t.labels.heading(name)} aria-busy={isPlaceholderData || undefined}>
            {rows.map((each) => (
              <Row key={each.id} page={each} current={name} spaceKey={space?.key} />
            ))}
          </ol>
          {pages > 1 && (
            <nav aria-label={t.labels.paging} className="mt-4 flex items-center justify-between gap-2" data-label-paging>
              <Button variant="secondary" size="sm" disabled={page <= 1} onClick={() => onPage(page - 1)} data-action="previous-labeled">
                {t.labels.previous}
              </Button>
              <span className="text-sm text-ink-subtle tabular-nums">{t.labels.pageOf(page, pages)}</span>
              <Button variant="secondary" size="sm" disabled={page >= pages} onClick={() => onPage(page + 1)} data-action="next-labeled">
                {t.labels.next}
              </Button>
            </nav>
          )}
        </>
      )}
    </div>
  );
}

function Row({ page, current, spaceKey }: { page: LabeledPage; current: string; spaceKey?: string }) {
  const others = page.labels.filter((each) => each !== current);
  return (
    <li className="py-3" data-labeled-page={page.title}>
      <div className="flex flex-wrap items-center gap-2">
        <PageLink spaceKey={page.spaceKey} id={page.id} title={page.title} className="font-medium text-accent hover:underline">
          {page.title}
        </PageLink>
        {page.unpublished && <Tag>{t.labels.unpublished}</Tag>}
      </div>
      <p className="mt-0.5 text-xs text-ink-subtle">
        {[page.spaceName, ...page.path.slice(1)].join(" / ")}
        {" / "}
        {t.labels.changed(page.updatedByName, changedOn.format(new Date(page.updatedAt)))}
      </p>
      {others.length > 0 && (
        <ul className="mt-1.5 flex flex-wrap gap-1.5" aria-label={t.labels.title}>
          {others.map((name) => (
            <li key={name}>
              <LabelLink name={name} spaceKey={spaceKey} />
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}
