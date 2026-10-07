import { templateTitle } from "@/api/templates";
import type { TemplateButtonQuery, TemplateButtonTarget } from "@/api/templateButton";
import { TEMPLATE_BUTTON_LABEL_MAX_LENGTH, TEMPLATE_BUTTON_TITLE_MAX_LENGTH } from "@/config";

export const TEMPLATE_BUTTON_NODE = "templateButton";

/** What a template button stores: the template, where its page goes and what it is called; never the pages made. */
export interface TemplateButtonSettings {
  /** A template's key, as the list of templates gives it; empty until one is picked. */
  template: string;
  /** The space whose top the page goes at, null for the space of the page holding the button. */
  space: string | null;
  /** The page the new one goes under, null for the top of the space. */
  parent: string | null;
  /** The button's words, empty for the template's name. */
  label: string;
  /** The new page's title, {date} for the day it is made; empty for the template's own. */
  title: string;
}

const KEY = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const SPACE = /^[A-Z][A-Z0-9]{1,9}$/;
const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

const text = (value: unknown, max: number) => (typeof value === "string" ? value.slice(0, max) : "");

/** A block's attributes as the server takes them, each one wrong put right. */
export function templateButtonSettings(attrs: Record<string, unknown> | undefined): TemplateButtonSettings {
  return {
    template: typeof attrs?.template === "string" && KEY.test(attrs.template) ? attrs.template : "",
    space: typeof attrs?.space === "string" && SPACE.test(attrs.space) ? attrs.space : null,
    parent: typeof attrs?.parent === "string" && ID.test(attrs.parent) ? attrs.parent : null,
    label: text(attrs?.label, TEMPLATE_BUTTON_LABEL_MAX_LENGTH),
    title: text(attrs?.title, TEMPLATE_BUTTON_TITLE_MAX_LENGTH),
  };
}

/** Where the button's page goes, for a button on a page of pageSpace: the page it names, else the top of its space or the page's own. */
export function buttonTarget(settings: TemplateButtonSettings, pageSpace: string | null): TemplateButtonQuery {
  return { template: settings.template, spaceKey: settings.space ?? pageSpace, parentId: settings.parent };
}

/** The title of a page made now: the button's pattern, else the template's, the day in the reader's own calendar; empty leaves it to the server, which takes the template's name. */
export function buttonTitle(settings: TemplateButtonSettings, target: TemplateButtonTarget | undefined, now: Date = new Date()): string {
  const pattern = settings.title.trim() || target?.template.title.trim() || "";
  return templateTitle(pattern, now);
}
