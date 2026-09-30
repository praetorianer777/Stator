import { useState, type FormEvent } from "react";
import type { Page } from "@/api/pages";
import { templateTitle, type Template } from "@/api/templates";
import { useCreatePage } from "@/api/tree";
import { Button, Dialog, ErrorBanner, Field } from "@/components/ui";
import { BLANK_TEMPLATE, PAGE_TITLE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { TemplatePicker } from "./TemplatePicker";

/**
 * Names a new page under a parent and picks what it starts from; the page is
 * made unpublished and opens in the editor.
 */
export function NewPageDialog({ parent, onClose, onDone }: { parent: { id: string; title: string }; onClose: () => void; onDone: (page: Page) => void }) {
  const create = useCreatePage();
  const [title, setTitle] = useState("");
  // A title the author typed is theirs; one a template filled in follows the choice.
  const [titleTyped, setTitleTyped] = useState(false);
  const [key, setKey] = useState(BLANK_TEMPLATE);
  const [template, setTemplate] = useState<Template>();
  const [error, setError] = useState("");

  function choose(next: string, chosen: Template | undefined) {
    setKey(next);
    setTemplate(chosen);
    if (!titleTyped) setTitle(chosen?.title ? templateTitle(chosen.title) : "");
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!title.trim()) {
      setError(t.page.emptyTitle);
      return;
    }
    setError("");
    create.mutate({ parentId: parent.id, title, ...(template ? { body: template.body } : {}) }, { onSuccess: onDone });
  }

  return (
    <Dialog title={t.page.newPageUnder(parent.title)} wide onClose={onClose} data-new-page-dialog="">
      <form onSubmit={submit} className="space-y-3" noValidate>
        {create.error && <ErrorBanner>{create.error.message}</ErrorBanner>}
        <Field
          label={t.page.title}
          value={title}
          maxLength={PAGE_TITLE_MAX_LENGTH}
          onChange={(event) => {
            setTitle(event.target.value);
            setTitleTyped(event.target.value !== "");
          }}
          error={error}
          autoFocus
        />
        <TemplatePicker value={key} onChange={choose} />
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
