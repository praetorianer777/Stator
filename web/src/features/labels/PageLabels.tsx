import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { labelPath, useAddLabel, useRemoveLabel } from "@/api/labels";
import type { Page } from "@/api/pages";
import { ErrorBanner, IconButton, SectionTitle } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { LabelCombobox } from "./LabelCombobox";

/** One label as a link to the pages that carry it, in a space or everywhere. */
export function LabelLink({ name, spaceKey, onRemove }: { name: string; spaceKey?: string; onRemove?: () => void }) {
  return (
    <span
      className="inline-flex items-center gap-1 rounded border border-border bg-surface-raised px-1.5 py-px text-xs font-medium text-ink-muted"
      data-label={name}
    >
      <Link {...labelPath(name, spaceKey)} className="inline-flex items-center gap-1 hover:text-ink hover:underline">
        <Icon.Label className="size-3" />
        {name}
      </Link>
      {onRemove && (
        <IconButton
          icon={<Icon.X className="size-3" />}
          label={t.labels.remove(name)}
          size="xs"
          onClick={onRemove}
          className="-my-0.5 -mr-1 text-current hover:bg-transparent hover:text-ink"
          data-action="remove-label"
        />
      )}
    </span>
  );
}

/**
 * The labels on a page, each a way to the other pages that carry it, and for
 * whoever may edit the page a box to add more and a way to take one off.
 */
export function PageLabels({ page }: { page: Page }) {
  const add = useAddLabel(page.id);
  const remove = useRemoveLabel(page.id);
  const [notice, setNotice] = useState("");
  const editable = page.can.edit;
  if (!editable && page.labels.length === 0) return null;
  const error = add.error ?? remove.error;

  return (
    <section aria-labelledby="labels-title" className="mt-10" data-page-labels>
      <SectionTitle id="labels-title">{t.labels.title}</SectionTitle>
      {page.labels.length > 0 && (
        <ul className="mt-2 flex flex-wrap gap-1.5" aria-labelledby="labels-title" data-label-list>
          {page.labels.map((name) => (
            <li key={name}>
              <LabelLink
                name={name}
                spaceKey={page.spaceKey}
                onRemove={
                  editable
                    ? () => {
                        add.reset();
                        remove.mutate(name, { onSuccess: () => setNotice(t.labels.removed(name)) });
                      }
                    : undefined
                }
              />
            </li>
          ))}
        </ul>
      )}
      {editable && (
        <div className="mt-3 max-w-xs">
          <LabelCombobox
            label={t.labels.add}
            exclude={page.labels}
            onPick={(name) => {
              remove.reset();
              add.mutate(name, { onSuccess: () => setNotice(t.labels.added(name)) });
            }}
          />
        </div>
      )}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      <p role="status" className="sr-only" data-label-notice>
        {notice}
      </p>
    </section>
  );
}
