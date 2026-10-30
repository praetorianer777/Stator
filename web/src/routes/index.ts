import { createElement } from "react";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { meQueryKey } from "@/api/auth";
import { Skeleton } from "@/components/ui";
import { LOGIN_PATH } from "@/config";
import { appRoute } from "./app";
import { armatureSettingsRoute } from "./armature";
import { exciterSettingsRoute } from "./exciter";
import { auditRoute } from "./audit";
import { devEditorRoute } from "./dev-editor";
import { exampleSpaceRoute } from "./example-space";
import { labelRoute, spaceLabelRoute } from "./labels";
import { loginRoute } from "./login";
import { notificationSettingsRoute, notificationsRoute, watchingRoute } from "./notifications";
import { homeRoute, personalHomeRoute } from "./pages";
import { siteTransferRoute } from "./site-transfer";
import { hubSettingsRoute } from "./hub";
import { pageEditRoute } from "./page-edit";
import { pageHistoryRoute } from "./page-history";
import { printLinkRoute, printPageRoute, printPublicPageRoute } from "./print";
import { profileRoute } from "./profile";
import { oldPublicRoute, publicLinkRoute, publicRoutes } from "./public";
import { movedSpaceRoute, pageBareRoute, pageRoute, spaceHomeRoute, spaceRoute } from "./space";
import { spaceSettingsRoutes } from "./space-settings";
import { spaceDecisionsRoute } from "./decisions";
import { spaceBlogRoute } from "./blog";
import { personalSpaceNewRoute, spaceImportRoute, spaceNewRoute, spacesRoute } from "./spaces";
import { RouteError, rootRoute } from "./root";
import { searchRoute } from "./search";
import { spaceStaleRoute, staleRoute } from "./stale";
import { reviewsRoute } from "./reviews";
import { tasksRoute } from "./tasks";
import { settingsRoute } from "./settings";
import { accessRoute, accessTabRoute } from "./access";
import { adminIndexRoute, adminRoute, settingsMovedDeepRoute, settingsMovedRoute } from "./admin";
import { connectedAppsRoute } from "./connected-apps";
import { installationRoute } from "./installation";
import { usersRoute } from "./users";
import { themeEditRoute, themeNewRoute } from "./theme-editor";
import { themesRoute } from "./themes";
import { tokensRoute } from "./tokens";
import { oauthAuthorizeRoute } from "./oauth-authorize";
import { webhooksRoute } from "./webhooks";
import { spaceTemplateEditRoute, spaceTemplateNewRoute, templateEditRoute, templateNewRoute, templatesRoute } from "./templates";

const routeTree = rootRoute.addChildren([
  loginRoute,
  publicRoutes,
  publicLinkRoute,
  oldPublicRoute,
  movedSpaceRoute,
  printPageRoute,
  printPublicPageRoute,
  printLinkRoute,
  appRoute.addChildren([
    homeRoute,
    personalHomeRoute,
    spacesRoute,
    spaceNewRoute,
    personalSpaceNewRoute,
    spaceImportRoute,
    spaceRoute.addChildren([
      spaceHomeRoute,
      pageRoute,
      pageBareRoute,
      pageEditRoute,
      pageHistoryRoute,
      spaceSettingsRoutes,
      spaceLabelRoute,
      spaceDecisionsRoute,
      spaceBlogRoute,
      spaceStaleRoute,
      spaceTemplateNewRoute,
      spaceTemplateEditRoute,
    ]),
    searchRoute,
    tasksRoute,
    reviewsRoute,
    labelRoute,
    themesRoute,
    themeNewRoute,
    themeEditRoute,
    tokensRoute,
    oauthAuthorizeRoute,
    settingsRoute,
    profileRoute,
    notificationsRoute,
    notificationSettingsRoute,
    watchingRoute,
    adminRoute.addChildren([
      adminIndexRoute,
      installationRoute,
      accessRoute,
      accessTabRoute,
      usersRoute,
      hubSettingsRoute,
      templatesRoute,
      templateNewRoute,
      templateEditRoute,
      staleRoute,
      exampleSpaceRoute,
      armatureSettingsRoute,
      exciterSettingsRoute,
      connectedAppsRoute,
      webhooksRoute,
      auditRoute,
      siteTransferRoute,
    ]),
    settingsMovedRoute,
    settingsMovedDeepRoute,
    devEditorRoute,
  ]),
]);

/** The application's router; tests pass a memory history to start anywhere. */
export function buildRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    history,
    context: { queryClient },
    defaultPreload: "intent",
    defaultErrorComponent: RouteError,
    defaultPendingComponent: () => createElement(Skeleton),
  });
}

export type AppRouter = ReturnType<typeof buildRouter>;

/** The session is gone: forget who it was, and sign in again to come back here. */
export function sendToLogin(router: AppRouter, client: QueryClient) {
  const here = router.state.location;
  if (here.pathname === LOGIN_PATH) return;
  client.removeQueries({ queryKey: meQueryKey });
  void router.navigate({ to: "/login", search: { next: here.href } });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
