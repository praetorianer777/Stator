import { useState } from "react";
import type { Page } from "@/api/pages";
import { useRemoteLinks, useRemoveRemoteLink, type RemoteLink } from "@/api/remoteLinks";
import { Button, ErrorBanner, SectionTitle, useToast } from "@/components/ui";
import { t } from "@/i18n";

/** The links in the order the API lists them, under one heading per source. */
function bySource(links: RemoteLink[]): [string, RemoteLink[]][] {
  const groups = new Map<string, RemoteLink[]>();
  for (const link of links) groups.set(link.source, [...(groups.get(link.source) ?? []), link]);
  return [...groups];
}

/**
 * Objects in other tools about this page, such as the requirements it
 * specifies, put here by those tools; the section appears once there is one,
 * and whoever may edit the page can take a link off.
 */
export function PageRemoteLinks({ page }: { page: Page }) {
  const links = useRemoteLinks(page.id);
  const remove = useRemoveRemoteLink(page.id);
  const toast = useToast();
  const rows = links.data ?? [];
  // With its last link gone the section goes too; the toast says so.
  if (rows.length === 0) return null;

  return (
    <section aria-labelledby="remote-links-title" className="mt-10" data-remote-links="">
      <SectionTitle id="remote-links-title">{t.remoteLinks.title}</SectionTitle>
      <p className="mt-1 text-sm text-ink-muted">{t.remoteLinks.intro}</p>
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      {bySource(rows).map(([source, held]) => (
        <div key={source} className="mt-3" data-remote-link-group={source}>
          <h3 className="text-xs font-semibold text-ink-muted">{t.remoteLinks.group(source, held.length)}</h3>
          <ul aria-label={source} className="mt-1 divide-y divide-border rounded-md border border-border">
            {held.map((link) => (
              <li key={link.id} className="flex items-center gap-2 px-3 py-1.5 text-sm" data-remote-link={link.url}>
                <SourceIcon link={link} />
                <span className="flex min-w-0 flex-1 flex-col">
                  <a href={link.url} target="_blank" rel="noopener noreferrer" className="truncate text-accent hover:underline">
                    {link.title}
                  </a>
                  <span className="text-xs text-ink-subtle">{t.remoteLinks.putBy(link.createdByName)}</span>
                </span>
                {page.can.edit && (
                  <Button
                    variant="link"
                    className="shrink-0 text-xs text-ink-subtle hover:text-danger"
                    aria-label={t.remoteLinks.remove(link.title)}
                    disabled={remove.isPending}
                    onClick={() => remove.mutate(link, { onSuccess: () => toast.success(t.remoteLinks.removed(link.title)) })}
                    data-action="remove-remote-link"
                  >
                    {t.remoteLinks.removeShort}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </div>
      ))}
      {status}
    </section>
  );
}

// The icon lives on the other tool's server, which may refuse it or be out
// of reach, as may the page's own policy; the source's initial stands in so
// the row keeps its shape, as Armature does.
function SourceIcon({ link }: { link: RemoteLink }) {
  const [broken, setBroken] = useState(false);
  if (link.iconUrl && !broken) {
    return (
      <img
        src={link.iconUrl}
        alt=""
        referrerPolicy="no-referrer"
        className="size-4 shrink-0 rounded-sm"
        onError={() => setBroken(true)}
        data-remote-link-icon=""
      />
    );
  }
  return (
    <span aria-hidden className="flex size-4 shrink-0 items-center justify-center rounded-sm bg-surface-sunken text-2xs font-semibold text-ink-muted uppercase">
      {link.source.slice(0, 1)}
    </span>
  );
}
