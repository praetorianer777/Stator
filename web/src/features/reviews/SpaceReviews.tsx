import { useReviewRules, useRemoveReviewRule, type ReviewRule } from "@/api/reviews";
import type { Space } from "@/api/spaces";
import { Button, Card, ErrorBanner, SectionTitle, Skeleton } from "@/components/ui";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { ReviewRuleForm } from "./ReviewRuleForm";
import { useConfirm } from "@/features/shell/ConfirmProvider";

/** The space's review rules for its administrators: the one for the whole space, and the branches that have their own. */
export function SpaceReviews({ space }: { space: Space }) {
  const { data: rules, error, refetch, isLoading } = useReviewRules(space.key);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (isLoading || !rules) return <Skeleton />;
  const whole = rules.find((rule) => rule.page === null) ?? null;
  const branches = rules.filter((rule) => rule.page !== null);
  return (
    <>
      <Card className="space-y-3 p-4" data-space-review-rule={whole ? "set" : "none"}>
        <SectionTitle>{t.review.spaceTitle}</SectionTitle>
        <p className="text-sm text-ink-muted">{t.review.spaceIntro}</p>
        {!whole && <p className="text-sm text-ink-subtle">{t.review.noSpaceRule}</p>}
        <ReviewRuleForm
          target={{ spaceKey: space.key }}
          rule={whole}
          confirmRemove={{ noun: t.kit.confirm.things.spaceRule, body: t.review.confirmRemoveSpace }}
        />
      </Card>
      <Card className="space-y-3 p-4" data-branch-review-rules="">
        <SectionTitle>{t.review.branchesTitle}</SectionTitle>
        <p className="text-sm text-ink-muted">{t.review.branchesIntro}</p>
        {branches.length === 0 ? (
          <p className="text-sm text-ink-subtle">{t.review.branchesEmpty}</p>
        ) : (
          <ul className="divide-y divide-border">
            {branches.map((rule) => (
              <BranchRule key={rule.id} rule={rule} space={space} />
            ))}
          </ul>
        )}
      </Card>
    </>
  );
}

function BranchRule({ rule, space }: { rule: ReviewRule; space: Space }) {
  const page = rule.page!;
  const remove = useRemoveReviewRule({ pageId: page.id });
  const confirm = useConfirm();
  return (
    <li className="flex flex-wrap items-center gap-3 py-2" data-branch-rule={page.id}>
      <div className="min-w-0 flex-1">
        <PageLink spaceKey={space.key} id={page.id} title={page.title} className="font-medium text-ink underline-offset-2 hover:underline">
          {page.title}
        </PageLink>
        <p className="text-sm text-ink-muted">{t.review.ruleSummary(t.review.names(rule.reviewers.map((each) => each.name)), rule.required)}</p>
        {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      </div>
      <Button
        size="sm"
        variant="secondary"
        aria-label={t.review.removeBranch(page.title)}
        loading={remove.isPending}
        onClick={async () => {
          const ask = { noun: t.kit.confirm.things.ruleOf(page.title), verb: t.kit.confirm.verbs.remove, body: t.review.confirmRemovePage };
          if (await confirm(ask)) remove.mutate();
        }}
        data-action="remove-branch-rule"
      >
        {t.review.remove}
      </Button>
    </li>
  );
}
