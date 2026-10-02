import { useId, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import {
  useCreateTemplate,
  useTemplate,
  useUpdateTemplate,
  type Template,
  type TemplateInput,
  type TemplateVariable,
  type VariableKind,
} from "@/api/templates";
import { Button, Checkbox, ErrorBanner, Field, IconButton, PageHeader, Select, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import {
  TEMPLATE_DEFAULT_TODAY,
  TEMPLATE_DESCRIPTION_MAX_LENGTH,
  TEMPLATE_NAME_MAX_LENGTH,
  TEMPLATE_OPTIONS_MAX,
  TEMPLATE_TEXT_VALUE_MAX_LENGTH,
  TEMPLATE_TITLE_MAX_LENGTH,
  TEMPLATE_VARIABLE_KINDS,
  TEMPLATE_VARIABLE_LABEL_MAX_LENGTH,
  TEMPLATE_VARIABLE_NAME_MAX_LENGTH,
  TEMPLATE_VARIABLES_MAX,
  TEMPLATES_PATH,
} from "@/config";
import { Editor, type EditorHandle } from "@/features/editor/Editor";
import { blankText } from "@/features/editor/blanks";
import { emptyDoc, type Doc } from "@/features/editor/schema";
import { t } from "@/i18n";

/** One variable as the form holds it; key keeps a row's identity while its name changes. */
interface Row {
  key: number;
  name: string;
  /** Once the name is typed it no longer follows the label. */
  nameTyped: boolean;
  label: string;
  kind: VariableKind;
  /** A choice's options, one per line. */
  options: string;
  default: string;
  required: boolean;
}

/** A variable's name made from its label: lower case letters, digits and underscores, starting with a letter. */
export function nameFrom(label: string): string {
  const base = label
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  const named = /^[a-z]/.test(base) ? base : `v_${base}`.replace(/_+$/, "");
  return named.slice(0, TEMPLATE_VARIABLE_NAME_MAX_LENGTH).replace(/_+$/, "");
}

function optionsOf(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

let nextKey = 0;

function rowOf(v: TemplateVariable): Row {
  nextKey += 1;
  return { key: nextKey, name: v.name, nameTyped: true, label: v.label, kind: v.kind, options: v.options.join("\n"), default: v.default, required: v.required };
}

function emptyRow(): Row {
  nextKey += 1;
  return { key: nextKey, name: "", nameTyped: false, label: "", kind: "text", options: "", default: "", required: false };
}

/** The variables as the API takes them. */
export function variablesOf(rows: Row[]): TemplateVariable[] {
  return rows.map((row) => ({
    name: row.name,
    label: row.label.trim(),
    kind: row.kind,
    options: row.kind === "select" ? optionsOf(row.options) : [],
    default: row.kind === "person" ? "" : row.default,
    required: row.required,
  }));
}

/** Makes one of the organization's templates, or changes one, for whoever keeps it. */
export function TemplateEditor({ templateKey, spaceKey }: { templateKey?: string; spaceKey?: string }) {
  const found = useTemplate(templateKey);
  if (templateKey) {
    if (found.error) return <ErrorBanner onRetry={() => void found.refetch()}>{found.error.message}</ErrorBanner>;
    if (!found.data) return <Skeleton />;
    if (!found.data.canEdit) return <ErrorBanner>{found.data.builtIn ? t.templates.builtInFixed : t.templates.cannotEdit}</ErrorBanner>;
    return <TemplateForm key={found.data.key} template={found.data} spaceKey={found.data.spaceKey || undefined} />;
  }
  return <TemplateForm spaceKey={spaceKey} />;
}

const FORM_ID = "template-form";

function TemplateForm({ template, spaceKey }: { template?: Template; spaceKey?: string }) {
  const navigate = useNavigate();
  const create = useCreateTemplate();
  const update = useUpdateTemplate(template?.key ?? "");
  const save = template ? update : create;
  const [name, setName] = useState(template?.name ?? "");
  const [description, setDescription] = useState(template?.description ?? "");
  const [title, setTitle] = useState(template?.title ?? "");
  const [initialBody] = useState<Doc>(template?.body ?? emptyDoc);
  const [body, setBody] = useState<Doc | null>(initialBody);
  const [rows, setRows] = useState<Row[]>(() => (template?.variables ?? []).map(rowOf));
  const editor = useRef<EditorHandle | null>(null);
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const back = spaceKey ? { to: "/s/$spaceKey/settings" as const, params: { spaceKey }, search: { tab: "templates" as const } } : { to: TEMPLATES_PATH };

  function change(key: number, next: Partial<Row>) {
    setRows((current) =>
      current.map((row) => {
        if (row.key !== key) return row;
        const merged = { ...row, ...next };
        if (next.label !== undefined && !row.nameTyped) merged.name = nameFrom(next.label);
        return merged;
      }),
    );
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const input: TemplateInput = { name, description, title, body: body ?? emptyDoc, variables: variablesOf(rows) };
    const done = () => void navigate(back);
    if (template) update.mutate(input, { onSuccess: done });
    else create.mutate({ ...input, ...(spaceKey ? { spaceKey } : {}) }, { onSuccess: done });
  }

  const banner = save.error && !["name", "description", "title"].some((field) => fields[field]) ? (fields.variables ?? fields.body ?? save.error.message) : "";
  return (
    <div className="mx-auto max-w-4xl space-y-4" data-template-editor={template?.key ?? "new"}>
      <PageHeader
        crumbs={[
          {
            label: spaceKey ? t.templates.spaceCrumb(spaceKey) : t.templates.title,
            render: (label) => <Link {...back}>{label}</Link>,
          },
        ]}
        title={template ? t.templates.editTitle(template.name) : spaceKey ? t.templates.newSpaceTitle(spaceKey) : t.templates.newOrgTitle}
        actions={
          <>
            <Link
              {...back}
              className="inline-flex h-8 items-center rounded-control px-3 text-sm font-medium text-ink-muted no-underline hover:bg-surface-raised"
            >
              {t.templates.cancel}
            </Link>
            <Button type="submit" form={FORM_ID} variant="primary" loading={save.isPending} data-action="save-template">
              {t.templates.save}
            </Button>
          </>
        }
      />
      <form id={FORM_ID} onSubmit={submit} className="space-y-4" noValidate>
        {banner && <ErrorBanner>{banner}</ErrorBanner>}
        <Field
          label={t.templates.name}
          value={name}
          maxLength={TEMPLATE_NAME_MAX_LENGTH}
          onChange={(event) => setName(event.target.value)}
          error={fields.name}
          autoFocus
        />
        <Field
          label={t.templates.description}
          value={description}
          rows={2}
          maxLength={TEMPLATE_DESCRIPTION_MAX_LENGTH}
          onChange={(event) => setDescription(event.target.value)}
          error={fields.description}
        />
        <Field
          label={t.templates.pageTitle}
          value={title}
          maxLength={TEMPLATE_TITLE_MAX_LENGTH}
          onChange={(event) => setTitle(event.target.value)}
          error={fields.title}
          hint={t.templates.pageTitleHint}
        />
        <Variables
          rows={rows}
          onAdd={() => setRows((current) => [...current, emptyRow()])}
          onChange={change}
          onRemove={(key) => setRows((current) => current.filter((row) => row.key !== key))}
          onInsert={(row) => editor.current?.insertVariable(row.name)}
        />
        <section className="space-y-1" aria-labelledby="template-body-label">
          <h2 id="template-body-label" className="text-sm font-medium text-ink-muted">
            {t.templates.body}
          </h2>
          <p className="text-xs text-ink-subtle">{t.templates.bodyHint}</p>
          <Editor
            id="template-body"
            value={initialBody}
            onChange={setBody}
            variant="template"
            aria-label={t.templates.body}
            handle={(handle) => {
              editor.current = handle;
            }}
          />
        </section>
      </form>
    </div>
  );
}

function Variables({
  rows,
  onAdd,
  onChange,
  onRemove,
  onInsert,
}: {
  rows: Row[];
  onAdd: () => void;
  onChange: (key: number, next: Partial<Row>) => void;
  onRemove: (key: number) => void;
  onInsert: (row: Row) => void;
}) {
  return (
    <fieldset className="min-w-0 space-y-3 rounded-overlay border border-border p-3" aria-labelledby="template-variables" data-template-variables="">
      <legend id="template-variables" className="px-1 text-sm font-medium text-ink">
        {t.templates.variables}
      </legend>
      <p className="text-xs text-ink-muted">{t.templates.variablesHint}</p>
      {rows.length === 0 && <p className="text-sm text-ink-subtle">{t.templates.noVariables}</p>}
      <ol className="space-y-3">
        {rows.map((row, i) => (
          <VariableRow
            key={row.key}
            row={row}
            position={i + 1}
            onChange={(next) => onChange(row.key, next)}
            onRemove={() => onRemove(row.key)}
            onInsert={() => onInsert(row)}
          />
        ))}
      </ol>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        icon={<Icon.Plus />}
        onClick={onAdd}
        disabled={rows.length >= TEMPLATE_VARIABLES_MAX}
        data-action="add-variable"
      >
        {t.templates.addVariable}
      </Button>
    </fieldset>
  );
}

function VariableRow({
  row,
  position,
  onChange,
  onRemove,
  onInsert,
}: {
  row: Row;
  position: number;
  onChange: (next: Partial<Row>) => void;
  onRemove: () => void;
  onInsert: () => void;
}) {
  const id = useId();
  const named = row.label.trim() || t.templates.variableN(position);
  const options = optionsOf(row.options);
  return (
    <li className="space-y-2 rounded-control border border-border bg-surface-sunken p-3" data-variable-row={row.name || position}>
      <div className="grid gap-2 sm:grid-cols-3">
        <Field
          id={`${id}-label`}
          label={t.templates.variableLabel}
          value={row.label}
          maxLength={TEMPLATE_VARIABLE_LABEL_MAX_LENGTH}
          onChange={(event) => onChange({ label: event.target.value })}
        />
        <Field
          id={`${id}-name`}
          label={t.templates.variableName}
          value={row.name}
          maxLength={TEMPLATE_VARIABLE_NAME_MAX_LENGTH}
          className="font-mono"
          onChange={(event) => onChange({ name: event.target.value, nameTyped: true })}
          hint={row.name ? t.templates.writtenAs(blankText(row.name)) : undefined}
        />
        <Select
          id={`${id}-kind`}
          label={t.templates.variableKind}
          value={row.kind}
          onChange={(event) => onChange({ kind: event.target.value as VariableKind, default: "" })}
        >
          {TEMPLATE_VARIABLE_KINDS.map((kind) => (
            <option key={kind} value={kind}>
              {t.templates.kinds[kind]}
            </option>
          ))}
        </Select>
      </div>
      {row.kind === "select" && (
        <Field
          id={`${id}-options`}
          label={t.templates.options}
          rows={3}
          value={row.options}
          onChange={(event) => onChange({ options: event.target.value })}
          hint={t.templates.optionsHint(TEMPLATE_OPTIONS_MAX)}
        />
      )}
      <DefaultControl id={`${id}-default`} row={row} options={options} onChange={(value) => onChange({ default: value })} />
      <div className="flex flex-wrap items-center gap-3">
        <Checkbox label={t.templates.required} checked={row.required} onChange={(event) => onChange({ required: event.target.checked })} />
        <span className="flex-1" />
        <Button type="button" size="sm" variant="secondary" icon={<Icon.Plus />} onClick={onInsert} disabled={!row.name} data-action="insert-variable">
          {t.templates.insert(named)}
        </Button>
        <IconButton icon={<Icon.Trash />} label={t.templates.removeVariable(named)} size="sm" onClick={onRemove} data-action="remove-variable" />
      </div>
    </li>
  );
}

function DefaultControl({ id, row, options, onChange }: { id: string; row: Row; options: string[]; onChange: (value: string) => void }) {
  switch (row.kind) {
    case "person":
      return <p className="text-xs text-ink-subtle">{t.templates.personNoDefault}</p>;
    case "select":
      return (
        <Select id={id} label={t.templates.defaultValue} value={row.default} onChange={(event) => onChange(event.target.value)}>
          <option value="">{t.templates.noDefault}</option>
          {options.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </Select>
      );
    case "date":
      return (
        <div className="flex flex-wrap items-end gap-3">
          <Checkbox
            label={t.templates.today}
            checked={row.default === TEMPLATE_DEFAULT_TODAY}
            onChange={(event) => onChange(event.target.checked ? TEMPLATE_DEFAULT_TODAY : "")}
          />
          {row.default !== TEMPLATE_DEFAULT_TODAY && (
            <Field id={id} label={t.templates.defaultValue} type="date" value={row.default} onChange={(event) => onChange(event.target.value)} />
          )}
        </div>
      );
    default:
      return (
        <Field
          id={id}
          label={t.templates.defaultValue}
          value={row.default}
          maxLength={TEMPLATE_TEXT_VALUE_MAX_LENGTH}
          onChange={(event) => onChange(event.target.value)}
        />
      );
  }
}
