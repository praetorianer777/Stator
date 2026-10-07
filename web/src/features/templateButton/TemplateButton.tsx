import { useContext, useId, useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useCreateFromTemplate, useTemplateButton, type TemplateButtonTarget } from "@/api/templateButton";
import { Button, Dialog, ErrorBanner } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DocPageContext } from "@/features/editor/BlockViews";
import { TemplateValues, initialValues, missingValues, wireValues, type Values } from "@/features/templates/TemplateValues";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { buttonTarget, buttonTitle, type TemplateButtonSettings } from "./button";

/**
 * Makes a page from the template where the button says, asking first for its
 * variables, then opens it to edit; whoever may not add pages there sees it disabled, and why.
 */
export function TemplateButton({ settings, inEditor = false }: { settings: TemplateButtonSettings; inEditor?: boolean }) {
  const b = t.templateButton;
  const page = useContext(DocPageContext);
  const target = buttonTarget(settings, page?.spaceKey ?? null);
  const query = useTemplateButton(target);
  const create = useCreateFromTemplate();
  const navigate = useNavigate();
  const noteId = useId();
  const found = query.data;
  const [asking, setAsking] = useState(false);
  const asks = (found?.template.variables.length ?? 0) > 0;
  const label = settings.label.trim() || (found ? b.defaultLabel(found.template.name) : b.untitled);

  let note: string;
  let state: string;
  if (!settings.template || !(target.spaceKey || target.parentId)) {
    state = "unset";
    note = b.unset;
  } else if (query.isPending) {
    state = "loading";
    note = b.loading;
  } else if (query.isError || !found) {
    state = "missing";
    note = b.missing;
  } else if (!found.canCreate) {
    state = "refused";
    note = b.refused(where(found));
  } else {
    state = inEditor ? "editing" : "ready";
    note = inEditor ? b.inEditor : (asks ? b.makesAsking : b.makes)(found.template.name, where(found));
  }

  function make(values?: Record<string, string>) {
    create.mutate(
      { ...target, title: buttonTitle(settings, found), values },
      {
        onSuccess: (made) =>
          void navigate({ to: "/s/$spaceKey/p/$pageId/$slug/edit", params: { spaceKey: made.spaceKey, pageId: made.id, slug: pageSlug(made.title) } }),
      },
    );
  }

  return (
    <div className="doc-template-button" data-template-button={settings.template} data-state={state}>
      <Button
        variant="primary"
        icon={<Icon.Plus />}
        disabled={state !== "ready"}
        loading={create.isPending && !asking}
        onClick={() => (asks ? setAsking(true) : make())}
        aria-describedby={noteId}
        data-action="create-from-template"
      >
        {label}
      </Button>
      <p id={noteId} className="doc-template-button-note">
        {note}
      </p>
      {create.error && !asking && (
        <p className="doc-template-button-error" role="alert">
          {create.error.message}
        </p>
      )}
      {asking && found && (
        <AskValues
          found={found}
          pending={create.isPending}
          error={create.error}
          onSubmit={make}
          onClose={() => {
            setAsking(false);
            create.reset();
          }}
        />
      )}
    </div>
  );
}

function AskValues({
  found,
  pending,
  error,
  onSubmit,
  onClose,
}: {
  found: TemplateButtonTarget;
  pending: boolean;
  error: Error | null;
  onSubmit: (values: Record<string, string>) => void;
  onClose: () => void;
}) {
  const variables = found.template.variables;
  const [values, setValues] = useState<Values>(() => initialValues(variables));
  const [missing, setMissing] = useState("");
  const fields = error instanceof ApiError ? error.fields : {};
  const banner = error && !Object.keys(fields).some((field) => field.startsWith("values.")) ? error.message : "";

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form may be this one's ancestor in React's tree.
    event.stopPropagation();
    const unfilled = missingValues(variables, values);
    if (unfilled.length > 0) {
      setMissing(t.templates.missing(unfilled));
      return;
    }
    setMissing("");
    onSubmit(wireValues(values));
  }

  return (
    <Dialog title={t.templateButton.fillIn(found.template.name)} onClose={onClose} data-template-button-values="">
      <form onSubmit={submit} className="space-y-3" noValidate>
        {banner && <ErrorBanner>{fields.template ?? banner}</ErrorBanner>}
        <TemplateValues variables={variables} values={values} onChange={setValues} parentId={found.parent.id} errors={fields} />
        {missing && (
          <p role="alert" className="text-sm text-danger" data-template-missing="">
            {missing}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.templateButton.cancel}
          </Button>
          <Button type="submit" loading={pending} data-action="confirm-from-template">
            {t.templateButton.create}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function where(found: { parent: { title: string; home: boolean }; spaceName: string }): string {
  return found.parent.home ? t.templateButton.topOf(found.spaceName) : t.templateButton.under(found.parent.title);
}
