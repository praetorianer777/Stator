import { useRef, type KeyboardEvent } from "react";
import { useSpaceTemplates, type SpaceTemplatePage } from "@/api/templates";
import { ErrorBanner, OptionCard, Tag } from "@/components/ui";
import { BLANK_SPACE_EVERYONE, BLANK_SPACE_TEMPLATE } from "@/config";
import { t } from "@/i18n";

const STEPS: Record<string, number> = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 };

function PageOutline({ pages }: { pages: SpaceTemplatePage[] }) {
  return (
    <ul className="ms-4 list-disc space-y-1">
      {pages.map((page) => (
        <li key={page.title} data-template-page={page.title}>
          <span className="text-ink">{page.title}</span>
          {page.labels.length > 0 && (
            <span className="ms-2 inline-flex flex-wrap gap-1" role="list" aria-label={t.spaces.templateLabels}>
              {page.labels.map((label) => (
                <Tag key={label} role="listitem">
                  {label}
                </Tag>
              ))}
            </span>
          )}
          {page.children.length > 0 && <PageOutline pages={page.children} />}
        </li>
      ))}
    </ul>
  );
}

/**
 * Picks what a new space starts from, blank or a space template, as a radio group like the page
 * template picker, with the chosen one's pages, labels and permissions beside it.
 */
export function SpaceTemplatePicker({ value, onChange, error }: { value: string; onChange: (key: string) => void; error?: string }) {
  const { data: templates = [], isLoading, isError } = useSpaceTemplates();
  const group = useRef<HTMLDivElement>(null);
  const keys = [BLANK_SPACE_TEMPLATE, ...templates.map((tpl) => tpl.key)];
  const chosen = templates.find((tpl) => tpl.key === value);
  const everyone: readonly string[] = chosen ? chosen.permissions.everyone : BLANK_SPACE_EVERYONE;
  const names = everyone.map((p) => t.permissions.spaceNames[p as keyof typeof t.permissions.spaceNames] ?? p);

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const at = keys.indexOf(value);
    let next = -1;
    if (event.key in STEPS) next = (at + (STEPS[event.key] ?? 0) + keys.length) % keys.length;
    else if (event.key === "Home") next = 0;
    else if (event.key === "End") next = keys.length - 1;
    if (next < 0) return;
    event.preventDefault();
    onChange(keys[next] ?? BLANK_SPACE_TEMPLATE);
    group.current?.querySelectorAll<HTMLElement>('[role="radio"]')[next]?.focus();
  }

  return (
    <div className="grid gap-3 sm:grid-cols-[minmax(0,15rem)_minmax(0,1fr)]">
      <fieldset className="min-w-0 space-y-1.5">
        <legend className="mb-1.5 block text-sm font-medium text-ink">{t.spaces.startFrom}</legend>
        {isError && <ErrorBanner>{t.spaces.templatesFailed}</ErrorBanner>}
        {error && (
          <p className="text-sm text-danger" role="alert">
            {error}
          </p>
        )}
        <div
          ref={group}
          role="radiogroup"
          aria-label={t.spaces.startFrom}
          aria-busy={isLoading}
          className="grid gap-1.5"
          onKeyDown={onKeyDown}
          data-space-template-picker=""
        >
          <OptionCard
            checked={value === BLANK_SPACE_TEMPLATE}
            tabIndex={value === BLANK_SPACE_TEMPLATE ? 0 : -1}
            onSelect={() => onChange(BLANK_SPACE_TEMPLATE)}
            title={t.spaces.blank}
            description={t.spaces.blankDescription}
            className="p-2"
            data-space-template="blank"
          />
          {templates.map((tpl) => (
            <OptionCard
              key={tpl.key}
              checked={value === tpl.key}
              tabIndex={value === tpl.key ? 0 : -1}
              onSelect={() => onChange(tpl.key)}
              title={tpl.name}
              description={tpl.description}
              className="p-2"
              data-space-template={tpl.key}
            />
          ))}
        </div>
        {isLoading && <p className="text-xs text-ink-muted">{t.spaces.loadingTemplates}</p>}
      </fieldset>
      <section
        aria-label={t.spaces.templatePreview(chosen?.name ?? t.spaces.blank)}
        className="min-w-0 space-y-3 rounded-overlay border border-border bg-surface p-3 text-sm"
        data-space-template-preview={chosen?.key ?? "blank"}
      >
        <div>
          <h2 className="mb-1 text-xs font-medium text-ink-muted">{t.spaces.templatePages}</h2>
          <p className="text-ink">{t.spaces.templateHome}</p>
          {chosen ? <PageOutline pages={chosen.pages} /> : <p className="text-ink-muted">{t.spaces.blankPreview}</p>}
        </div>
        <div>
          <h2 className="mb-1 text-xs font-medium text-ink-muted">{t.spaces.templateWho}</h2>
          <p className="text-ink" data-space-template-everyone={everyone.join(" ")}>
            {names.length ? t.spaces.everyoneMay(names) : t.spaces.everyoneNothing}
          </p>
          <p className="text-ink-muted">{t.spaces.youAdminister}</p>
        </div>
      </section>
    </div>
  );
}
