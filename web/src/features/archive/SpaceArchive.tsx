import { useArchiveSpace } from "@/api/archive";
import type { Space } from "@/api/spaces";
import { Button, Card, ErrorBanner, SectionTitle, useToast } from "@/components/ui";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { useConfirm } from "@/features/shell/ConfirmProvider";

const archivedOn = localDateFormat({ dateStyle: "medium" });

/** Archiving the whole space, or bringing it back, for its administrators. */
export function SpaceArchive({ space }: { space: Space }) {
  const a = t.archive;
  const toggle = useArchiveSpace(space.key);
  const toast = useToast();
  const confirm = useConfirm();
  const archived = Boolean(space.archivedAt);
  return (
    <Card className="space-y-3 p-4" data-space-archive-card={archived ? "archived" : "live"}>
      <SectionTitle>{a.spaceTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">
        {archived && space.archivedAt ? a.spaceArchivedBody(space.archivedByName, archivedOn.format(new Date(space.archivedAt))) : a.spaceBody}
      </p>
      {toggle.error && <ErrorBanner>{toggle.error.message}</ErrorBanner>}
      <div className="flex flex-wrap items-center gap-3">
        <Button
          variant="secondary"
          loading={toggle.isPending}
          onClick={async () => {
            if (archived) {
              toggle.mutate(false, { onSuccess: () => toast.success(a.spaceUnarchived) });
            } else if (await confirm({ noun: t.kit.confirm.things.space(space.name), verb: t.kit.confirm.verbs.archive, body: a.confirmArchiveSpace })) {
              toggle.mutate(true, { onSuccess: () => toast.success(a.archived(space.name)) });
            }
          }}
          data-action={archived ? "unarchive-space" : "archive-space"}
        >
          {archived ? a.spaceUnarchive : a.spaceArchive}
        </Button>
      </div>
    </Card>
  );
}
