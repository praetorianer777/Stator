import { useState, type FormEvent } from "react";
import { useSpaces } from "@/api/spaces";
import { useTemplates } from "@/api/templates";
import { useOutline } from "@/api/tree";
import { Button, Dialog, ErrorBanner, Field, Select } from "@/components/ui";
import { TEMPLATE_BUTTON_LABEL_MAX_LENGTH, TEMPLATE_BUTTON_TITLE_MAX_LENGTH, TEMPLATE_DATE_TOKEN } from "@/config";
import { t } from "@/i18n";
import type { TemplateButtonSettings } from "./button";

// Indents a page of the outline under its parent in a plain select.
const INDENT = "  ";

/** Asks what a template button makes: a template, where the page goes, the button's words and the page's title. */
export function TemplateButtonDialog({
  initial,
  pageSpace,
  isNew,
  onSave,
  onClose,
}: {
  initial: TemplateButtonSettings;
  /** The space of the page holding the button, where its page goes unless it names another. */
  pageSpace: string | null;
  isNew: boolean;
  onSave: (settings: TemplateButtonSettings) => void;
  onClose: () => void;
}) {
  const d = t.templateButton.dialog;
  const templates = useTemplates();
  const spaces = useSpaces();
  const [template, setTemplate] = useState(initial.template);
  const [space, setSpace] = useState(initial.space ?? "");
  const [parent, setParent] = useState(initial.parent ?? "");
  const [label, setLabel] = useState(initial.label);
  const [title, setTitle] = useState(initial.title);
  const [error, setError] = useState("");
  const outline = useOutline(space || pageSpace || undefined);
  // The page picked before may have moved out of the space, or out of the reader's sight.
  const keptParent = initial.parent && parent === initial.parent && !outline.data?.some((p) => p.id === initial.parent);

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    if (!template) {
      setError(d.pickTemplate);
      return;
    }
    onSave({ template, space: space || null, parent: parent || null, label: label.trim(), title: title.trim() });
  }

  return (
    <Dialog title={isNew ? d.titleNew : d.titleEdit} onClose={onClose} data-template-button-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        {templates.isError && <ErrorBanner>{t.page.templatesFailed}</ErrorBanner>}
        <Select label={d.template} value={template} onChange={(event) => setTemplate(event.target.value)} error={error}>
          <option value="">{d.chooseTemplate}</option>
          {initial.template && !templates.data?.some((tpl) => tpl.key === initial.template) && <option value={initial.template}>{initial.template}</option>}
          {(templates.data ?? []).map((tpl) => (
            <option key={tpl.key} value={tpl.key}>
              {tpl.name}
            </option>
          ))}
        </Select>
        <Select
          label={d.space}
          value={space}
          onChange={(event) => {
            setSpace(event.target.value);
            setParent("");
          }}
        >
          <option value="">{d.thisSpace}</option>
          {initial.space && !spaces.data?.some((s) => s.key === initial.space) && <option value={initial.space}>{initial.space}</option>}
          {(spaces.data ?? []).map((s) => (
            <option key={s.key} value={s.key}>
              {s.name} ({s.key})
            </option>
          ))}
        </Select>
        <Select label={d.parent} value={parent} onChange={(event) => setParent(event.target.value)}>
          <option value="">{d.top}</option>
          {keptParent && <option value={initial.parent ?? ""}>{d.keptParent}</option>}
          {(outline.data ?? [])
            .filter((p) => p.depth > 0)
            .map((p) => (
              <option key={p.id} value={p.id}>
                {INDENT.repeat(p.depth - 1)}
                {p.title}
              </option>
            ))}
        </Select>
        <Field
          label={d.label}
          value={label}
          maxLength={TEMPLATE_BUTTON_LABEL_MAX_LENGTH}
          hint={d.labelHint}
          onChange={(event) => setLabel(event.target.value)}
        />
        <Field
          label={d.title}
          value={title}
          maxLength={TEMPLATE_BUTTON_TITLE_MAX_LENGTH}
          hint={d.titleHint(TEMPLATE_DATE_TOKEN)}
          onChange={(event) => setTitle(event.target.value)}
        />
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {d.cancel}
          </Button>
          <Button type="submit" data-action="save-template-button">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
