import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useSpaces } from "@/api/spaces";
import { useStalePages, type StaleFilter, type StalePage, type StaleVerification } from "@/api/stale";
import { Button, EmptyState, ErrorBanner, PageHeader, Select, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { STALE_AGE_DAYS, STALE_DEFAULT_DAYS, STALE_REVIEW_FROM } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { pageSlug } from "@/lib/slug";

const day = localDateFormat({ dateStyle: "medium" });

const VERIFICATIONS: StaleVerification[] = ["verified", "expired", "none"];

/** The pages nobody published or opened for a while, in the spaces the reader administers, with filters and pages of rows. */
export function StaleReport({ space }: { space?: string }) {
  const [filter, setFilter] = useState<StaleFilter>({ space, olderThan: STALE_DEFAULT_DAYS });
  const [ownerName, setOwnerName] = useState("");
  // Each page is reached by the cursor the one before handed out; going back pops it.
  const [cursors, setCursors] = useState<string[]>([]);
  const report = useStalePages(filter, cursors[cursors.length - 1]);
  const spaces = (useSpaces().data ?? []).filter((each) => each.can.administer);
  const rows = report.data?.pages ?? [];
  const next = report.data?.next ?? null;
  const problem = report.error instanceof ApiError ? report.error : null;
  const filtered = Boolean(filter.space || filter.owner || filter.verification) || filter.olderThan !== STALE_DEFAULT_DAYS;

  function narrow(patch: Partial<StaleFilter>) {
    setFilter((current) => ({ ...current, ...patch }));
    setCursors([]);
  }

  function clear() {
    setFilter({ olderThan: STALE_DEFAULT_DAYS });
    setOwnerName("");
    setCursors([]);
  }

  if (problem?.status === 403) {
    return (
      <div className="mx-auto max-w-3xl" data-stale-refused>
        <PageHeader crumb={t.settings.title} title={t.stale.title} />
        <EmptyState icon={<Icon.Lock />} title={t.stale.title} description={t.stale.notAdmin} />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-5xl" data-stale-report>
      <PageHeader crumb={t.settings.title} title={t.stale.title} />
      <p className="mb-4 text-sm text-ink-muted">{t.stale.intro(filter.olderThan)}</p>
      <section aria-label={t.stale.filters} className="mb-4 space-y-3" data-stale-filters>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Select label={t.stale.space} id="stale-space" value={filter.space ?? ""} onChange={(e) => narrow({ space: e.target.value || undefined })}>
            <option value="">{t.stale.everySpace}</option>
            {filter.space && !spaces.some((each) => each.key === filter.space) && <option value={filter.space}>{filter.space}</option>}
            {spaces.map((each) => (
              <option key={each.key} value={each.key}>
                {each.name}
              </option>
            ))}
          </Select>
          <Select label={t.stale.age} id="stale-age" value={String(filter.olderThan)} onChange={(e) => narrow({ olderThan: Number(e.target.value) })}>
            {STALE_AGE_DAYS.map((days) => (
              <option key={days} value={days}>
                {t.stale.ageDays(days)}
              </option>
            ))}
          </Select>
          <Select
            label={t.stale.owner}
            id="stale-owner"
            value={filter.owner ?? ""}
            onChange={(e) => {
              if (e.target.value !== filter.owner) setOwnerName("");
              narrow({ owner: e.target.value || undefined });
            }}
          >
            <option value="">{t.stale.anyOwner}</option>
            <option value="none">{t.stale.noOwner}</option>
            {filter.owner && filter.owner !== "none" && <option value={filter.owner}>{ownerName || filter.owner}</option>}
          </Select>
          <Select
            label={t.stale.verification}
            id="stale-verification"
            value={filter.verification ?? ""}
            onChange={(e) => narrow({ verification: (e.target.value || undefined) as StaleVerification | undefined })}
          >
            <option value="">{t.stale.anyVerification}</option>
            {VERIFICATIONS.map((each) => (
              <option key={each} value={each}>
                {t.stale.verifications[each]}
              </option>
            ))}
          </Select>
        </div>
        {filtered && (
          <Button variant="ghost" size="sm" onClick={clear} data-action="clear-stale-filters">
            {t.stale.clearFilters}
          </Button>
        )}
      </section>
      {report.error && <ErrorBanner onRetry={() => void report.refetch()}>{report.error.message}</ErrorBanner>}
      {report.isLoading ? (
        <Skeleton />
      ) : rows.length === 0 && !report.error ? (
        <EmptyState
          icon={<Icon.Seal />}
          title={t.stale.empty}
          description={cursors.length > 0 ? t.stale.emptyLast : filtered ? t.stale.emptyFiltered : t.stale.emptyBody}
        />
      ) : (
        rows.length > 0 && (
          <Table data-stale-table>
            <thead>
              <tr>
                <Th>{t.stale.columnPage}</Th>
                <Th className="w-36">{t.stale.columnPublished}</Th>
                <Th className="w-36">{t.stale.columnViewed}</Th>
                <Th className="w-44">{t.stale.columnOwner}</Th>
                <Th className="w-40">{t.stale.columnVerification}</Th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <StaleRow
                  key={row.id}
                  row={row}
                  onOwner={(id, name) => {
                    setOwnerName(name);
                    narrow({ owner: id });
                  }}
                />
              ))}
            </tbody>
          </Table>
        )
      )}
      {(cursors.length > 0 || next) && (
        <nav aria-label={t.stale.pages} className="mt-3 flex items-center justify-end gap-2">
          <span className="mr-auto text-sm text-ink-muted" data-stale-page>
            {t.stale.page(cursors.length + 1)}
          </span>
          <Button variant="secondary" size="sm" disabled={cursors.length === 0} onClick={() => setCursors(cursors.slice(0, -1))} data-action="stale-previous">
            {t.stale.previous}
          </Button>
          <Button variant="secondary" size="sm" disabled={!next} onClick={() => next && setCursors([...cursors, next])} data-action="stale-next">
            {t.stale.next}
          </Button>
        </nav>
      )}
    </div>
  );
}

function StaleRow({ row, onOwner }: { row: StalePage; onOwner: (id: string, name: string) => void }) {
  const expires = row.verificationExpiresAt ? day.format(new Date(row.verificationExpiresAt)) : "";
  return (
    <tr data-stale-row={row.title}>
      <Td className="text-sm">
        <Link
          to="/s/$spaceKey/p/$pageId/$slug"
          params={{ spaceKey: row.spaceKey, pageId: row.id, slug: pageSlug(row.title) }}
          search={{ from: STALE_REVIEW_FROM }}
          className="font-medium text-ink underline-offset-2 hover:underline"
          data-stale-page-link
        >
          {row.title}
        </Link>
        <span className="block text-2xs text-ink-subtle">{row.spaceName}</span>
      </Td>
      <Td className="text-sm whitespace-nowrap text-ink-muted">
        <time dateTime={row.publishedAt}>{day.format(new Date(row.publishedAt))}</time>
      </Td>
      <Td className="text-sm whitespace-nowrap text-ink-muted" data-stale-viewed>
        {row.viewedAt ? <time dateTime={row.viewedAt}>{day.format(new Date(row.viewedAt))}</time> : t.stale.neverViewed}
      </Td>
      <Td className="text-sm" data-stale-owner>
        {row.owner ? (
          <>
            <button
              type="button"
              className="text-left text-ink underline-offset-2 hover:underline"
              aria-label={t.stale.filterByOwner(row.owner.name)}
              onClick={() => row.owner && onOwner(row.owner.id, row.owner.name)}
              data-action="filter-owner"
            >
              {row.owner.name}
            </button>
            {!row.owner.canView && <span className="block text-2xs text-ink-muted">{t.stale.ownerNoAccess}</span>}
          </>
        ) : (
          <span className="text-ink-subtle">{t.stale.noOwner}</span>
        )}
      </Td>
      <Td className="text-sm" data-stale-verification={row.verification}>
        <span className={row.verification === "none" ? "text-ink-muted" : "font-medium text-ink"}>{t.stale.verifications[row.verification]}</span>
        {expires && <span className="block text-2xs text-ink-subtle">{row.verification === "verified" ? t.stale.until(expires) : t.stale.since(expires)}</span>}
      </Td>
    </tr>
  );
}
