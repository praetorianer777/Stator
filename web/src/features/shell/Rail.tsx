import { IconButton } from "@/components/ui";
import { HomeNavItem, HubNavItem } from "@/features/hub/HubNav";
import { Icon } from "@/components/icons";
import { SIDEBAR_RAIL_WIDTH } from "@/config";
import { t } from "@/i18n";
import { NavItem } from "./nav";
import { SIDEBAR_ID } from "./state";
import { ThemeButton } from "./ThemeButton";

/**
 * The rail is what is always there: the places every page reaches from, and
 * the theme at the bottom. When the sidebar is folded away, it is the whole chrome.
 */
export function Rail({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  return (
    <aside className="flex shrink-0 flex-col items-center border-r border-border bg-surface" style={{ width: SIDEBAR_RAIL_WIDTH }} data-rail data-print-hide>
      <div className="flex h-12 w-full items-center justify-center border-b border-border">
        <IconButton
          icon={open ? <Icon.Collapse /> : <Icon.Expand />}
          label={open ? t.nav.collapseSidebar : t.nav.expandSidebar}
          size="sm"
          onClick={onToggle}
          aria-expanded={open}
          aria-controls={open ? SIDEBAR_ID : undefined}
          data-action="sidebar"
        />
      </div>
      <nav aria-label={t.nav.everywhere} className="flex flex-1 flex-col items-center gap-1 py-3">
        <HomeNavItem rail />
        <HubNavItem rail />
        <NavItem to="/spaces" icon="Space" rail>
          {t.nav.spaces}
        </NavItem>
        <NavItem to="/search" icon="Search" rail>
          {t.nav.search}
        </NavItem>
      </nav>
      <div className="flex flex-col items-center gap-1 border-t border-border py-2">
        <ThemeButton />
      </div>
    </aside>
  );
}
