import { useId, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { Page } from "@/api/pages";
import { useSetPageMode, type PageMode } from "@/api/versions";
import { Button, Dialog, ErrorBanner } from "@/components/ui";
import { LIVE_VERSION_SPAN_MINUTES } from "@/config";
import { t } from "@/i18n";

/**
 * Chooses between drafts and live. Making a page live throws away drafts
 * nobody published, so the server names them first and the person confirms.
 */
export function ModeDialog({ page, onClose }: { page: Page; onClose: () => void }) {
  const id = useId();
  const [mode, setMode] = useState<PageMode>(page.mode);
  const [pending, setPending] = useState("");
  const change = useSetPageMode(page.id);

  function send(discardDrafts: boolean) {
    change.mutate(
      { mode, discardDrafts },
      {
        onSuccess: onClose,
        onError: (error) => {
          if (error instanceof ApiError && error.code === "drafts_pending") setPending(error.message);
        },
      },
    );
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (mode === page.mode) onClose();
    else send(false);
  }

  const options: { value: PageMode; label: string; hint: string }[] = [
    { value: "draft", label: t.live.draftLabel, hint: t.live.draftHint },
    { value: "live", label: t.live.liveLabel, hint: t.live.liveHint(LIVE_VERSION_SPAN_MINUTES) },
  ];
  const failed = change.error && !(change.error instanceof ApiError && change.error.code === "drafts_pending") ? change.error : null;

  return (
    <Dialog title={t.live.dialog} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4" data-mode-dialog="">
        <fieldset className="space-y-3">
          <legend className="sr-only">{t.live.dialog}</legend>
          {options.map((option) => (
            <label key={option.value} className="flex items-start gap-2 text-sm text-ink">
              <input
                type="radio"
                name={`${id}-mode`}
                checked={mode === option.value}
                onChange={() => {
                  setMode(option.value);
                  setPending("");
                }}
                className="mt-1 accent-accent"
                data-mode={option.value}
              />
              <span>
                <span className="block font-medium">{option.label}</span>
                <span className="block text-ink-muted">{option.hint}</span>
              </span>
            </label>
          ))}
        </fieldset>
        {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
        {pending && (
          <div role="alert" className="space-y-2 rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink" data-drafts-pending="">
            <p>{pending}</p>
            <Button type="button" variant="danger" size="sm" onClick={() => send(true)} loading={change.isPending} data-action="discard-drafts-go-live">
              {t.live.discardAnyway}
            </Button>
          </div>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.live.cancel}
          </Button>
          <Button type="submit" loading={change.isPending && !pending} data-action="save-mode">
            {t.live.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
