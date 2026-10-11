import { useId, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { subjectKey, subjectRef, type Subject } from "@/api/permissions";
import { useRemoveReviewRule, useSetReviewRule, type ReviewRule } from "@/api/reviews";
import { Button, ErrorBanner, Field } from "@/components/ui";
import { REVIEW_REQUIRED_MAX } from "@/config";
import { SubjectList } from "@/features/permissions/SubjectList";
import { SubjectPicker } from "@/features/permissions/SubjectPicker";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const changedOn = localDateFormat({ dateStyle: "medium" });

/** Whom a rule form sets the rule of: a whole space, or a page with every page below it. */
export type RuleTarget = { spaceKey: string } | { pageId: string };

/**
 * Sets or removes one rule: the people and groups who review, never everyone,
 * and how many approvals it takes. The API's refusals are shown as it words them.
 */
export function ReviewRuleForm({
  target,
  rule,
  confirmRemove,
  onDone,
}: {
  target: RuleTarget;
  /** The rule as it stands, null when there is none to change. */
  rule: ReviewRule | null;
  confirmRemove: string;
  onDone?: () => void;
}) {
  const id = useId();
  const save = useSetReviewRule(target);
  const remove = useRemoveReviewRule(target);
  const [reviewers, setReviewers] = useState<Subject[]>(rule?.reviewers ?? []);
  const [required, setRequired] = useState(String(rule?.required ?? 1));
  const [problems, setProblems] = useState<{ reviewers?: string; required?: string }>({});
  const [notice, setNotice] = useState("");
  const failed = save.error ?? remove.error;
  const fields = failed instanceof ApiError ? failed.fields : {};

  function reset() {
    setNotice("");
    save.reset();
    remove.reset();
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    reset();
    const count = Number(required);
    const found = {
      reviewers: reviewers.length === 0 ? t.review.reviewersNeeded : undefined,
      required: Number.isInteger(count) && count >= 1 && count <= REVIEW_REQUIRED_MAX ? undefined : t.review.requiredRange(REVIEW_REQUIRED_MAX),
    };
    setProblems(found);
    if (found.reviewers || found.required) return;
    save.mutate(
      { reviewers: reviewers.map(subjectRef), required: count },
      {
        onSuccess: (saved) => {
          setReviewers(saved.reviewers);
          setRequired(String(saved.required));
          setNotice(t.review.saved);
          onDone?.();
        },
      },
    );
  }

  function removeRule() {
    if (!window.confirm(confirmRemove)) return;
    reset();
    remove.mutate(undefined, {
      onSuccess: () => {
        setReviewers([]);
        setRequired("1");
        setNotice(t.review.removed);
        onDone?.();
      },
    });
  }

  const reviewersError = problems.reviewers ?? fields.reviewers;
  return (
    <form onSubmit={submit} className="space-y-3" noValidate data-review-rule-form="">
      {failed && !fields.reviewers && !fields.required && <ErrorBanner>{failed.message}</ErrorBanner>}
      <fieldset className="space-y-2" aria-describedby={reviewersError ? `${id}-reviewers-error` : undefined}>
        <legend className="mb-1 text-sm font-medium text-ink">{t.review.reviewersTitle}</legend>
        <SubjectList
          subjects={reviewers}
          empty={t.review.reviewersEmpty}
          onRemove={(gone) => {
            reset();
            setReviewers(reviewers.filter((each) => subjectKey(each) !== subjectKey(gone)));
          }}
        />
        <SubjectPicker
          label={t.review.pickerLabel}
          inline
          exclude={reviewers.map(subjectKey)}
          onPick={(picked) => {
            reset();
            setProblems((before) => ({ ...before, reviewers: undefined }));
            setReviewers([...reviewers, picked]);
          }}
        />
        {reviewersError && (
          <p id={`${id}-reviewers-error`} className="text-sm text-danger" data-reviewers-error="">
            {reviewersError}
          </p>
        )}
      </fieldset>
      <Field
        type="number"
        label={t.review.required}
        hint={t.review.requiredHint(REVIEW_REQUIRED_MAX)}
        min={1}
        max={REVIEW_REQUIRED_MAX}
        value={required}
        error={problems.required ?? fields.required}
        onChange={(event) => {
          reset();
          setRequired(event.target.value);
        }}
        className="w-24"
        data-review-required=""
      />
      {rule && <p className="text-xs text-ink-subtle">{t.review.updated(rule.updatedByName, changedOn.format(new Date(rule.updatedAt)))}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" loading={save.isPending} data-action="save-review-rule">
          {t.review.save}
        </Button>
        {rule && (
          <Button type="button" variant="secondary" loading={remove.isPending} onClick={removeRule} data-action="remove-review-rule">
            {t.review.remove}
          </Button>
        )}
        <span role="status" className="text-sm text-ink-muted">
          {notice}
        </span>
      </div>
    </form>
  );
}
