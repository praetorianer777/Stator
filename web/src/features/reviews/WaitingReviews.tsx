import { useWaitingReviews, type WaitingReview } from "@/api/reviews";
import { Page, PageHeader, ErrorBanner, Skeleton } from "@/components/ui";
import { UNREAD_BADGE_MAX } from "@/config";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const submittedOn = localDateFormat({ dateStyle: "medium" });

/** One review waiting on the caller: its page, who submitted it and when, and how many approvals it has. */
export function WaitingReviewRow({ waiting, className }: { waiting: WaitingReview; className?: string }) {
  const { review, page, spaceKey } = waiting;
  return (
    <li className={className} data-waiting-review={review.id}>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <PageLink spaceKey={spaceKey} id={page.id} title={page.title} className="truncate text-sm font-medium text-ink hover:text-accent hover:underline" />
        <span className="flex flex-wrap items-center gap-x-1.5 text-xs text-ink-subtle">
          <span>{t.waitingReviews.submitted(review.authorName, submittedOn.format(new Date(review.submittedAt)))}</span>
          <span aria-hidden="true">·</span>
          <span data-waiting-approvals={`${review.approvals}/${review.required}`}>{t.review.approvals(review.approvals, review.required)}</span>
        </span>
      </span>
    </li>
  );
}

/** Every review waiting on the caller, the oldest first. */
export function WaitingReviewsScreen() {
  const { data, error, refetch, isLoading } = useWaitingReviews();
  return (
    <Page width="narrow" data-waiting-reviews="">
      <PageHeader title={t.waitingReviews.title} meta={t.waitingReviews.intro} />
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {data &&
        (data.length === 0 ? (
          <p className="text-sm text-ink-muted" data-waiting-reviews-empty="">
            {t.waitingReviews.empty}
          </p>
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {data.map((waiting) => (
              <WaitingReviewRow key={waiting.review.id} waiting={waiting} className="flex min-w-0 py-3" />
            ))}
          </ul>
        ))}
    </Page>
  );
}

/** How many reviews wait on the caller, beside Reviews in the sidebar; nothing while none wait. */
export function WaitingReviewsCount() {
  const { data } = useWaitingReviews();
  const count = data?.length ?? 0;
  if (count === 0) return null;
  return (
    <span
      className="min-w-4 rounded-full bg-accent px-1 text-center text-2xs leading-4 font-medium text-on-accent"
      title={t.waitingReviews.button(count)}
      data-waiting-badge={count}
    >
      <span aria-hidden="true">{count > UNREAD_BADGE_MAX ? `${UNREAD_BADGE_MAX}+` : count}</span>
      <span className="sr-only">{t.waitingReviews.button(count)}</span>
    </span>
  );
}
