import { localDateFormat } from "@/lib/format";
import type { Space } from "@/api/spaces";
import { useEmptyTrash, usePurgePage, useRestorePage, useTrash } from "@/api/trash";
import { Button, EmptyState, ErrorBanner, Skeleton, Table, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { useConfirm } from "@/features/shell/ConfirmProvider";

const deletedAt = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** A space's trash: what was deleted, by whom and where it goes back; restoring for editors, purging for administrators. */
export function TrashPanel({ space }: { space: Space }) {
  const s = t.spaceSettings;
  const { data: items, isLoading, error, refetch } = useTrash(space.key, space.can.deletePages);
  const restore = useRestorePage(space.key);
  const purge = usePurgePage(space.key);
  const empty = useEmptyTrash(space.key);
  const toast = useToast();
  const confirm = useConfirm();
  const failure = restore.error ?? purge.error ?? empty.error;
  if (!space.can.deletePages)
    return (
      <p className="text-sm text-ink-muted" data-space-trash={space.key}>
        {s.notTrasher}
      </p>
    );

  return (
    <div className="space-y-4" data-space-trash={space.key}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="max-w-xl text-sm text-ink-muted">{s.trashIntro}</p>
        {space.can.purgeTrash && items && items.length > 0 && (
          <Button
            variant="danger"
            loading={empty.isPending}
            onClick={async () => {
              const ask = { noun: t.kit.confirm.things.trash, verb: t.kit.confirm.verbs.empty, body: s.confirmEmpty };
              if (await confirm(ask)) empty.mutate(undefined, { onSuccess: () => toast.success(s.emptied) });
            }}
            data-action="empty-trash"
          >
            {s.emptyTrash}
          </Button>
        )}
      </div>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {items && items.length === 0 && <EmptyState icon={<Icon.Trash />} title={s.trashEmpty} />}
      {items && items.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>{s.columnPage}</Th>
              <Th>{s.columnDeleted}</Th>
              <Th>{s.columnGoesBack}</Th>
              <Th>
                <span className="sr-only">{t.page.actions}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.id} data-trash-item={item.title}>
                <Td>
                  <span className="font-medium text-ink">{item.title}</span>
                  <span className="block text-xs text-ink-muted">{s.pages(item.pages)}</span>
                </Td>
                <Td className="text-ink-muted">{s.deleted(item.trashedByName, deletedAt.format(new Date(item.trashedAt)))}</Td>
                <Td className="text-ink-muted">{item.kind === "post" ? s.toBlog : item.parentInTree ? s.backUnder(item.parentTitle) : s.underHome}</Td>
                <Td>
                  <div className="flex justify-end gap-2">
                    {space.can.deletePages && (
                      <Button
                        size="sm"
                        variant="secondary"
                        aria-label={s.restoreItem(item.title)}
                        onClick={() => restore.mutate(item.id, { onSuccess: () => toast.success(s.restored(item.title)) })}
                        data-action="restore-page"
                      >
                        {s.restore}
                      </Button>
                    )}
                    {space.can.purgeTrash && (
                      <Button
                        size="sm"
                        variant="danger"
                        aria-label={s.purgeItem(item.title)}
                        onClick={async () => {
                          if (await confirm({ noun: t.kit.confirm.things.page(item.title), body: s.confirmPurge }))
                            purge.mutate(item.id, { onSuccess: () => toast.success(s.purged(item.title)) });
                        }}
                        data-action="purge-page"
                      >
                        {s.purge}
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
