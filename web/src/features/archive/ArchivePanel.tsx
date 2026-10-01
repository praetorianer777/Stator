import { useState } from "react";
import { useArchivedPages, useArchivePage } from "@/api/archive";
import type { Space } from "@/api/spaces";
import { Button, EmptyState, ErrorBanner, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const archivedAt = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** A space's archive: what was archived, by whom and where it hangs; anybody who reads the space reads it, its administrators unarchive. */
export function ArchivePanel({ space }: { space: Space }) {
  const a = t.archive;
  const { data: items, isLoading, error, refetch } = useArchivedPages(space.key);
  const unarchive = useArchivePage();
  const [notice, setNotice] = useState("");
  const mayUnarchive = space.can.administer && !space.archivedAt;

  return (
    <div className="space-y-4" data-space-archive={space.key}>
      <p className="max-w-xl text-sm text-ink-muted">{a.intro}</p>
      <p role="status" className="text-sm text-ink-muted [&:empty]:hidden" data-archive-notice>
        {notice}
      </p>
      {unarchive.error && <ErrorBanner>{unarchive.error.message}</ErrorBanner>}
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {items && items.length === 0 && <EmptyState icon={<Icon.Archive />} title={a.empty} />}
      {items && items.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>{a.columnPage}</Th>
              <Th>{a.columnArchived}</Th>
              <Th>{a.columnUnder}</Th>
              <Th>
                <span className="sr-only">{t.page.actions}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.id} data-archive-item={item.title}>
                <Td>
                  <PageLink spaceKey={space.key} id={item.id} title={item.title} className="font-medium text-ink hover:text-accent hover:underline" />
                  <span className="block text-xs text-ink-muted">{a.pages(item.pages)}</span>
                </Td>
                <Td className="text-ink-muted">{a.archivedAt(item.archivedByName, archivedAt.format(new Date(item.archivedAt)))}</Td>
                <Td className="text-ink-muted">{item.parentTitle}</Td>
                <Td>
                  <div className="flex justify-end">
                    {mayUnarchive && (
                      <Button
                        size="sm"
                        variant="secondary"
                        aria-label={a.unarchiveItem(item.title)}
                        onClick={() => unarchive.mutate({ id: item.id, archived: false }, { onSuccess: () => setNotice(a.unarchived(item.title)) })}
                        data-action="unarchive-item"
                      >
                        {a.unarchive}
                      </Button>
                    )}
                  </div>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}
