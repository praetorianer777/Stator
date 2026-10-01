import { localDateFormat } from "@/lib/format";
import { useId, useState, type FormEvent } from "react";
import { useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import { HIT_TYPES, usePeople, useSearch, type Hit, type HitType, type SearchRequest, type SearchSort } from "@/api/search";
import { useSpaces } from "@/api/spaces";
import { Button, Checkbox, EmptyState, ErrorBanner, Field, Input, PageHeader, SectionTitle, Select, Skeleton, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SEARCH_PAGE_SIZE, SEARCH_QUERY_MAX_LENGTH } from "@/config";
import { LabelCombobox } from "@/features/labels/LabelCombobox";
import { LabelLink } from "@/features/labels/PageLabels";
import { VerifiedMark } from "@/features/stewardship/VerificationBadge";
import { ArchivedMark } from "@/features/archive/ArchiveBanner";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { Highlight } from "./Highlight";

/** The search as the address holds it: lists joined by commas, and the page of results counted from 1. */
export interface SearchAddress {
  q?: string;
  space?: string;
  type?: string;
  label?: string;
  author?: string;
  updatedAfter?: string;
  updatedBefore?: string;
  sort?: SearchSort;
  /** Archived pages and spaces are left out unless this is set. */
  archived?: boolean;
  page?: number;
}

const FILTERS = ["space", "type", "label", "author", "updatedAfter", "updatedBefore", "archived"] as const;

const list = (value: string | undefined): string[] =>
  (value ?? "")
    .split(",")
    .map((each) => each.trim())
    .filter(Boolean);
const joined = (values: string[]): string | undefined => (values.length > 0 ? values.join(",") : undefined);

function request(address: SearchAddress): SearchRequest {
  return {
    q: address.q ?? "",
    space: list(address.space),
    type: list(address.type).filter((each): each is HitType => (HIT_TYPES as string[]).includes(each)),
    label: list(address.label),
    author: list(address.author),
    updatedAfter: address.updatedAfter,
    updatedBefore: address.updatedBefore,
    sort: address.sort,
    archived: address.archived === true,
    offset: ((address.page ?? 1) - 1) * SEARCH_PAGE_SIZE,
  };
}

// Only a refused query is the reader's to fix, and the server says how; anything else is ours.
function problem(error: Error): string {
  return error instanceof ApiError && error.status === 422 ? error.message : t.search.failed;
}

const changedOn = localDateFormat({ dateStyle: "medium" });

/** The full search: words, the filters beside them, and the hits a page at a time. */
export function SearchScreen({ address, onChange }: { address: SearchAddress; onChange: (next: SearchAddress, replace: boolean) => void }) {
  const wanted = request(address);
  const { data, error, isLoading, isPlaceholderData, refetch } = useSearch(wanted);
  const [draft, setDraft] = useState(address.q ?? "");
  const [seenQ, setSeenQ] = useState(address.q);
  // The address can change under the form, from quick search or Back, and the box follows it.
  if (seenQ !== address.q) {
    setSeenQ(address.q);
    setDraft(address.q ?? "");
  }

  const without = (next: SearchAddress): SearchAddress =>
    Object.fromEntries(Object.entries(next).filter(([, value]) => value !== undefined && value !== "")) as SearchAddress;
  const filter = (patch: Partial<SearchAddress>) => onChange(without({ ...address, ...patch, page: undefined }), true);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    onChange(without({ ...address, q: draft.trim() || undefined, page: undefined }), false);
  };

  const hits = data?.hits ?? [];
  const page = address.page ?? 1;
  const pages = data ? Math.max(1, Math.ceil(data.total / SEARCH_PAGE_SIZE)) : 1;
  const filtered = FILTERS.some((name) => address[name]);

  return (
    <>
      <PageHeader title={t.search.title} />
      {/* biome-ignore lint/a11y/useSemanticElements: jsdom, which the unit tests run in, does not know the search element yet */}
      <form role="search" onSubmit={submit} className="mb-6 flex max-w-2xl items-end gap-2" data-search-form>
        <div className="min-w-0 flex-1">
          <label htmlFor="search-query" className="mb-1 block text-sm font-medium text-ink-muted">
            {t.search.queryLabel}
          </label>
          <Input
            id="search-query"
            type="search"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={t.search.queryPlaceholder}
            maxLength={SEARCH_QUERY_MAX_LENGTH}
            controlSize="lg"
            data-search-query
          />
        </div>
        <Button type="submit" size="lg" icon={<Icon.Search />} data-action="run-search">
          {t.search.submit}
        </Button>
      </form>

      <div className="grid gap-6 shell:grid-cols-[14rem_minmax(0,1fr)]">
        <Filters address={address} onFilter={filter} filtered={filtered} />
        <section aria-label={t.search.title} className="min-w-0" data-search-results>
          {error ? (
            <ErrorBanner onRetry={() => void refetch()}>
              <span data-search-error>{problem(error)}</span>
            </ErrorBanner>
          ) : isLoading || !data ? (
            <Skeleton rows={5} />
          ) : hits.length === 0 ? (
            <EmptyState icon={<Icon.Search />} title={t.search.emptyTitle} description={t.search.emptyBody} />
          ) : (
            <>
              <p className="mb-2 text-sm text-ink-subtle tabular-nums" role="status" data-search-count>
                {t.search.count(data.total, data.offset + 1, data.offset + hits.length)}
              </p>
              <ol className="divide-y divide-border" aria-busy={isPlaceholderData || undefined}>
                {hits.map((hit) => (
                  <HitRow key={`${hit.type}:${hit.attachmentId ?? hit.commentId ?? hit.page.id}`} hit={hit} />
                ))}
              </ol>
              {pages > 1 && (
                <nav aria-label={t.search.paging} className="mt-4 flex items-center justify-between gap-2" data-search-paging>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={page <= 1}
                    onClick={() => onChange({ ...address, page: page - 1 > 1 ? page - 1 : undefined }, false)}
                    data-action="previous-results"
                  >
                    {t.search.previous}
                  </Button>
                  <span className="text-sm text-ink-subtle tabular-nums">{t.search.pageOf(page, pages)}</span>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={page >= pages}
                    onClick={() => onChange({ ...address, page: page + 1 }, false)}
                    data-action="next-results"
                  >
                    {t.search.next}
                  </Button>
                </nav>
              )}
            </>
          )}
        </section>
      </div>
    </>
  );
}

function HitRow({ hit }: { hit: Hit }) {
  const title = hit.title.length > 0 ? hit.title : [{ text: hit.page.title, match: false }];
  return (
    <li className="py-3" data-search-hit={hit.page.title} data-hit-type={hit.type}>
      <div className="flex flex-wrap items-center gap-2">
        <PageLink spaceKey={hit.page.spaceKey} id={hit.page.id} title={hit.page.title} className="font-medium text-accent hover:underline">
          <Highlight segments={title} />
        </PageLink>
        {hit.type === "page" && hit.verified && <VerifiedMark />}
        {hit.archived && <ArchivedMark />}
        {hit.type !== "page" && <Tag>{t.search.typeTag[hit.type]}</Tag>}
        {hit.labels.map((label) => (
          <LabelLink key={label} name={label} />
        ))}
      </div>
      <p className="mt-0.5 text-xs text-ink-subtle">
        {hit.page.spaceName}
        {hit.type !== "page" && ` / ${t.search.on(hit.page.title)}`}
        {" / "}
        {t.search.changed(hit.updatedByName, changedOn.format(new Date(hit.updatedAt)))}
      </p>
      {hit.snippet.length > 0 && (
        <p className="mt-1 text-sm text-ink-muted" data-search-snippet>
          <Highlight segments={hit.snippet} />
        </p>
      )}
    </li>
  );
}

function Filters({ address, onFilter, filtered }: { address: SearchAddress; onFilter: (patch: Partial<SearchAddress>) => void; filtered: boolean }) {
  const headingId = useId();
  const { data: spaces } = useSpaces(address.archived === true);
  const { data: me } = useMe();
  const { data: people } = usePeople();
  const types = list(address.type);
  const labels = list(address.label);
  const space = list(address.space)[0] ?? "";
  const author = list(address.author)[0] ?? "";
  const others = (people ?? []).filter((person) => person.id !== me?.user.id);

  return (
    <section aria-labelledby={headingId} className="space-y-4" data-search-filters>
      <div className="flex items-center justify-between gap-2">
        <SectionTitle id={headingId}>{t.search.filters}</SectionTitle>
        {filtered && (
          <Button
            variant="link"
            onClick={() =>
              onFilter({
                space: undefined,
                type: undefined,
                label: undefined,
                author: undefined,
                updatedAfter: undefined,
                updatedBefore: undefined,
                archived: undefined,
              })
            }
            data-action="clear-filters"
          >
            {t.search.clear}
          </Button>
        )}
      </div>
      <Select label={t.search.space} value={space} onChange={(e) => onFilter({ space: e.target.value || undefined })} data-filter="space">
        <option value="">{t.search.anySpace}</option>
        {(spaces ?? []).map((each) => (
          <option key={each.key} value={each.key}>
            {each.name}
          </option>
        ))}
      </Select>
      <fieldset className="flex flex-col items-start gap-1.5" data-filter="type">
        <legend className="mb-1 text-sm font-medium text-ink-muted">{t.search.type}</legend>
        {HIT_TYPES.map((type) => (
          <Checkbox
            key={type}
            label={t.search.types[type]}
            checked={types.includes(type)}
            onChange={(e) => onFilter({ type: joined(e.target.checked ? [...types, type] : types.filter((each) => each !== type)) })}
            data-filter-type={type}
          />
        ))}
      </fieldset>
      <div data-filter="label">
        <LabelCombobox
          label={t.search.labels}
          exclude={labels}
          newOption={t.search.labelFilter}
          onPick={(name) => onFilter({ label: joined([...labels, name]) })}
          onRemoveLast={() => onFilter({ label: joined(labels.slice(0, -1)) })}
          hint={t.search.labelsHint}
        />
        {labels.length > 0 && (
          <ul className="mt-2 flex flex-wrap gap-1.5" aria-label={t.search.labels} data-label-filters>
            {labels.map((name) => (
              <li key={name}>
                <LabelLink name={name} onRemove={() => onFilter({ label: joined(labels.filter((each) => each !== name)) })} />
              </li>
            ))}
          </ul>
        )}
      </div>
      <Select label={t.search.author} value={author} onChange={(e) => onFilter({ author: e.target.value || undefined })} data-filter="author">
        <option value="">{t.search.anyone}</option>
        {me && <option value={me.user.id}>{t.search.you(me.user.name)}</option>}
        {others.map((person) => (
          <option key={person.id} value={person.id}>
            {person.name}
          </option>
        ))}
        {author && author !== me?.user.id && !others.some((person) => person.id === author) && <option value={author}>{author}</option>}
      </Select>
      <Field
        label={t.search.after}
        type="date"
        value={address.updatedAfter ?? ""}
        max={address.updatedBefore}
        onChange={(e) => onFilter({ updatedAfter: e.target.value || undefined })}
        data-filter="updated-after"
      />
      <Field
        label={t.search.before}
        type="date"
        value={address.updatedBefore ?? ""}
        min={address.updatedAfter}
        onChange={(e) => onFilter({ updatedBefore: e.target.value || undefined })}
        data-filter="updated-before"
      />
      <Checkbox
        label={t.archive.searchFilter}
        checked={address.archived === true}
        onChange={(e) => onFilter({ archived: e.target.checked || undefined })}
        data-filter="archived"
      />
      <Select
        label={t.search.sort}
        value={address.sort ?? "relevance"}
        onChange={(e) => onFilter({ sort: e.target.value === "updated" ? "updated" : undefined })}
        data-filter="sort"
      >
        <option value="relevance">{t.search.relevance}</option>
        <option value="updated">{t.search.updated}</option>
      </Select>
    </section>
  );
}
