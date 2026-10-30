import { Link, Outlet, createRoute, notFound, redirect, useLocation, useNavigate } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { Button, Card, EmptyState, Menu, Page, PageHeader, SectionTitle, type MenuEntry } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ADMIN_PATH } from "@/config";
import { useCanAdministerSite } from "@/features/permissions/access";
import { NavItem } from "@/features/shell/nav";
import { ADMIN_SECTIONS, adminPlaces, installationName, type AdminSection, type Place } from "@/features/shell/places";
import { JoinRequestsCount } from "@/features/shell/Sidebar";
import { useNarrow } from "@/features/shell/state";
import { t } from "@/i18n";
import { appRoute } from "./app";

// Adapted from Armature's Admin page.

/** Everything that is the installation's, for its administrators; anybody else is refused here once, and no page below mounts. */
export const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  path: ADMIN_PATH,
  component: AdminLayout,
});

export const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/",
  component: AdminIndex,
});

/** The /settings pages that moved to /admin, by the name they had there and the address below /admin they have now. */
export const MOVED_SECTIONS: Record<string, string> = {
  access: "access",
  sso: "access/sso",
  permissions: "access/roles",
  brand: "installation",
  hub: "hub",
  templates: "templates",
  stale: "stale",
  "example-space": "example-space",
  armature: "armature",
  exciter: "exciter",
  webhooks: "webhooks",
  audit: "audit",
  transfer: "transfer",
};

/** Where an old /settings address lives now, or undefined for one that never moved. A space's stale pages and new template went to the space's settings. */
export function movedAddress(section: string, rest = "", search: Record<string, unknown> = {}, searchStr = "", hash = ""): string | undefined {
  const moved = Object.hasOwn(MOVED_SECTIONS, section) ? MOVED_SECTIONS[section] : undefined;
  if (!moved) return undefined;
  const after = hash ? `#${hash}` : "";
  const space = typeof search.space === "string" && search.space ? encodeURIComponent(search.space.toUpperCase()) : "";
  if (space && section === "stale" && !rest) return `/spaces/${space}/settings/general/stale${after}`;
  if (space && section === "templates" && rest === "new") return `/spaces/${space}/settings/templates/new${after}`;
  return `${ADMIN_PATH}/${moved}${rest ? `/${rest}` : ""}${searchStr}${after}`;
}

function sendOn(section: string, rest: string | undefined, location: { search: unknown; searchStr: string; hash: string }): never {
  const to = movedAddress(section, rest, (location.search ?? {}) as Record<string, unknown>, location.searchStr, location.hash);
  if (!to) throw notFound();
  throw redirect({ href: to, replace: true });
}

/** An old /settings/<moved> address, sent on with its search and hash; any other unknown one is not found. */
export const settingsMovedRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/$section",
  beforeLoad: ({ params, location }) => sendOn(params.section, undefined, location),
});

export const settingsMovedDeepRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/$section/$",
  beforeLoad: ({ params, location }) => sendOn(params.section, params._splat, location),
});

function bySection(): Array<{ section: AdminSection; places: Place[] }> {
  const all = adminPlaces();
  return ADMIN_SECTIONS.map((section) => ({ section, places: all.filter((place) => place.section === section) }));
}

function AdminLayout() {
  const administers = useCanAdministerSite();
  const narrow = useNarrow();
  if (!administers) {
    return (
      <Page width="narrow" data-admin-refused>
        <EmptyState title={t.admin.refusedTitle} description={t.admin.refusedBody} icon={<Icon.Shield />} />
      </Page>
    );
  }
  return (
    <div className={narrow ? "flex flex-col" : "flex items-start gap-6"} data-admin-area>
      {narrow ? <AdminMenu /> : <AdminNav />}
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}

/** The sections and their pages as a list beside the page, for wide screens. */
function AdminNav() {
  return (
    <nav aria-label={t.admin.navLabel} className="sticky top-4 w-56 shrink-0 py-2" data-admin-nav>
      <NavItem to={ADMIN_PATH} exact id="admin" icon="Settings" rail={false}>
        {t.admin.allSections}
      </NavItem>
      {bySection().map(({ section, places }) => (
        <div key={section} className="mt-3" data-admin-nav-section={section}>
          <SectionTitle className="mb-1 px-2">{t.admin.sections[section]}</SectionTitle>
          {places.map((place) => (
            <NavItem
              key={place.id}
              id={place.id}
              to={place.to}
              icon={place.icon}
              rail={false}
              trailing={place.id === "users" ? <JoinRequestsCount /> : undefined}
            >
              {place.label}
            </NavItem>
          ))}
        </div>
      ))}
    </nav>
  );
}

/** The same list behind one button, for narrow screens. */
function AdminMenu() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const current = adminPlaces().find((place) => pathname === place.to || pathname.startsWith(`${place.to}/`));
  const items: MenuEntry[] = [
    { label: t.admin.allSections, icon: <Icon.Settings />, onSelect: () => void navigate({ to: ADMIN_PATH }), attrs: { "data-nav": "admin" } },
    ...bySection().map(({ section, places }) => ({
      group: t.admin.sections[section],
      items: places.map((place) => {
        const Glyph = Icon[place.icon];
        return { label: place.label, icon: <Glyph />, onSelect: () => void navigate({ to: place.to }), attrs: { "data-nav": place.id } };
      }),
    })),
  ];
  return (
    <div className="mb-2" data-admin-menu>
      <Menu
        label={t.admin.menuLabel}
        items={items}
        trigger={(props) => (
          <Button
            variant="secondary"
            size="sm"
            iconRight={<Icon.ChevronDown />}
            onClick={props.toggle}
            aria-haspopup={props["aria-haspopup"]}
            aria-expanded={props["aria-expanded"]}
            aria-controls={props["aria-controls"]}
            data-action="admin-sections"
          >
            {current ? current.label : t.admin.title}
          </Button>
        )}
      />
    </div>
  );
}

/** Every page of the Admin area with what it is for, grouped the way the navigation groups them. */
function AdminIndex() {
  const { data: me } = useMe();
  return (
    <Page width="narrow" data-admin-index>
      <PageHeader title={t.admin.title} meta={installationName(me)} />
      <p className="mb-6 text-sm text-ink-muted">{t.admin.intro}</p>
      {bySection().map(({ section, places }) => (
        <section key={section} className="mb-8" aria-labelledby={`admin-section-${section}`} data-admin-section={section}>
          <SectionTitle id={`admin-section-${section}`} className="mb-2">
            {t.admin.sections[section]}
          </SectionTitle>
          <Card className="divide-y divide-border">
            {places.map((place) => {
              const Glyph = Icon[place.icon];
              return (
                <Link
                  key={place.id}
                  to={place.to}
                  className="flex items-center gap-3 px-4 py-3 text-ink no-underline hover:bg-surface-raised"
                  data-admin-card={place.id}
                >
                  <Glyph className="shrink-0 text-ink-muted" />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-ink">{place.label}</span>
                    <span className="block text-sm text-ink-muted">{place.detail}</span>
                  </span>
                  <Icon.ChevronRight className="shrink-0 text-ink-subtle" />
                </Link>
              );
            })}
          </Card>
        </section>
      ))}
    </Page>
  );
}
