import { localDateFormat } from "@/lib/format";
import { useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { pageQueryKey, usePage, type Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { useComparison, useRestoreVersion, useVersion, useVersions, type CompareRef, type CompareSide, type VersionEntry } from "@/api/versions";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Table, Tag, Td, Th, cx, type Crumb } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DocPageContext } from "@/features/editor/BlockViews";
import { DocDiffView, DocView } from "@/features/editor/DocView";
import { HISTORY_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { PageLink } from "./PageLink";
import { PAGE_SHEET_HEADER, pageSheet } from "./pageSheet";
import { pageCrumbs } from "./PageScreen";

/** Where in a page's history the reader is: the list, one version, or two sides compared. */
export interface HistorySearch {
  version?: number;
  from?: CompareRef;
  to?: CompareRef;
  offset?: number;
}

const publishedAt = localDateFormat({ dateStyle: "medium", timeStyle: "short" });
const when = (iso: string) => publishedAt.format(new Date(iso));

/** A page's published versions: listed, read one at a time, compared and restored. */
export function PageHistory({ pageId, search, onSearch }: { pageId: string; search: HistorySearch; onSearch: (next: HistorySearch) => void }) {
  const { data, isLoading, error, refetch } = usePage(pageId);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (isLoading || !data) return <Skeleton />;
  const props = { page: data.page, space: data.space, onSearch };
  if (search.version !== undefined) return <VersionScreen key={search.version} number={search.version} {...props} />;
  if (search.from !== undefined || search.to !== undefined) return <CompareScreen from={search.from} to={search.to} {...props} />;
  return <HistoryList offset={search.offset ?? 0} {...props} />;
}

interface ScreenProps {
  page: Page;
  space: Space;
  onSearch: (next: HistorySearch) => void;
}

/** The crumbs up to the page itself, and on to its history when the screen is below that. */
function historyCrumbs(space: Space, page: Page, withHistory: boolean): Crumb[] {
  const crumbs = [
    ...pageCrumbs(space, page),
    {
      label: page.home ? space.name : page.title,
      render: (label: ReactNode) => (
        <PageLink spaceKey={space.key} id={page.id} title={page.title} home={page.home}>
          {label}
        </PageLink>
      ),
    },
  ];
  if (withHistory) {
    crumbs.push({
      label: t.history.crumb,
      render: (label) => (
        <Link to="/s/$spaceKey/p/$pageId/$slug/history" params={{ spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) }}>
          {label}
        </Link>
      ),
    });
  }
  return crumbs;
}

function sideLabel(side: { number: number; draft: boolean }): string {
  if (side.draft) return t.history.yourDraft;
  return side.number === 0 ? t.history.emptyPage : t.history.version(side.number);
}

/** Restores a version after asking, and says how it went; shared by the list and a version's own screen. */
function useRestore(page: Page) {
  const restore = useRestoreVersion(page.id);
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState("");
  const [failure, setFailure] = useState("");
  const run = (number: number) => {
    if (!window.confirm(t.history.confirmRestore(number))) return;
    setNotice("");
    setFailure("");
    restore.mutate(
      { number, baseVersion: page.version },
      {
        onSuccess: (saved) => setNotice(t.history.restored(number, saved.version)),
        onError: (error) => {
          if (error instanceof ApiError && error.code === "conflict") {
            setFailure(t.history.restoreConflict);
            void queryClient.invalidateQueries({ queryKey: pageQueryKey(page.id) });
          } else setFailure(error.message);
        },
      },
    );
  };
  const messages = (
    <>
      <p role="status" className="text-sm text-ink-muted [&:empty]:hidden" data-history-notice>
        {notice}
      </p>
      {failure && <ErrorBanner>{failure}</ErrorBanner>}
    </>
  );
  return { run, pending: restore.isPending, messages };
}

function HistoryList({ page, space, offset, onSearch }: ScreenProps & { offset: number }) {
  const { data, isLoading, error, refetch } = useVersions(page.id, offset);
  const restore = useRestore(page);
  const navigate = useNavigate();
  const [selected, setSelected] = useState<CompareRef[]>([]);
  const canEdit = page.can.edit;

  const toggle = (ref: CompareRef, on: boolean) =>
    // A third pick lets go of the oldest pick, so two stay selected.
    setSelected((before) => (on ? [...before.filter((each) => each !== ref), ref].slice(-2) : before.filter((each) => each !== ref)));
  const order = (ref: CompareRef) => (ref === "draft" ? Number.POSITIVE_INFINITY : ref);
  const compareSelected = () => {
    const [older, newer] = [...selected].sort((a, b) => order(a) - order(b));
    onSearch({ from: older, to: newer });
  };

  const rows: { ref: CompareRef; entry?: VersionEntry }[] = [];
  if (page.draft && offset === 0) rows.push({ ref: "draft" });
  for (const entry of data?.versions ?? []) rows.push({ ref: entry.number, entry });
  const total = data?.total ?? 0;

  return (
    <div className="mx-auto max-w-4xl space-y-4" data-page-history={page.id}>
      <PageHeader crumbs={historyCrumbs(space, page, false)} title={t.history.title(page.title)} meta={t.history.intro} />
      {restore.messages}
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {data && rows.length === 0 && <EmptyState icon={<Icon.Lines />} title={t.history.empty} />}
      {rows.length > 0 && (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="secondary" disabled={selected.length !== 2} onClick={compareSelected} data-action="compare-selected">
              {t.history.compareSelected}
            </Button>
            <p className="text-sm text-ink-muted">{t.history.compareHint}</p>
          </div>
          <Table>
            <thead>
              <tr>
                <Th className="w-8">
                  <span className="sr-only">{t.history.compareSelected}</span>
                </Th>
                <Th>{t.history.columnVersion}</Th>
                <Th>{t.history.columnPublished}</Th>
                <Th>{t.history.columnComment}</Th>
                <Th>
                  <span className="sr-only">{t.history.columnActions}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ ref, entry }) => {
                const label = ref === "draft" ? t.history.yourDraft : t.history.version(ref);
                return (
                  <tr key={String(ref)} data-version-row={ref}>
                    <Td>
                      <input
                        type="checkbox"
                        className="size-4 rounded-[4px] border-border-strong accent-accent"
                        aria-label={t.history.select(label)}
                        checked={selected.includes(ref)}
                        onChange={(event) => toggle(ref, event.target.checked)}
                      />
                    </Td>
                    <Td>
                      {entry ? (
                        <Link
                          to="/s/$spaceKey/p/$pageId/$slug/history"
                          params={{ spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) }}
                          search={{ version: entry.number }}
                          className="font-medium text-ink underline-offset-2 hover:underline"
                        >
                          {label}
                        </Link>
                      ) : (
                        <span className="font-medium text-ink">{label}</span>
                      )}
                      {entry?.number === page.version && <Tag className="ml-2">{t.history.latest}</Tag>}
                      {entry && entry.title !== page.title && <span className="block text-xs text-ink-muted">{entry.title}</span>}
                    </Td>
                    <Td className="text-ink-muted">
                      {entry ? t.history.published(entry.authorName, when(entry.createdAt)) : ""}
                      {entry?.live && (
                        <span className="block text-xs" data-live-version="">
                          {t.live.versionSaved(when(entry.updatedAt), entry.coEditors.join(", "))}
                        </span>
                      )}
                    </Td>
                    <Td>
                      {entry &&
                        (entry.comment ? (
                          <span className="text-ink">{entry.comment}</span>
                        ) : (
                          <span className="text-ink-subtle">{entry.live ? t.live.savedAsTyped : t.history.noComment}</span>
                        ))}
                      {entry?.restoredFrom != null && <span className="block text-xs text-ink-muted">{t.history.restoredFrom(entry.restoredFrom)}</span>}
                    </Td>
                    <Td>
                      <div className="flex justify-end gap-2">
                        {entry ? (
                          <Button
                            size="sm"
                            variant="ghost"
                            aria-label={t.history.compareWithPreviousOf(entry.number)}
                            onClick={() => onSearch({ from: entry.number - 1, to: entry.number })}
                            data-action="compare-previous"
                          >
                            {t.history.compareWithPrevious}
                          </Button>
                        ) : (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() =>
                              void navigate({
                                to: "/s/$spaceKey/p/$pageId/$slug/edit",
                                params: { spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) },
                              })
                            }
                            data-action="continue-draft"
                          >
                            {t.page.continueDraft}
                          </Button>
                        )}
                        {entry && canEdit && entry.number !== page.version && (
                          <Button
                            size="sm"
                            variant="secondary"
                            aria-label={t.history.restoreVersion(entry.number)}
                            loading={restore.pending}
                            onClick={() => restore.run(entry.number)}
                            data-action="restore-version"
                          >
                            {t.history.restore}
                          </Button>
                        )}
                      </div>
                    </Td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
          {total > HISTORY_PAGE_SIZE && (
            <div className="flex justify-between gap-2">
              <Button variant="secondary" disabled={offset === 0} onClick={() => onSearch({ offset: Math.max(0, offset - HISTORY_PAGE_SIZE) || undefined })}>
                {t.history.newer}
              </Button>
              <Button variant="secondary" disabled={offset + HISTORY_PAGE_SIZE >= total} onClick={() => onSearch({ offset: offset + HISTORY_PAGE_SIZE })}>
                {t.history.older}
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function VersionScreen({ page, space, number, onSearch }: ScreenProps & { number: number }) {
  const sheet = pageSheet(page.appearance.width);
  const { data: version, isLoading, error, refetch } = useVersion(page.id, number);
  const restore = useRestore(page);
  const latest = number === page.version;
  return (
    <article className={cx(sheet.className, "space-y-4")} style={sheet.style} data-page-version={number}>
      <PageHeader
        className={PAGE_SHEET_HEADER}
        style={sheet.style}
        crumbs={historyCrumbs(space, page, true)}
        title={t.history.viewingTitle(version?.title ?? page.title, number)}
        meta={
          version && (
            <>
              {t.history.published(version.authorName, when(version.createdAt))}
              {version.comment && <span className="block text-ink">{version.comment}</span>}
              {version.restoredFrom != null && <span className="block">{t.history.restoredFrom(version.restoredFrom)}</span>}
            </>
          )
        }
        actions={
          <>
            <Button variant="secondary" onClick={() => onSearch({ from: number - 1, to: number })} data-action="compare-previous">
              {t.history.compareWithPrevious}
            </Button>
            {page.can.edit && !latest && (
              <Button variant="secondary" loading={restore.pending} onClick={() => restore.run(number)} data-action="restore-version">
                {t.history.restoreVersion(number)}
              </Button>
            )}
          </>
        }
      />
      {restore.messages}
      {!latest && <p className="text-sm text-ink-muted">{t.history.oldVersionNote(page.version)}</p>}
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {version && (
        <DocPageContext value={{ id: page.id, spaceKey: space.key }}>
          <DocView doc={version.body} />
        </DocPageContext>
      )}
    </article>
  );
}

function SideFacts({ label, side }: { label: string; side: CompareSide }) {
  const name = sideLabel(side);
  const described = side.number === 0 && !side.draft ? name : t.history.side(name, side.authorName, when(side.createdAt));
  return (
    <div className="min-w-0">
      <dt className="text-2xs font-medium tracking-wide text-ink-subtle uppercase">{label}</dt>
      <dd className="text-sm text-ink">{described}</dd>
    </div>
  );
}

function CompareScreen({ page, space, from, to }: ScreenProps & { from?: CompareRef; to?: CompareRef }) {
  const sheet = pageSheet(page.appearance.width);
  const { data: comparison, isLoading, error, refetch } = useComparison(page.id, from, to);
  const navigate = useNavigate();
  const same = comparison && comparison.from.title === comparison.to.title && comparison.blocks.every((block) => block.change === "equal");
  return (
    <article className={cx(sheet.className, "space-y-4")} style={sheet.style} data-page-compare="">
      <PageHeader
        className={PAGE_SHEET_HEADER}
        style={sheet.style}
        crumbs={historyCrumbs(space, page, true)}
        title={t.history.compareTitle}
        actions={
          to === "draft" &&
          page.can.edit && (
            <Button
              variant="secondary"
              onClick={() =>
                void navigate({ to: "/s/$spaceKey/p/$pageId/$slug/edit", params: { spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) } })
              }
              data-action="continue-draft"
            >
              {t.page.continueDraft}
            </Button>
          )
        }
      />
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {comparison && (
        <>
          <dl className="grid gap-3 rounded-control border border-border bg-surface px-3 py-2 sm:grid-cols-2" data-compare-sides="">
            <SideFacts label={t.history.from} side={comparison.from} />
            <SideFacts label={t.history.to} side={comparison.to} />
          </dl>
          {comparison.from.title !== comparison.to.title && (
            <p className="text-sm text-ink" data-title-change="">
              {t.history.titleChanged(comparison.from.title, comparison.to.title)}
            </p>
          )}
          {same ? (
            <p className="text-sm text-ink-muted">{t.history.noDifferences}</p>
          ) : (
            <>
              <p className="text-sm text-ink-muted">{t.history.legend}</p>
              <DocDiffView blocks={comparison.blocks} />
            </>
          )}
        </>
      )}
    </article>
  );
}
