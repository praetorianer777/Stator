import { TEMPLATE_VARIABLE_NAME_PATTERN } from "@/config";
import { t } from "@/i18n";

// Kept apart from the editor's node, so a template's preview draws its blanks
// without bringing the editor along.

export const TEMPLATE_VARIABLE_NODE = "templateVariable";

/** A variable's name as the API takes it, or null. */
export function variableName(value: unknown): string | null {
  return typeof value === "string" && TEMPLATE_VARIABLE_NAME_PATTERN.test(value) ? value : null;
}

/** The words a blank shows: its name in braces, as a template's title names it. */
export function blankText(name: string): string {
  return `{${name}}`;
}

/** A template's blank as readers of the template see it. */
export function Blank({ name }: { name: string }) {
  return (
    <span className="doc-variable" data-template-variable={name}>
      <span className="sr-only">{t.templates.blankPrefix} </span>
      {blankText(name)}
    </span>
  );
}
