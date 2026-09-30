import { useState, type FormEvent } from "react";
import type { PublishOptions } from "@/api/versions";
import { Button, Checkbox, Dialog, Field } from "@/components/ui";
import { VERSION_COMMENT_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/** Asks what changed and whether watchers hear of it, then publishes. */
export function PublishDialog({
  title,
  busy,
  onClose,
  onPublish,
}: {
  title: string;
  busy: boolean;
  onClose: () => void;
  onPublish: (options: PublishOptions) => void;
}) {
  const [comment, setComment] = useState("");
  const [notifyWatchers, setNotifyWatchers] = useState(true);

  function submit(event: FormEvent) {
    event.preventDefault();
    onPublish({ comment: comment.trim(), notifyWatchers });
  }

  return (
    <Dialog title={t.draft.publishTitle(title)} onClose={onClose} data-publish-dialog="">
      <form onSubmit={submit} className="space-y-3" noValidate>
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
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.draft.cancel}
          </Button>
          <Button type="submit" loading={busy} data-action="confirm-publish">
            {t.draft.confirmPublish}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
