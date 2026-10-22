import { useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useRouter } from "@tanstack/react-router";
import { administers, useJoinRequests, useMe } from "@/api/auth";
import { brandLogoHref, useBrand } from "@/api/brand";
import { IconButton } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { LogoMark } from "@/components/Logo";
import { APP_NAME, BRAND_LOGO_HEIGHT_PX, SIDEBAR_RAIL_WIDTH, SIDEBAR_WIDTH } from "@/config";
import { NotificationBell } from "@/features/notifications/NotificationBell";
import { WaitingReviewsCount } from "@/features/reviews/WaitingReviews";
import { SpaceNav } from "@/features/spaces/SpaceNav";
import { t } from "@/i18n";
import { AccountMenu } from "./AccountMenu";
import { BuildStamp } from "./BuildStamp";
import { NavItem } from "./nav";
import { usePlaces, type Place } from "./places";
import { Rail } from "./Rail";
import { settingsReturn } from "./settingsReturn";
import { SidebarGroup } from "./SidebarGroup";
import { DRAWER_ID, SIDEBAR_ID, useSidebarGroups, useSidebarMode } from "./state";
import { ThemeButton } from "./ThemeButton";

// The sidebar is the map: where you are is highlighted and where you can go is
// listed in groups that fold. The rail beside it stays when the map is folded.
// In the settings area the map is the settings sidebar instead. On a narrow
// screen there is no room for either column, so the map becomes a drawer the
// strip above the page opens, and the rail's bell, theme and avatar move into it.
export function Sidebar({
  narrow,
  drawerOpen,
  onCloseDrawer,
  settings,
}: {
  narrow: boolean;
  drawerOpen: boolean;
  onCloseDrawer: () => void;
  settings: boolean;
}) {
  const [mode, toggleMode] = useSidebarMode();
  const open = mode === "open";
  const groups = settings ? <SettingsGroups onNavigate={narrow ? onCloseDrawer : undefined} /> : <WikiGroups onNavigate={narrow ? onCloseDrawer : undefined} />;

  if (narrow) {
    return drawerOpen ? <SidebarDrawer onClose={onCloseDrawer}>{groups}</SidebarDrawer> : null;
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
          data-settings-sidebar={settings || undefined}
          data-print-hide
          aria-label={settings ? t.settings.navigation : t.nav.whereYouAre}
        >
          <div className="flex h-12 items-center gap-2 border-b border-border px-3">
            <SiteMark />
            <span className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">{APP_NAME}</span>
          </div>

          <div className="flex-1 overflow-y-auto px-2 py-3">{groups}</div>

          <BuildStamp className="block truncate px-3 pb-2 text-2xs text-ink-subtle" />
        </nav>
      )}
    </>
  );
}

function placeItem(place: Place, onNavigate: (() => void) | undefined, trailing?: ReactNode) {
  return (
    <NavItem
      key={place.id}
      id={place.id}
      to={place.to}
      hash={place.hash}
      matchHash={place.area === "you"}
      exact={place.exact}
      icon={place.icon}
      rail={false}
      onNavigate={onNavigate}
      trailing={trailing}
    >
      {place.label}
    </NavItem>
  );
}

function WikiGroups({ onNavigate }: { onNavigate?: () => void }) {
  const [groups, toggleGroup] = useSidebarGroups();
  const isOpen = (id: string, defaultOpen: boolean) => groups[id] ?? defaultOpen;
  const { data: me } = useMe();
  const trailing: Record<string, ReactNode> = {
    // On the current item the hint takes the accent: subtle ink is too faint
    // on the accent tint in the dark palette.
    search: <kbd className="font-mono text-2xs text-ink-subtle in-data-[status=active]:text-accent">{t.search.shortcut}</kbd>,
    reviews: me?.organization ? <WaitingReviewsCount /> : null,
  };
  const places = usePlaces().filter((place) => place.area === "wiki");
  return (
    <>
      <SidebarGroup id="wiki" title={t.nav.groupWiki} open={isOpen("wiki", true)} onToggle={() => toggleGroup("wiki", true)}>
        {places.map((place) => placeItem(place, onNavigate, trailing[place.id]))}
      </SidebarGroup>
      <SpaceNav
        open={isOpen("space", true)}
        onToggle={() => toggleGroup("space", true)}
        adminOpen={isOpen("space-admin", false)}
        onToggleAdmin={() => toggleGroup("space-admin", false)}
        onNavigate={onNavigate}
      />
    </>
  );
}

/** The settings area's own sidebar: the way back, the person's own pages, and the organization's settings for site administrators. */
function SettingsGroups({ onNavigate }: { onNavigate?: () => void }) {
  const [groups, toggleGroup] = useSidebarGroups();
  const isOpen = (id: string) => groups[id] ?? true;
  const places = usePlaces();
  const you = places.filter((place) => place.area === "you");
  const settings = places.filter((place) => place.area === "settings");
  const trailing: Record<string, ReactNode> = { "sso-settings": <JoinRequestsBadge /> };
  const router = useRouter();
  const back = settingsReturn();
  return (
    <>
      <a
        href={back}
        onClick={(event) => {
          // The address is a whole href, search and hash included, which the
          // history takes as it is; the router's links take a route.
          event.preventDefault();
          onNavigate?.();
          router.history.push(back);
        }}
        className="mb-3 flex h-8 items-center gap-2 rounded-control px-2 text-sm text-ink-muted no-underline hover:bg-surface-raised hover:text-ink"
        data-action="leave-settings"
      >
        <Icon.ChevronLeft className="shrink-0" />
        <span className="min-w-0 flex-1 truncate">{t.settings.backTo}</span>
      </a>
      <SidebarGroup id="settings-you" title={t.settings.you} open={isOpen("settings-you")} onToggle={() => toggleGroup("settings-you", true)}>
        {you.map((place) => placeItem(place, onNavigate))}
      </SidebarGroup>
      {settings.length > 0 && (
        <SidebarGroup id="settings-site" title={t.settings.site} open={isOpen("settings-site")} onToggle={() => toggleGroup("settings-site", true)}>
          {settings.map((place) => placeItem(place, onNavigate, trailing[place.id]))}
        </SidebarGroup>
      )}
    </>
  );
}

/** How many people wait to be let in, beside Single sign-on; asked only of those the list answers. */
function JoinRequestsBadge() {
  const { data: me } = useMe();
  const waiting = useJoinRequests(administers(me?.organization?.role)).data?.length ?? 0;
  if (waiting === 0) return null;
  return (
    <span className="rounded-full bg-accent px-1.5 text-2xs font-medium text-on-primary" data-join-badge>
      <span aria-hidden="true">{waiting}</span>
      <span className="sr-only">{t.sso.waitingCount(waiting)}</span>
    </span>
  );
}

// A modal panel from the left: the page stays in view behind the scrim but out
// of reach, focus is held inside, and it hands focus back to the menu button.
function SidebarDrawer({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const { data: me } = useMe();
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
        <div className="flex items-center gap-1 border-t border-border px-2 py-2" data-drawer-foot>
          <AccountMenu side="top" />
          {me?.organization && <NotificationBell side="top" />}
          <ThemeButton side="top" />
          <BuildStamp className="min-w-0 flex-1 truncate text-right text-2xs text-ink-subtle" />
        </div>
      </div>
    </div>,
    document.body,
  );
}

/** The organization's own logo where it has one, else the mark. */
function SiteMark() {
  const { data: brand } = useBrand();
  const href = brand ? brandLogoHref(brand) : null;
  if (!href) return <LogoMark className="shrink-0 rounded-control" />;
  return (
    <img
      src={href}
      alt=""
      height={BRAND_LOGO_HEIGHT_PX}
      style={{ height: BRAND_LOGO_HEIGHT_PX }}
      className="max-w-24 shrink-0 rounded-control object-contain"
      data-site-logo
    />
  );
}
