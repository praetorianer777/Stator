import { useDecisions, type DecisionFilter } from "@/api/decisions";
import { useSpace } from "@/api/spaces";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const updatedAt = localDateFormat({ dateStyle: "medium" });
const FILTERS: Array<{ state: DecisionFilter; label: string }> = [
  { state: undefined, label: t.decisions.all },
  { state: "decided", label: t.decisions.decided },
  { state: "undecided", label: t.decisions.undecided },
];

/** Every decision item on a space's pages the reader may read, newest page first, each linked to its page. */
export function DecisionLog({ spaceKey, state, onState }: { spaceKey: string; state: DecisionFilter; onState: (state: DecisionFilter) => void }) {
  const { data: space } = useSpace(spaceKey);
  const { data, isLoading, error, refetch } = useDecisions(spaceKey, state);
  return (
    <div className="mx-auto max-w-3xl" data-decision-log={spaceKey}>
      <PageHeader crumb={space?.name ?? spaceKey} title={t.decisions.title} />
      <div role="group" aria-label={t.decisions.filter} className="mb-4 flex gap-2">
        {FILTERS.map((filter) => (
          <Button
            key={filter.label}
            size="sm"
            variant={filter.state === state ? "primary" : "secondary"}
            aria-pressed={filter.state === state}
            onClick={() => onState(filter.state)}
            data-decision-filter={filter.state ?? "all"}
          >
            {filter.label}
          </Button>
        ))}
      </div>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {data && data.decisions.length === 0 && (
        <EmptyState icon={<Icon.Decision />} title={t.decisions.title} description={state ? t.decisions.emptyFiltered : t.decisions.empty} />
      )}
      {data && data.decisions.length > 0 && (
        <ul className="divide-y divide-border rounded-control border border-border">
          {data.decisions.map((decision, i) => (
            <li key={`${decision.pageId}-${i}`} className="flex items-start gap-3 px-3 py-2" data-decision-row={decision.state}>
              <span className="doc-decision-badge mt-0.5 shrink-0" data-decision={decision.state}>
                {decision.state === "decided" ? t.decisions.decided : t.decisions.undecided}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-ink">{decision.text}</span>
                <span className="block text-sm text-ink-muted">
                  <PageLink spaceKey={spaceKey} id={decision.pageId} title={decision.pageTitle} className="hover:text-accent hover:underline">
                    {t.decisions.onPage(decision.pageTitle, updatedAt.format(new Date(decision.updatedAt)))}
                  </PageLink>
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
      {data?.truncated && <p className="mt-3 text-sm text-ink-muted">{t.decisions.truncated}</p>}
    </div>
  );
}
