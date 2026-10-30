import { useArchiveSpace } from "@/api/archive";
import type { Space } from "@/api/spaces";
import { Button, Card, ErrorBanner, SectionTitle, useToast } from "@/components/ui";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const archivedOn = localDateFormat({ dateStyle: "medium" });

/** Archiving the whole space, at once and with Undo since nothing is lost, or bringing it back, for its administrators. */
export function SpaceArchive({ space }: { space: Space }) {
  const a = t.archive;
  const toggle = useArchiveSpace(space.key);
  const toast = useToast();
  const archived = Boolean(space.archivedAt);
  const unarchive = () => toggle.mutate(false, { onSuccess: () => toast.success(a.spaceUnarchived) });
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
          onClick={() => {
            if (archived) {
              unarchive();
              return;
            }
            toggle.mutate(true, { onSuccess: () => toast.success(a.archived(space.name), { action: { label: a.undo, onClick: unarchive } }) });
          }}
          data-action={archived ? "unarchive-space" : "archive-space"}
        >
          {archived ? a.spaceUnarchive : a.spaceArchive}
        </Button>
      </div>
    </Card>
  );
}
