import type { ComponentType } from "react";
import { Outlet, createRoute, redirect, useLocation } from "@tanstack/react-router";
import {
  ExportSection,
  GeneralSection,
  IntegrationsSection,
  MembersSection,
  SpaceSettingsLayout,
  TemplatesSection,
  TrashSection,
  WorkflowSection,
} from "@/features/spaces/SpaceSettings";
import { isSection, settingsAddress } from "@/features/spaces/settingsSections";
import { spaceRoute } from "./space";

/** A space's settings, one route per section below it, so a section can be linked and survives a reload. */
export const spaceSettingsRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/settings",
  component: function SpaceSettingsRoute() {
    const { spaceKey } = spaceSettingsRoute.useParams();
    const section = useLocation({ select: (location) => location.pathname.split("/")[4] ?? "" });
    return (
      <SpaceSettingsLayout spaceKey={spaceKey} section={isSection(section) ? section : "general"}>
        <Outlet />
      </SpaceSettingsLayout>
    );
  },
});

/** The settings themselves open on General, or on the section an old ?tab= named. */
export const spaceSettingsIndexRoute = createRoute({
  getParentRoute: () => spaceSettingsRoute,
  path: "/",
  beforeLoad: ({ params, location }) => {
    throw redirect({ href: settingsAddress(params.spaceKey, (location.search as Record<string, unknown>).tab, location.hash), replace: true });
  },
});

function show(Section: ComponentType<{ spaceKey: string }>) {
  return function SpaceSettingsSectionRoute() {
    const { spaceKey } = spaceSettingsRoute.useParams();
    return <Section spaceKey={spaceKey} />;
  };
}

const parent = () => spaceSettingsRoute;

export const spaceSettingsGeneralRoute = createRoute({ getParentRoute: parent, path: "/general", component: show(GeneralSection) });
export const spaceSettingsWorkflowRoute = createRoute({ getParentRoute: parent, path: "/workflow", component: show(WorkflowSection) });
export const spaceSettingsTemplatesRoute = createRoute({ getParentRoute: parent, path: "/templates", component: show(TemplatesSection) });
export const spaceSettingsMembersRoute = createRoute({ getParentRoute: parent, path: "/members", component: show(MembersSection) });
export const spaceSettingsIntegrationsRoute = createRoute({ getParentRoute: parent, path: "/integrations", component: show(IntegrationsSection) });
export const spaceSettingsExportRoute = createRoute({ getParentRoute: parent, path: "/export", component: show(ExportSection) });
export const spaceSettingsTrashRoute = createRoute({ getParentRoute: parent, path: "/trash", component: show(TrashSection) });

export const spaceSettingsRoutes = spaceSettingsRoute.addChildren([
  spaceSettingsIndexRoute,
  spaceSettingsGeneralRoute,
  spaceSettingsWorkflowRoute,
  spaceSettingsTemplatesRoute,
  spaceSettingsMembersRoute,
  spaceSettingsIntegrationsRoute,
  spaceSettingsExportRoute,
  spaceSettingsTrashRoute,
]);
