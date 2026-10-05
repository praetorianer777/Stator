import { guestSpaceOf, useMe } from "@/api/auth";
import { useHub } from "@/api/hub";
import { NavItem } from "@/features/shell/nav";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";

/** Home, which is the hub when everybody lands there, so the reader's own home moves to /home. */
export function HomeNavItem({ rail, onNavigate }: { rail: boolean; onNavigate?: () => void }) {
  const { data: hub } = useHub();
  return (
    <NavItem to={hub?.landing && hub.page ? "/home" : "/"} exact icon="Home" rail={rail} onNavigate={onNavigate}>
      {t.nav.home}
    </NavItem>
  );
}

/** The organization's hub, for whoever may read it; a guest's organization is their one space. */
export function HubNavItem({ rail, onNavigate }: { rail: boolean; onNavigate?: () => void }) {
  const { data: hub } = useHub();
  const { data: me } = useMe();
  if (!hub?.page || guestSpaceOf(me)) return null;
  return (
    <NavItem to={`/s/${hub.page.spaceKey}/p/${hub.page.id}/${pageSlug(hub.page.title)}`} icon="Flag" rail={rail} onNavigate={onNavigate}>
      {t.nav.hub}
    </NavItem>
  );
}
