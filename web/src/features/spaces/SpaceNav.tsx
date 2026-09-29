import { useParams } from "@tanstack/react-router";
import { useSpace } from "@/api/spaces";
import { NavItem } from "@/features/shell/nav";
import { SidebarGroup } from "@/features/shell/SidebarGroup";
import { t } from "@/i18n";

/** The space the reader is in, in the sidebar: its home and its settings. */
export function SpaceNav({ open, onToggle, onNavigate }: { open: boolean; onToggle: () => void; onNavigate?: () => void }) {
  const { spaceKey } = useParams({ strict: false });
  const { data: space } = useSpace(spaceKey ?? "");
  if (!spaceKey || !space) return null;
  return (
    <SidebarGroup id="space" title={space.name} open={open} onToggle={onToggle}>
      <div data-space-nav={space.key}>
        <NavItem to={`/s/${space.key}`} exact icon="Home" rail={false} onNavigate={onNavigate}>
          {t.space.home}
        </NavItem>
        <NavItem to={`/s/${space.key}/settings`} icon="Settings" rail={false} onNavigate={onNavigate}>
          {t.space.settings}
        </NavItem>
      </div>
    </SidebarGroup>
  );
}
