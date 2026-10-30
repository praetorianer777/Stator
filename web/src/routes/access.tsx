import { useId } from "react";
import { createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { EmptyState, Page, PageHeader, TabPanel, Tabs } from "@/components/ui";
import { Icon } from "@/components/icons";
import { GroupList } from "@/features/access/GroupList";
import { MemberList } from "@/features/access/MemberList";
import { RoleMatrix } from "@/features/access/RoleMatrix";
import { RoleTable } from "@/features/access/RoleTable";
import { ProviderSettings } from "@/features/auth/ProviderSettings";
import { SiteAnonymousAccess, SitePublicLinks } from "@/features/public/AnonymousAccess";
import { useCanAdministerSite } from "@/features/permissions/access";
import { ACCESS_PATH, SITE_PERMISSIONS_PATH, SSO_PATH } from "@/config";
import { t } from "@/i18n";
import { appRoute } from "./app";

const tabs = ["roles", "matrix", "members", "groups", "sso"] as const;

export type AccessTab = (typeof tabs)[number];

function isTab(value: string): value is AccessTab {
  return (tabs as readonly string[]).includes(value);
}

// The tests find a tab by these words, whatever language the page speaks.
const tabHooks: Record<AccessTab, string> = { roles: "Roles", matrix: "Matrix", members: "Members", groups: "Groups", sso: "Single sign-on" };

/** /settings/access opens on its first tab. */
export const accessRoute = createRoute({
  getParentRoute: () => appRoute,
  path: ACCESS_PATH,
  beforeLoad: () => {
    throw redirect({ to: "/settings/access/$tab", params: { tab: "roles" }, replace: true });
  },
});

/** Each tab is a route of its own, so a tab can be linked, survives a reload and steps back with Back. */
export const accessTabRoute = createRoute({
  getParentRoute: () => appRoute,
  path: `${ACCESS_PATH}/$tab`,
  beforeLoad: ({ params }) => {
    if (!isTab(params.tab)) throw redirect({ to: "/settings/access/$tab", params: { tab: "roles" }, replace: true });
  },
  component: AccessPage,
});

/** The single sign-on page of before is the Access page's last tab now. */
export const ssoRoute = createRoute({
  getParentRoute: () => appRoute,
  path: SSO_PATH,
  beforeLoad: () => {
    throw redirect({ to: "/settings/access/$tab", params: { tab: "sso" }, replace: true });
  },
});

/** The permissions of the whole organization of before are the roles granted across the installation now, beside who reads without signing in. */
export const sitePermissionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: SITE_PERMISSIONS_PATH,
  beforeLoad: () => {
    throw redirect({ to: "/settings/access/$tab", params: { tab: "roles" }, replace: true });
  },
});

function AccessPage() {
  const { tab: param } = accessTabRoute.useParams();
  const tab: AccessTab = isTab(param) ? param : "roles";
  const navigate = useNavigate();
  const panelId = useId();
  const administers = useCanAdministerSite();
  if (!administers) {
    return (
      <Page width="narrow" data-access-refused>
        <PageHeader title={t.accessAdmin.title} />
        <EmptyState icon={<Icon.Lock />} title={t.accessAdmin.title} description={t.accessAdmin.notAdmin} />
      </Page>
    );
  }
  return (
    <Page width="content" data-access-page={tab}>
      <PageHeader title={t.accessAdmin.title} />
      <p className="mb-3 text-sm text-ink-muted">{t.accessAdmin.intro}</p>
      <Tabs
        label={t.accessAdmin.title}
        value={tab}
        onChange={(next) => void navigate({ to: "/settings/access/$tab", params: { tab: next } })}
        panelId={panelId}
        className="mb-4"
        tabs={tabs.map((each) => ({ value: each, label: t.accessAdmin.tabs[each], attrs: { "data-access-tab": tabHooks[each] } }))}
      />
      <TabPanel id={panelId} label={t.accessAdmin.tabs[tab]}>
        {tab === "roles" && (
          <div className="space-y-6">
            <RoleTable />
            <SiteAnonymousAccess />
            <SitePublicLinks />
          </div>
        )}
        {tab === "matrix" && <RoleMatrix />}
        {tab === "members" && <MemberList />}
        {tab === "groups" && <GroupList />}
        {tab === "sso" && <ProviderSettings />}
      </TabPanel>
    </Page>
  );
}
