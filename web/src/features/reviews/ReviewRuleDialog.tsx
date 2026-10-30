import type { Page } from "@/api/pages";
import { Dialog, SectionTitle } from "@/components/ui";
import { t } from "@/i18n";
import { ReviewRuleForm } from "./ReviewRuleForm";

/** The rule a page is held to and where it comes from, and its own rule for its branch to set or remove; for the space's administrators. */
export function ReviewRuleDialog({ page, onClose }: { page: Page; onClose: () => void }) {
  const rule = page.review.rule;
  const own = rule?.page?.id === page.id ? rule : null;
  const above = rule?.page && rule.page.id !== page.id ? rule.page : null;
  return (
    <Dialog open title={t.review.ruleTitle(page.title)} onClose={onClose} size="lg" attrs={{ "data-review-rule-dialog": page.id }}>
      <div className="space-y-4">
        <p className="text-sm text-ink-muted">{t.review.ruleIntro}</p>
        <div className="space-y-1 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-review-held-by={held(page)}>
          <p>
            {!rule && t.review.notHeld}
            {rule && !rule.page && t.review.heldBySpace}
            {own && t.review.heldByOwn}
            {above && t.review.heldByAbove(above.title)}
          </p>
          {rule && <p className="text-ink-muted">{t.review.ruleSummary(t.review.names(rule.reviewers.map((each) => each.name)), rule.required)}</p>}
        </div>
        <section className="space-y-2" aria-label={t.review.ownRuleTitle}>
          <SectionTitle>{t.review.ownRuleTitle}</SectionTitle>
          <ReviewRuleForm
            target={{ pageId: page.id }}
            rule={own}
            confirmRemove={{ noun: t.kit.confirm.things.ruleOf(page.title), body: t.review.confirmRemovePage }}
          />
        </section>
      </div>
    </Dialog>
  );
}

function held(page: Page): string {
  const rule = page.review.rule;
  if (!rule) return "none";
  if (!rule.page) return "space";
  return rule.page.id === page.id ? "own" : "above";
}
