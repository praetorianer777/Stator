import { useState, type FormEvent } from "react";
import type { Page } from "@/api/pages";
import { reviewSide, useDecideReview, useSkipReview, useWithdrawReview, type DecisionOptions, type ReviewSummary } from "@/api/reviews";
import type { Space } from "@/api/spaces";
import { useComparison } from "@/api/versions";
import { Button, Dialog, Drawer, ErrorBanner, Field, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DOCUMENT_DRAWER_WIDTH, REVIEW_COMMENT_MAX_LENGTH } from "@/config";
import { ComparisonView } from "@/features/pages/PageHistory";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { useConfirm } from "@/features/shell/ConfirmProvider";

const submittedOn = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** The review a page waits on: who submitted it, how far it is, who said what, and what the caller may do with it. */
export function ReviewBanner({ page, space }: { page: Page; space: Space }) {
  const review = page.review.pending;
  const [dialog, setDialog] = useState<"review" | "skip">();
  const withdraw = useWithdrawReview(page.id, space.key);
  const confirm = useConfirm();
  if (!review) return null;
  const { can } = review;
  return (
    <>
      {withdraw.error && <ErrorBanner>{withdraw.error.message}</ErrorBanner>}
      <section
        aria-label={t.review.bannerTitle}
        className="mb-4 space-y-2 rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink"
        data-review-banner={review.id}
      >
        <div className="flex flex-wrap items-start gap-3">
          <Icon.Seal className="mt-0.5 shrink-0" />
          <div className="min-w-0 flex-1 space-y-1">
            <p className="font-medium">{t.review.bannerTitle}</p>
            <p>
              {t.review.submittedBy(review.authorName, submittedOn.format(new Date(review.submittedAt)))}{" "}
              <span className="font-medium" data-review-approvals={`${review.approvals}/${review.required}`}>
                {t.review.approvals(review.approvals, review.required)}
              </span>
            </p>
            <p className="text-ink-muted" data-review-reviewers="">
              {t.review.reviewers(t.review.names(review.reviewers.map((each) => each.name)))}
            </p>
            {review.comment && <p className="text-ink-muted">{t.review.authorComment(review.comment)}</p>}
          </div>
          <div className="flex flex-wrap gap-2">
            {can.decide && (
              <Button size="sm" onClick={() => setDialog("review")} data-action="open-review">
                {t.review.open}
              </Button>
            )}
            {can.withdraw && (
              <Button
                size="sm"
                variant="secondary"
                loading={withdraw.isPending}
                onClick={async () => {
                  const ask = { noun: t.kit.confirm.things.review, verb: t.kit.confirm.verbs.withdraw, body: t.review.confirmWithdraw };
                  if (await confirm(ask)) withdraw.mutate(review.id);
                }}
                data-action="withdraw-review"
              >
                {t.review.withdraw}
              </Button>
            )}
            {can.skip && (
              <Button size="sm" variant="secondary" onClick={() => setDialog("skip")} data-action="skip-review">
                {t.review.skip}
              </Button>
            )}
          </div>
        </div>
        {review.decisions.length > 0 && <Decisions review={review} />}
      </section>
      {dialog === "review" && <ReviewDialog page={page} space={space} review={review} onClose={() => setDialog(undefined)} />}
      {dialog === "skip" && <SkipDialog page={page} space={space} review={review} onClose={() => setDialog(undefined)} />}
    </>
  );
}

function Decisions({ review }: { review: ReviewSummary }) {
  return (
    <ul aria-label={t.review.decisionsLabel} className="space-y-1 border-t border-warning/40 pt-2" data-review-decisions="">
      {review.decisions.map((decision, i) => (
        <li key={decision.reviewerId ?? i} className="flex gap-2" data-review-decision={decision.decision}>
          {decision.decision === "approve" ? <Icon.Check className="mt-0.5 shrink-0 text-success" /> : <Icon.Edit className="mt-0.5 shrink-0" />}
          <span className="min-w-0">
            <span className="font-medium">
              {decision.decision === "approve" ? t.review.approved(decision.reviewerName) : t.review.changesRequested(decision.reviewerName)}
            </span>
            {decision.comment && <span className="block text-ink-muted">{decision.comment}</span>}
          </span>
        </li>
      ))}
    </ul>
  );
}

/** The draft compared with the published page, and the reviewer's word on it. */
function ReviewDialog({ page, space, review, onClose }: { page: Page; space: Space; review: ReviewSummary; onClose: () => void }) {
  const comparison = useComparison(page.id, undefined, reviewSide(review.id));
  const decide = useDecideReview(page.id, space.key);
  const [comment, setComment] = useState("");
  const [commentError, setCommentError] = useState("");

  function send(decision: DecisionOptions["decision"]) {
    const said = comment.trim();
    if (decision === "changes" && !said) {
      setCommentError(t.review.commentNeeded);
      return;
    }
    setCommentError("");
    decide.mutate({ reviewId: review.id, decision, ...(said ? { comment: said } : {}) }, { onSuccess: onClose });
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    send("approve");
  }

  return (
    <Drawer open title={t.review.reviewTitle(page.title)} onClose={onClose} width={DOCUMENT_DRAWER_WIDTH} attrs={{ "data-review-dialog": review.id }}>
      <div className="space-y-4">
        <p className="text-sm text-ink-muted">{t.review.reviewIntro}</p>
        {comparison.error && <ErrorBanner onRetry={() => void comparison.refetch()}>{comparison.error.message}</ErrorBanner>}
        {comparison.isLoading && <Skeleton />}
        {comparison.data && (
          <div className="space-y-3" data-review-comparison="">
            <ComparisonView comparison={comparison.data} toLabel={t.review.draftSide} />
          </div>
        )}
        <form onSubmit={submit} className="space-y-3 border-t border-border pt-3" noValidate>
          {decide.error && <ErrorBanner>{decide.error.message}</ErrorBanner>}
          <Field
            label={t.review.decisionComment}
            hint={t.review.decisionCommentHint}
            value={comment}
            maxLength={REVIEW_COMMENT_MAX_LENGTH}
            rows={3}
            error={commentError}
            onChange={(event) => {
              setComment(event.target.value);
              setCommentError("");
            }}
          />
          <div className="flex flex-wrap justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => send("changes")} disabled={decide.isPending} data-action="request-changes">
              {t.review.requestChanges}
            </Button>
            <Button type="submit" loading={decide.isPending} data-action="approve-review">
              {t.review.approve}
            </Button>
          </div>
        </form>
      </div>
    </Drawer>
  );
}

/** Publishes a pending review without its approvals, for a reason the history and the audit log keep. */
function SkipDialog({ page, space, review, onClose }: { page: Page; space: Space; review: ReviewSummary; onClose: () => void }) {
  const skip = useSkipReview(page.id, space.key);
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    const said = reason.trim();
    if (!said) {
      setReasonError(t.review.reasonNeeded);
      return;
    }
    setReasonError("");
    skip.mutate({ reviewId: review.id, reason: said }, { onSuccess: onClose });
  }

  return (
    <Dialog
      open
      title={t.review.skipTitle(page.title)}
      onClose={onClose}
      attrs={{ "data-skip-review-dialog": review.id }}
      footer={
        <>
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.draft.cancel}
          </Button>
          <Button type="submit" form="review-banner-form" variant="danger" loading={skip.isPending} data-action="confirm-skip-review">
            {t.review.confirmSkip}
          </Button>
        </>
      }
    >
      <form id="review-banner-form" onSubmit={submit} className="space-y-3" noValidate>
        <p className="text-sm text-ink-muted">{t.review.skipIntro}</p>
        {skip.error && <ErrorBanner>{skip.error.message}</ErrorBanner>}
        <Field
          label={t.review.reason}
          value={reason}
          maxLength={REVIEW_COMMENT_MAX_LENGTH}
          rows={3}
          required
          autoFocus
          error={reasonError}
          onChange={(event) => {
            setReason(event.target.value);
            setReasonError("");
          }}
        />
      </form>
    </Dialog>
  );
}
