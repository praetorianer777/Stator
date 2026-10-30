import { useId } from "react";
import { createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { Page, PageHeader, TabPanel, Tabs } from "@/components/ui";
import { GroupList } from "@/features/access/GroupList";
import { MemberList } from "@/features/access/MemberList";
import { RoleMatrix } from "@/features/access/RoleMatrix";
import { RoleTable } from "@/features/access/RoleTable";
import { ProviderSettings } from "@/features/auth/ProviderSettings";
import { SiteAnonymousAccess, SitePublicLinks } from "@/features/public/AnonymousAccess";
import { t } from "@/i18n";
import { adminRoute } from "./admin";

const tabs = ["roles", "matrix", "members", "groups", "sso"] as const;

export type AccessTab = (typeof tabs)[number];

function isTab(value: string): value is AccessTab {
  return (tabs as readonly string[]).includes(value);
}

// The tests find a tab by these words, whatever language the page speaks.
const tabHooks: Record<AccessTab, string> = { roles: "Roles", matrix: "Matrix", members: "Members", groups: "Groups", sso: "Single sign-on" };

/** /admin/access opens on its first tab. */
export const accessRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/access",
  beforeLoad: () => {
    throw redirect({ to: "/admin/access/$tab", params: { tab: "roles" }, replace: true });
  },
});

/** Each tab is a route of its own, so a tab can be linked, survives a reload and steps back with Back. */
export const accessTabRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/access/$tab",
  beforeLoad: ({ params }) => {
    if (!isTab(params.tab)) throw redirect({ to: "/admin/access/$tab", params: { tab: "roles" }, replace: true });
  },
  component: AccessPage,
});

function AccessPage() {
  const { tab: param } = accessTabRoute.useParams();
  const tab: AccessTab = isTab(param) ? param : "roles";
  const navigate = useNavigate();
  const panelId = useId();
  return (
    <Page width="content" data-access-page={tab}>
      <PageHeader title={t.accessAdmin.title} />
      <p className="mb-3 text-sm text-ink-muted">{t.accessAdmin.intro}</p>
      <Tabs
        label={t.accessAdmin.title}
        value={tab}
        onChange={(next) => void navigate({ to: "/admin/access/$tab", params: { tab: next } })}
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
