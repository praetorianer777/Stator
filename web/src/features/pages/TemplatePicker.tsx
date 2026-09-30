import { useRef, type KeyboardEvent } from "react";
import { useTemplates, type Template } from "@/api/templates";
import { ErrorBanner, OptionCard } from "@/components/ui";
import { BLANK_TEMPLATE } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { t } from "@/i18n";

const STEPS: Record<string, number> = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 };

/**
 * Picks what a new page starts from: blank, or one of the templates. A radio
 * group with one stop in the tab order, arrows moving the choice as in any
 * radio group, and the chosen one drawn read-only beside it.
 */
export function TemplatePicker({ value, onChange }: { value: string; onChange: (key: string, template: Template | undefined) => void }) {
  const { data: templates = [], isLoading, isError } = useTemplates();
  const group = useRef<HTMLDivElement>(null);
  const keys = [BLANK_TEMPLATE, ...templates.map((tpl) => tpl.key)];
  const chosen = templates.find((tpl) => tpl.key === value);
  const pick = (key: string) =>
    onChange(
      key,
      templates.find((tpl) => tpl.key === key),
    );

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const at = keys.indexOf(value);
    let next = -1;
    if (event.key in STEPS) next = (at + (STEPS[event.key] ?? 0) + keys.length) % keys.length;
    else if (event.key === "Home") next = 0;
    else if (event.key === "End") next = keys.length - 1;
    if (next < 0) return;
    event.preventDefault();
    pick(keys[next] ?? BLANK_TEMPLATE);
    group.current?.querySelectorAll<HTMLElement>('[role="radio"]')[next]?.focus();
  }

  return (
    <div className="grid gap-3 sm:grid-cols-[minmax(0,15rem)_minmax(0,1fr)]">
      <fieldset className="min-w-0 space-y-1.5">
        <legend className="mb-1.5 block text-sm font-medium text-ink">{t.page.startFrom}</legend>
        {isError && <ErrorBanner>{t.page.templatesFailed}</ErrorBanner>}
        <div
          ref={group}
          role="radiogroup"
          aria-label={t.page.startFrom}
          aria-busy={isLoading}
          className="grid gap-1.5"
          onKeyDown={onKeyDown}
          data-template-picker=""
        >
          <OptionCard
            checked={value === BLANK_TEMPLATE}
            tabIndex={value === BLANK_TEMPLATE ? 0 : -1}
            onSelect={() => pick(BLANK_TEMPLATE)}
            title={t.page.blank}
            description={t.page.blankDescription}
            className="p-2"
            data-template="blank"
          />
          {templates.map((tpl) => (
            <OptionCard
              key={tpl.key}
              checked={value === tpl.key}
              tabIndex={value === tpl.key ? 0 : -1}
              onSelect={() => pick(tpl.key)}
              title={tpl.name}
              description={tpl.description}
              className="p-2"
              data-template={tpl.key}
            />
          ))}
        </div>
        {isLoading && <p className="text-xs text-ink-muted">{t.page.loadingTemplates}</p>}
      </fieldset>
      <section
        aria-label={t.page.preview(chosen?.name ?? t.page.blank)}
        // biome-ignore lint/a11y/noNoninteractiveTabindex: a scrolling region has to be reachable by keyboard
        tabIndex={0}
        className="max-h-[50vh] min-h-40 overflow-y-auto rounded-overlay border border-border bg-surface p-3 focus-visible:outline-2 focus-visible:outline-focus"
        data-template-preview={chosen?.key ?? "blank"}
      >
        {chosen ? <DocView doc={chosen.body} size="sm" anchors={false} /> : <p className="text-sm text-ink-muted">{t.page.blankPreview}</p>}
      </section>
    </div>
  );
}
