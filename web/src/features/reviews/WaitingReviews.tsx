import { Link } from "@tanstack/react-router";
import { useWaitingReviews, type WaitingReview } from "@/api/reviews";
import { ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
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
    <div className="mx-auto max-w-3xl" data-waiting-reviews="">
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
    </div>
  );
}

/** The top bar's way to the reviews waiting on the caller, with their count; nothing while none wait. */
export function WaitingReviewsButton() {
  const { data } = useWaitingReviews();
  const count = data?.length ?? 0;
  if (count === 0) return null;
  return (
    <Link
      to="/reviews"
      aria-label={t.waitingReviews.button(count)}
      className="relative inline-flex size-8 items-center justify-center rounded-control text-ink-muted hover:bg-surface-raised hover:text-ink"
      data-action="waiting-reviews"
    >
      <Icon.Seal />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-accent px-1 text-center text-2xs leading-4 font-medium text-on-accent"
        data-waiting-badge={count}
      >
        {count > UNREAD_BADGE_MAX ? `${UNREAD_BADGE_MAX}+` : count}
      </span>
    </Link>
  );
}
