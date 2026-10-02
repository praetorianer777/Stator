import { useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { IconButton } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { APP_NAME, APP_VERSION, SIDEBAR_RAIL_WIDTH, SIDEBAR_WIDTH } from "@/config";
import { t } from "@/i18n";
import { SpaceNav } from "@/features/spaces/SpaceNav";
import { NavItem } from "./nav";
import { Rail } from "./Rail";
import { SidebarGroup } from "./SidebarGroup";
import { DRAWER_ID, SIDEBAR_ID, useSidebarGroups, useSidebarMode } from "./state";
import { ThemeButton } from "./ThemeButton";

// The sidebar is the map: where you are is highlighted and where you can go is
// listed in groups that fold. The rail beside it stays when the map is folded.
// On a narrow screen there is no room for either column, so the map becomes a
// drawer the top bar opens, and the rail's theme switch moves into it.
export function Sidebar({ narrow, drawerOpen, onCloseDrawer }: { narrow: boolean; drawerOpen: boolean; onCloseDrawer: () => void }) {
  const [mode, toggleMode] = useSidebarMode();
  const open = mode === "open";

  if (narrow) {
    return drawerOpen ? (
      <SidebarDrawer onClose={onCloseDrawer}>
        <SidebarGroups onNavigate={onCloseDrawer} />
      </SidebarDrawer>
    ) : null;
  }

  return (
    <>
      <Rail open={open} onToggle={toggleMode} />
      {open && (
        <nav
          id={SIDEBAR_ID}
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
            <SidebarGroups />
          </div>

          <div className="truncate px-3 pb-2 text-2xs text-ink-subtle" data-build>
            {APP_VERSION}
          </div>
        </nav>
      )}
    </>
  );
}

function SidebarGroups({ onNavigate }: { onNavigate?: () => void }) {
  const [groups, toggleGroup] = useSidebarGroups();
  const isOpen = (id: string, defaultOpen: boolean) => groups[id] ?? defaultOpen;
  return (
    <>
      <SidebarGroup id="wiki" title={t.nav.groupWiki} open={isOpen("wiki", true)} onToggle={() => toggleGroup("wiki", true)}>
        <NavItem to="/" exact icon="Home" rail={false} onNavigate={onNavigate}>
          {t.nav.home}
        </NavItem>
        <NavItem to="/spaces" icon="Space" rail={false} onNavigate={onNavigate}>
          {t.nav.spaces}
        </NavItem>
        <NavItem to="/tasks" icon="Checklist" rail={false} onNavigate={onNavigate}>
          {t.nav.tasks}
        </NavItem>
        <NavItem
          to="/search"
          icon="Search"
          rail={false}
          onNavigate={onNavigate}
          // On the current item the hint takes the accent: subtle ink is too
          // faint on the accent tint in the dark palette.
          trailing={<kbd className="font-mono text-2xs text-ink-subtle in-data-[status=active]:text-accent">{t.search.shortcut}</kbd>}
        >
          {t.nav.search}
        </NavItem>
      </SidebarGroup>
      <SpaceNav open={isOpen("space", true)} onToggle={() => toggleGroup("space", true)} onNavigate={onNavigate} />
    </>
  );
}

// A modal panel from the left: the page stays in view behind the scrim but out
// of reach, focus is held inside, and it hands focus back to the menu button.
function SidebarDrawer({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useEscape(true, onClose);
  useFocusReturn(true, ref, true);
  // The scrim closes on the click, not on the press: closing on the press
  // would let the rest of the click land on the page and take the focus
  // that was just handed back to the menu button.
  return createPortal(
    // biome-ignore lint/a11y: the scrim is for the pointer; the keyboard closes the drawer with Escape or its close button
    <div className="fixed inset-0 z-50 flex bg-ink/30" onClick={(e) => e.target === e.currentTarget && onClose()} data-drawer-backdrop>
      <div
        ref={ref}
        id={DRAWER_ID}
        role="dialog"
        aria-modal="true"
        aria-label={t.nav.drawer}
        // A strip of the page as wide as the rail stays uncovered, so there
        // is always somewhere to tap the drawer shut.
        style={{ width: `min(${SIDEBAR_WIDTH}px, calc(100vw - ${SIDEBAR_RAIL_WIDTH}px))` }}
        className="flex h-full flex-col border-r border-border bg-surface shadow-2 motion-safe:animate-drawer-in"
        data-sidebar="drawer"
      >
        <div className="flex h-12 items-center gap-2 border-b border-border px-3">
          <span className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">{APP_NAME}</span>
          <IconButton icon={<Icon.X />} label={t.nav.closeDrawer} size="sm" onClick={onClose} data-focus-last="" data-action="close-drawer" />
        </div>
        <nav aria-label={t.nav.whereYouAre} className="flex-1 overflow-y-auto px-2 py-3">
          {children}
        </nav>
        <div className="flex items-center gap-2 border-t border-border px-2 py-2">
          <ThemeButton side="top" />
          <span className="min-w-0 flex-1 truncate text-right text-2xs text-ink-subtle" data-build>
            {APP_VERSION}
          </span>
        </div>
      </div>
    </div>,
    document.body,
  );
}
