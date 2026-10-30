import { useId, useState, type FormEvent } from "react";
import type { PageSchedule, PublishOptions, ScheduleOptions } from "@/api/versions";
import { Button, Checkbox, Dialog, Field } from "@/components/ui";
import { SCHEDULE_MAX_AHEAD_DAYS, VERSION_COMMENT_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { defaultScheduleTime, latestScheduleTime, localInputValue, localZone, parseLocalInput } from "./schedule";

type When = "now" | "later";

/**
 * Asks what changed, whether watchers hear of it and when it goes out, then
 * publishes now or schedules the draft. A schedule of the caller's opens it as set.
 * A page held to a review submits the draft to its reviewers instead of publishing it.
 */
export function PublishDialog({
  title,
  busy,
  schedule,
  review,
  onClose,
  onPublish,
  onSchedule,
}: {
  title: string;
  busy: boolean;
  /** The caller's own schedule of the page, to change. */
  schedule?: PageSchedule | null;
  /** The approvals the page's rule asks for, when publishing it needs a review. */
  review?: { required: number };
  onClose: () => void;
  onPublish: (options: PublishOptions) => void;
  onSchedule: (options: ScheduleOptions) => void;
}) {
  const id = useId();
  const [when, setWhen] = useState<When>(schedule ? "later" : "now");
  const [at, setAt] = useState(() => localInputValue(schedule ? new Date(schedule.publishAt) : defaultScheduleTime(new Date())));
  const [atError, setAtError] = useState("");
  const [comment, setComment] = useState(schedule?.comment ?? "");
  const [notifyWatchers, setNotifyWatchers] = useState(schedule?.notifyWatchers ?? true);

  function submit(event: FormEvent) {
    event.preventDefault();
    const options = { comment: comment.trim(), notifyWatchers };
    if (when === "now") {
      onPublish(options);
      return;
    }
    const now = new Date();
    const chosen = parseLocalInput(at);
    if (!chosen || chosen <= now) {
      setAtError(t.schedule.timeNeeded);
      return;
    }
    if (chosen > latestScheduleTime(now)) {
      setAtError(t.schedule.timeTooFar(SCHEDULE_MAX_AHEAD_DAYS));
      return;
    }
    setAtError("");
    onSchedule({ ...options, publishAt: chosen.toISOString() });
  }

  const choices: { value: When; label: string }[] = [
    { value: "now", label: t.schedule.now },
    { value: "later", label: t.schedule.later },
  ];

  return (
    <Dialog
      open
      title={review ? t.review.submitTitle(title) : t.draft.publishTitle(title)}
      onClose={onClose}
      attrs={{ "data-publish-dialog": review ? "review" : "" }}
      footer={
        <>
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.draft.cancel}
          </Button>
          {when === "now" ? (
            <Button type="submit" form="publish-dialog-form" loading={busy} data-action={review ? "confirm-submit-review" : "confirm-publish"}>
              {review ? t.review.confirmSubmit : t.draft.confirmPublish}
            </Button>
          ) : (
            <Button type="submit" form="publish-dialog-form" loading={busy} data-action="confirm-schedule">
              {t.schedule.confirm}
            </Button>
          )}
        </>
      }
    >
      <form id="publish-dialog-form" onSubmit={submit} className="space-y-3" noValidate>
        {review && (
          <p className="text-sm text-ink-muted" data-review-submit-note="">
            {when === "now" ? t.review.submitIntro(review.required) : t.review.submitLater}
          </p>
        )}
        <fieldset className="flex flex-wrap gap-x-4 gap-y-2">
          <legend className="mb-1 text-sm font-medium text-ink">{t.schedule.when}</legend>
          {choices.map((choice) => (
            <label key={choice.value} className="flex items-center gap-2 text-sm text-ink">
              <input
                type="radio"
                name={`${id}-when`}
                checked={when === choice.value}
                onChange={() => {
                  setWhen(choice.value);
                  setAtError("");
                }}
                className="accent-accent"
                data-publish-when={choice.value}
              />
              {choice.label}
            </label>
          ))}
        </fieldset>
        {when === "later" && (
          <Field
            type="datetime-local"
            label={t.schedule.at}
            hint={t.schedule.atHint(localZone())}
            value={at}
            min={localInputValue(new Date())}
            max={localInputValue(latestScheduleTime(new Date()))}
            error={atError}
            required
            onChange={(event) => {
              setAt(event.target.value);
              setAtError("");
            }}
            data-schedule-at=""
          />
        )}
        <Field
          label={t.draft.comment}
          hint={t.draft.commentHint}
          value={comment}
          maxLength={VERSION_COMMENT_MAX_LENGTH}
          onChange={(event) => setComment(event.target.value)}
          rows={3}
          autoFocus
        />
        <Checkbox label={t.draft.notifyWatchers} checked={notifyWatchers} onChange={(event) => setNotifyWatchers(event.target.checked)} />
      </form>
    </Dialog>
  );
}
