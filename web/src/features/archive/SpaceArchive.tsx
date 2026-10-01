import { useState } from "react";
import { useArchiveSpace } from "@/api/archive";
import type { Space } from "@/api/spaces";
import { Button, Card, ErrorBanner, SectionTitle } from "@/components/ui";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const archivedOn = localDateFormat({ dateStyle: "medium" });

/** Archiving the whole space, or bringing it back, for its administrators. */
export function SpaceArchive({ space }: { space: Space }) {
  const a = t.archive;
  const toggle = useArchiveSpace(space.key);
  const [notice, setNotice] = useState("");
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
          onClick={() => {
            if (archived) {
              toggle.mutate(false, { onSuccess: () => setNotice(a.spaceUnarchived) });
            } else if (window.confirm(a.confirmArchiveSpace(space.name))) {
              toggle.mutate(true, { onSuccess: () => setNotice(a.archived(space.name)) });
            }
          }}
          data-action={archived ? "unarchive-space" : "archive-space"}
        >
          {archived ? a.spaceUnarchive : a.spaceArchive}
        </Button>
        <span role="status" className="text-sm text-ink-muted">
          {notice}
        </span>
      </div>
    </Card>
  );
}
