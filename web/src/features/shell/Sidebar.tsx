import { APP_NAME, APP_VERSION, SIDEBAR_WIDTH } from "@/config";
import { t } from "@/i18n";
import { NavItem } from "./nav";
import { Rail } from "./Rail";
import { SidebarGroup } from "./SidebarGroup";
import { useSidebarGroups, useSidebarMode } from "./state";

// The sidebar is the map: where you are is highlighted and where you can go is
// listed in groups that fold. The rail beside it stays when the map is folded.
export function Sidebar() {
  const [mode, toggleMode] = useSidebarMode();
  const open = mode === "open";
  const [groups, toggleGroup] = useSidebarGroups();
  const isOpen = (id: string, defaultOpen: boolean) => groups[id] ?? defaultOpen;

  return (
    <>
      <Rail open={open} onToggle={toggleMode} />
      {open && (
        <nav
          className="flex shrink-0 flex-col border-r border-border bg-surface"
          style={{ width: SIDEBAR_WIDTH }}
          data-sidebar={mode}
          data-print-hide
          aria-label={t.nav.whereYouAre}
        >
          <div className="flex h-12 items-center border-b border-border px-3">
            <span className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">{APP_NAME}</span>
          </div>

          <div className="flex-1 overflow-y-auto px-2 py-3">
            <SidebarGroup id="wiki" title={t.nav.groupWiki} open={isOpen("wiki", true)} onToggle={() => toggleGroup("wiki", true)}>
              <NavItem to="/" exact icon="Home" rail={false}>
                {t.nav.home}
              </NavItem>
              <NavItem to="/spaces" icon="Space" rail={false}>
                {t.nav.spaces}
              </NavItem>
              <NavItem to="/search" icon="Search" rail={false} trailing={<kbd className="font-mono text-2xs text-ink-subtle">{t.search.shortcut}</kbd>}>
                {t.nav.search}
              </NavItem>
            </SidebarGroup>
          </div>

          <div className="truncate px-3 pb-2 text-2xs text-ink-subtle" data-build>
            {APP_VERSION}
          </div>
        </nav>
      )}
    </>
  );
}
