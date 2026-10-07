import { useId, useMemo } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { SubjectSearch, SubjectSearchResult } from "@/api/permissions";
import type { TemplateVariable } from "@/api/templates";
import { IconButton, Field, Select } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PICKER_LIMIT, TEMPLATE_DEFAULT_TODAY, TEMPLATE_TEXT_VALUE_MAX_LENGTH } from "@/config";
import { SubjectPicker } from "@/features/permissions/SubjectPicker";
import { today } from "@/features/editor/InlineValueViews";
import { t } from "@/i18n";

/** A person variable's answer: the id the API takes and the name the form shows. */
export interface PersonValue {
  id: string;
  name: string;
}

/** What the form holds for each variable, by name. */
export type Values = Record<string, string | PersonValue | undefined>;

/** Each variable's starting value: its default, today's local day for a date that defaults to today. */
export function initialValues(variables: TemplateVariable[]): Values {
  const out: Values = {};
  for (const v of variables) {
    if (v.kind === "person") continue;
    out[v.name] = v.kind === "date" && v.default === TEMPLATE_DEFAULT_TODAY ? today() : v.default;
  }
  return out;
}

/** The values as the API takes them: strings by name, a person by id, empty ones left out so the server's default applies. */
export function wireValues(values: Values): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [name, value] of Object.entries(values)) {
    const wire = typeof value === "object" ? value.id : (value ?? "").trim();
    if (wire) out[name] = wire;
  }
  return out;
}

/** The variables a form still needs before it can be sent, by label. */
export function missingValues(variables: TemplateVariable[], values: Values): string[] {
  return variables.filter((v) => v.required && !wireValues({ [v.name]: values[v.name] })[v.name]).map((v) => v.label);
}

// People who may view the page the new one goes under, so a mention reaches
// them; the server checks the space again.
function viewersOf(pageId: string): SubjectSearch {
  return function useViewers(q: string, enabled: boolean): SubjectSearchResult {
    const query = { q: q || undefined, limit: PICKER_LIMIT };
    const found = useQuery({
      queryKey: ["template-people", pageId, query],
      queryFn: async () => (await api.GET("/pages/{pageID}/mentionable", { params: { path: { pageID: pageId }, query } })).data!.people,
      enabled,
      placeholderData: keepPreviousData,
    });
    return {
      people: (found.data ?? []).filter((person) => person.canView),
      groups: [],
      error: found.error,
      current: found.data !== undefined && !found.isPlaceholderData,
    };
  };
}

/**
 * The form a template's variables ask for when a page is made from it, one
 * control per kind; the server fills the page from what it sends.
 */
export function TemplateValues({
  variables,
  values,
  onChange,
  parentId,
  errors,
}: {
  variables: TemplateVariable[];
  values: Values;
  onChange: (values: Values) => void;
  /** The page the new one goes under, whose viewers a person variable offers. */
  parentId: string;
  /** The API's sentences, keyed values.<name>. */
  errors: Record<string, string>;
}) {
  const heading = useId();
  if (variables.length === 0) return null;
  return (
    <fieldset className="min-w-0 space-y-3 rounded-overlay border border-border p-3" aria-labelledby={heading} data-template-values="">
      <legend id={heading} className="px-1 text-sm font-medium text-ink">
        {t.templates.fillIn}
      </legend>
      <p className="text-xs text-ink-muted">{t.templates.fillInHint}</p>
      {variables.map((v) => (
        <ValueControl
          key={v.name}
          variable={v}
          value={values[v.name]}
          onChange={(next) => onChange({ ...values, [v.name]: next })}
          parentId={parentId}
          error={errors[`values.${v.name}`]}
        />
      ))}
    </fieldset>
  );
}

function ValueControl({
  variable: v,
  value,
  onChange,
  parentId,
  error,
}: {
  variable: TemplateVariable;
  value: Values[string];
  onChange: (value: Values[string]) => void;
  parentId: string;
  error?: string;
}) {
  const id = `template-value-${useId()}`;
  const label = v.required ? t.templates.requiredLabel(v.label) : v.label;
  const text = typeof value === "string" ? value : "";
  const attrs = { "data-template-value": v.name };
  switch (v.kind) {
    case "date":
      return (
        <Field
          id={id}
          label={label}
          type="date"
          value={text}
          onChange={(event) => onChange(event.target.value)}
          error={error}
          required={v.required}
          {...attrs}
        />
      );
    case "select":
      return (
        <Select id={id} label={label} value={text} onChange={(event) => onChange(event.target.value)} error={error} required={v.required} {...attrs}>
          {(!v.required || !text) && <option value="">{t.templates.noChoice}</option>}
          {v.options.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </Select>
      );
    case "person":
      return (
        <PersonControl
          id={id}
          label={label}
          value={typeof value === "object" ? value : undefined}
          onChange={onChange}
          parentId={parentId}
          error={error}
          name={v.name}
        />
      );
    default:
      return (
        <Field
          id={id}
          label={label}
          value={text}
          maxLength={TEMPLATE_TEXT_VALUE_MAX_LENGTH}
          onChange={(event) => onChange(event.target.value)}
          error={error}
          required={v.required}
          {...attrs}
        />
      );
  }
}

function PersonControl({
  id,
  label,
  name,
  value,
  onChange,
  parentId,
  error,
}: {
  id: string;
  label: string;
  name: string;
  value: PersonValue | undefined;
  onChange: (value: PersonValue | undefined) => void;
  parentId: string;
  error?: string;
}) {
  const useSearch = useMemo(() => viewersOf(parentId), [parentId]);
  return (
    <div className="space-y-1" data-template-value={name} id={id}>
      {value ? (
        <div className="space-y-1">
          <p className="text-sm font-medium text-ink-muted">{label}</p>
          <p className="flex items-center gap-2 text-sm text-ink" data-template-person={value.name}>
            <Icon.User className="shrink-0 text-ink-subtle" />
            <span className="min-w-0 truncate">{value.name}</span>
            <IconButton
              icon={<Icon.X />}
              label={t.templates.removePerson(value.name)}
              size="sm"
              onClick={() => onChange(undefined)}
              data-action="clear-person"
            />
          </p>
        </div>
      ) : (
        <SubjectPicker
          label={label}
          peopleOnly
          useSearch={useSearch}
          onPick={(subject) => {
            if (subject.type === "user" && subject.id) onChange({ id: subject.id, name: subject.name });
          }}
        />
      )}
      {error && <p className="text-sm text-danger">{error}</p>}
    </div>
  );
}
