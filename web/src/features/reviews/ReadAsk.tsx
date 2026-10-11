import type { Page } from "@/api/pages";
import { useConfirmRead, useReadConfirmations } from "@/api/reviews";
import { Button, Dialog, ErrorBanner, SectionTitle, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const confirmedOn = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** Asks the reader to confirm they read a page that asks, and once they did, says when. */
export function ReadAskBanner({ page }: { page: Page }) {
  const confirm = useConfirmRead(page.id);
  const { since, confirmedAt } = page.readAsk;
  if (since === null) return null;
  if (confirmedAt) {
    return (
      <p role="status" className="mb-4 flex items-center gap-2 text-sm text-ink-muted" data-read-ask-banner="confirmed">
        <Icon.Check className="shrink-0 text-success" />
        {t.readAsk.confirmed(confirmedOn.format(new Date(confirmedAt)))}
      </p>
    );
  }
  return (
    <>
      {confirm.error && <ErrorBanner>{confirm.error.message}</ErrorBanner>}
      <div
        role="status"
        className="mb-4 flex flex-wrap items-center gap-3 rounded-control border border-accent bg-surface-raised px-3 py-2 text-sm text-ink"
        data-read-ask-banner="ask"
      >
        <Icon.Eye className="shrink-0" />
        <p className="min-w-0 flex-1">{t.readAsk.banner}</p>
        <Button size="sm" loading={confirm.isPending} onClick={() => confirm.mutate()} data-action="confirm-read">
          {t.readAsk.confirm}
        </Button>
      </div>
    </>
  );
}

/** How many confirmed reading a page that asks, as a way to who did and who has not; for its editors. */
export function ReadByButton({ page, onOpen }: { page: Page; onOpen: () => void }) {
  const asks = page.readAsk.since !== null && page.can.edit;
  const { data } = useReadConfirmations(page.id, asks);
  if (!asks || !data) return null;
  return (
    <button
      type="button"
      onClick={onOpen}
      className="inline-flex h-6 items-center gap-1 rounded-control px-1 text-ink-muted hover:text-ink hover:underline"
      data-action="read-by"
      data-read-by={data.confirmed.length}
    >
      <Icon.Check />
      {t.readAsk.readBy(data.confirmed.length)}
    </button>
  );
}

/** Who confirmed reading the page since it asked, with when and which version, and who has not yet. */
export function ReadersDialog({ page, onClose }: { page: Page; onClose: () => void }) {
  const { data, error, refetch, isLoading } = useReadConfirmations(page.id);
  return (
    <Dialog title={t.readAsk.title(page.title)} wide onClose={onClose} data-readers-dialog={page.id}>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {data && (
        <div className="space-y-4">
          <p className="text-sm text-ink-muted">{t.readAsk.since(data.since)}</p>
          <section className="space-y-2" aria-label={t.readAsk.confirmedTitle}>
            <SectionTitle>{t.readAsk.confirmedTitle}</SectionTitle>
            {data.confirmed.length === 0 ? (
              <p className="text-sm text-ink-subtle">{t.readAsk.noneConfirmed}</p>
            ) : (
              <ul className="divide-y divide-border">
                {data.confirmed.map((entry) => (
                  <li key={entry.person.id} className="flex flex-wrap justify-between gap-x-3 py-1.5 text-sm" data-read-confirmed={entry.person.name}>
                    <span className="font-medium text-ink">{entry.person.name || entry.person.email}</span>
                    <span className="text-ink-muted">{t.readAsk.confirmedRow(confirmedOn.format(new Date(entry.confirmedAt)), entry.version)}</span>
                  </li>
                ))}
              </ul>
            )}
          </section>
          <section className="space-y-2" aria-label={t.readAsk.pendingTitle}>
            <SectionTitle>{t.readAsk.pendingTitle}</SectionTitle>
            {data.pending.length === 0 ? (
              <p className="text-sm text-ink-subtle">{t.readAsk.allConfirmed}</p>
            ) : (
              <ul className="divide-y divide-border">
                {data.pending.map((person) => (
                  <li key={person.id} className="py-1.5 text-sm text-ink" data-read-pending={person.name}>
                    {person.name || person.email}
                  </li>
                ))}
              </ul>
            )}
            {data.more > 0 && (
              <p className="text-sm text-ink-muted" data-read-more={data.more}>
                {t.readAsk.more(data.more)}
              </p>
            )}
          </section>
        </div>
      )}
    </Dialog>
  );
}
