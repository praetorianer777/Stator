import { useMe } from "@/api/auth";
import { IconButton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SIDEBAR_RAIL_WIDTH } from "@/config";
import { NotificationBell } from "@/features/notifications/NotificationBell";
import { t } from "@/i18n";
import { AccountMenu } from "./AccountMenu";
import { NavItem } from "./nav";
import { usePlaces } from "./places";
import { SIDEBAR_ID } from "./state";
import { ThemeButton } from "./ThemeButton";

// The rail's own order, which puts the bell among the places, as all three tools do.
const BEFORE_BELL = ["home", "hub", "search"];
const AFTER_BELL = ["spaces", "tasks"];

/**
 * The rail is what is always there: the places every page reaches from, the
 * bell, and the theme and the avatar at the bottom. When the sidebar is
 * folded away, it is the whole chrome.
 */
export function Rail({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  const { data: me } = useMe();
  const places = usePlaces().filter((place) => place.area === "wiki");
  const items = (ids: string[]) =>
    ids.flatMap((id) => {
      const place = places.find((each) => each.id === id);
      return place
        ? [
            <NavItem key={id} id={id} to={place.to} exact={place.exact} icon={place.icon} rail>
              {place.label}
            </NavItem>,
          ]
        : [];
    });
  return (
    <aside className="flex shrink-0 flex-col items-center border-r border-border bg-surface" style={{ width: SIDEBAR_RAIL_WIDTH }} data-rail data-print-hide>
      {/* The toggle opens and closes the navigation, so it sits inside it, not in a banner of its own. */}
      <nav aria-label={t.nav.everywhere} className="flex w-full flex-1 flex-col items-center">
        <div className="flex h-12 w-full items-center justify-center border-b border-border" data-rail-top>
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
        <div className="flex flex-1 flex-col items-center gap-1 py-3">
          {items(BEFORE_BELL)}
          {me?.organization && <NotificationBell side="right" />}
          {items(AFTER_BELL)}
        </div>
      </nav>
      {/* As wide as the rail, unlike its centred controls, so the account menu's anchor reaches the rail's edge. */}
      <div className="flex w-full flex-col items-center gap-1 border-t border-border py-2" data-rail-foot>
        <ThemeButton />
        <AccountMenu />
      </div>
    </aside>
  );
}
