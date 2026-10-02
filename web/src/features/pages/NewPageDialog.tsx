import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { Page } from "@/api/pages";
import { templateTitle, type Template } from "@/api/templates";
import { useCreatePage } from "@/api/tree";
import { Button, Dialog, ErrorBanner, Field } from "@/components/ui";
import { BLANK_TEMPLATE, PAGE_TITLE_MAX_LENGTH } from "@/config";
import { TemplateValues, initialValues, missingValues, wireValues, type Values } from "@/features/templates/TemplateValues";
import { t } from "@/i18n";
import { TemplatePicker } from "./TemplatePicker";

/**
 * Names a new page under a parent and picks what it starts from; the page is
 * made unpublished and opens in the editor.
 */
export function NewPageDialog({
  parent,
  onClose,
  onDone,
}: {
  parent: { id: string; title: string; spaceKey?: string };
  onClose: () => void;
  onDone: (page: Page) => void;
}) {
  const create = useCreatePage();
  const [title, setTitle] = useState("");
  // A title the author typed is theirs; one a template filled in follows the choice.
  const [titleTyped, setTitleTyped] = useState(false);
  const [key, setKey] = useState(BLANK_TEMPLATE);
  const [template, setTemplate] = useState<Template>();
  const [values, setValues] = useState<Values>({});
  const [error, setError] = useState("");
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const variables = template?.variables ?? [];

  function choose(next: string, chosen: Template | undefined) {
    setKey(next);
    setTemplate(chosen);
    setValues(initialValues(chosen?.variables ?? []));
    setError("");
    create.reset();
    if (!titleTyped) setTitle(chosen?.title ? templateTitle(chosen.title) : "");
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!template && !title.trim()) {
      setError(t.page.emptyTitle);
      return;
    }
    const missing = missingValues(variables, values);
    if (missing.length > 0) {
      setError(t.templates.missing(missing));
      return;
    }
    setError("");
    create.mutate(
      {
        parentId: parent.id,
        title,
        ...(template ? { template: template.key } : {}),
        ...(variables.length > 0 ? { values: wireValues(values) } : {}),
      },
      { onSuccess: onDone },
    );
  }

  const banner = create.error && !Object.keys(fields).some((field) => field.startsWith("values.") || field === "title") ? create.error.message : "";
  return (
    <Dialog title={t.page.newPageUnder(parent.title)} wide onClose={onClose} data-new-page-dialog="">
      <form onSubmit={submit} className="space-y-3" noValidate>
        {banner && <ErrorBanner>{fields.template ?? banner}</ErrorBanner>}
        <Field
          label={t.page.title}
          value={title}
          maxLength={PAGE_TITLE_MAX_LENGTH}
          onChange={(event) => {
            setTitle(event.target.value);
            setTitleTyped(event.target.value !== "");
          }}
          error={(template ? "" : error) || fields.title}
          hint={variables.length > 0 ? t.templates.titleHint : undefined}
          autoFocus
        />
        <TemplatePicker value={key} onChange={choose} spaceKey={parent.spaceKey} />
        <TemplateValues variables={variables} values={values} onChange={setValues} parentId={parent.id} errors={fields} />
        {template && error && (
          <p role="alert" className="text-sm text-danger" data-template-missing="">
            {error}
          </p>
        )}
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
