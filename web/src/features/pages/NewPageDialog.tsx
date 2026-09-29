import { useState, type FormEvent } from "react";
import type { Page } from "@/api/pages";
import { useCreatePage } from "@/api/tree";
import { Button, Dialog, ErrorBanner, Field } from "@/components/ui";
import { PAGE_TITLE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/** Names a new page under a parent; the page then opens in the editor. */
export function NewPageDialog({ parent, onClose, onDone }: { parent: { id: string; title: string }; onClose: () => void; onDone: (page: Page) => void }) {
  const create = useCreatePage();
  const [title, setTitle] = useState("");
  const [error, setError] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!title.trim()) {
      setError(t.page.emptyTitle);
      return;
    }
    setError("");
    create.mutate({ parentId: parent.id, title }, { onSuccess: onDone });
  }

  return (
    <Dialog title={t.page.newPageUnder(parent.title)} onClose={onClose} data-new-page-dialog="">
      <form onSubmit={submit} className="space-y-3" noValidate>
        {create.error && <ErrorBanner>{create.error.message}</ErrorBanner>}
        <Field
          label={t.page.title}
          value={title}
          maxLength={PAGE_TITLE_MAX_LENGTH}
          onChange={(event) => setTitle(event.target.value)}
          error={error}
          autoFocus
        />
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.page.cancel}
          </Button>
          <Button type="submit" loading={create.isPending} data-action="confirm-new-page">
            {t.page.create}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
