import { useContext, useId } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useCreateFromTemplate, useTemplateButton } from "@/api/templateButton";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DocPageContext } from "@/features/editor/BlockViews";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { buttonTarget, buttonTitle, type TemplateButtonSettings } from "./button";

/**
 * Makes a page from the template where the button says, then opens it to
 * edit; whoever may not add pages there sees it disabled, and why.
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
    note = inEditor ? b.inEditor : b.makes(found.template.name, where(found));
  }

  function click() {
    create.mutate(
      { ...target, title: buttonTitle(settings, found) },
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
        loading={create.isPending}
        onClick={click}
        aria-describedby={noteId}
        data-action="create-from-template"
      >
        {label}
      </Button>
      <p id={noteId} className="doc-template-button-note">
        {note}
      </p>
      {create.error && (
        <p className="doc-template-button-error" role="alert">
          {create.error.message}
        </p>
      )}
    </div>
  );
}

function where(found: { parent: { title: string; home: boolean }; spaceName: string }): string {
  return found.parent.home ? t.templateButton.topOf(found.spaceName) : t.templateButton.under(found.parent.title);
}
