import { useState } from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { usePage, type Page } from "@/api/pages";
import { useSpace } from "@/api/spaces";
import type { TreeNode } from "@/api/tree";
import { PageTree } from "@/features/pages/PageTree";
import { PlaceDialog } from "@/features/pages/PlaceDialog";
import { NavItem } from "@/features/shell/nav";
import { SidebarGroup } from "@/features/shell/SidebarGroup";
import { SidebarShortcuts } from "@/features/shortcuts/SidebarShortcuts";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";

/** The space the reader is in, in the sidebar: its home, its shortcuts, its pages as a tree, and its settings. */
export function SpaceNav({ open, onToggle, onNavigate }: { open: boolean; onToggle: () => void; onNavigate?: () => void }) {
  const { spaceKey, pageId } = useParams({ strict: false });
  const { data: space } = useSpace(spaceKey ?? "");
  const { data: reading } = usePage(pageId);
  const [moving, setMoving] = useState<TreeNode>();
  const navigate = useNavigate();
  if (!spaceKey || !space) return null;
  const openPath = reading ? reading.page.ancestors.filter((each) => !each.home).map((each) => each.id) : [];

  const arrived = (page: Page) => {
    setMoving(undefined);
    void navigate({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: page.spaceKey, pageId: page.id, slug: pageSlug(page.title) } });
  };

  return (
    <SidebarGroup id="space" title={space.name} open={open} onToggle={onToggle}>
      <div data-space-nav={space.key}>
        <NavItem to={`/s/${space.key}`} exact icon="Home" rail={false} onNavigate={onNavigate}>
          {t.space.home}
        </NavItem>
        <SidebarShortcuts spaceKey={space.key} onNavigate={onNavigate} />
        <div className="my-1">
          <PageTree space={space} currentId={pageId} openPath={openPath} onMove={setMoving} />
        </div>
        <NavItem to={`/s/${space.key}/blog`} icon="Megaphone" rail={false} onNavigate={onNavigate}>
          {t.space.blog}
        </NavItem>
        <NavItem to={`/s/${space.key}/decisions`} icon="Decision" rail={false} onNavigate={onNavigate}>
          {t.space.decisions}
        </NavItem>
        {space.can.deletePages && (
          <NavItem to={`/s/${space.key}/settings`} search={{ tab: "trash" }} icon="Trash" rail={false} onNavigate={onNavigate}>
            {t.space.trash}
          </NavItem>
        )}
        <NavItem to={`/s/${space.key}/settings`} search={{ tab: "archive" }} icon="Archive" rail={false} onNavigate={onNavigate}>
          {t.space.archive}
        </NavItem>
        <NavItem to={`/s/${space.key}/settings`} icon="Settings" rail={false} onNavigate={onNavigate}>
          {t.space.settings}
        </NavItem>
      </div>
      {moving && (
        <PlaceDialog page={{ id: moving.id, title: moving.title, spaceKey: space.key }} mode="move" onClose={() => setMoving(undefined)} onDone={arrived} />
      )}
    </SidebarGroup>
  );
}
