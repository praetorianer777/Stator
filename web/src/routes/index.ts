import { createElement } from "react";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { meQueryKey } from "@/api/auth";
import { Skeleton } from "@/components/ui";
import { LOGIN_PATH } from "@/config";
import { appRoute } from "./app";
import { armatureSettingsRoute } from "./armature";
import { auditRoute } from "./audit";
import { devEditorRoute } from "./dev-editor";
import { labelRoute, spaceLabelRoute } from "./labels";
import { loginRoute } from "./login";
import { notificationSettingsRoute, watchingRoute } from "./notifications";
import { homeRoute } from "./pages";
import { pageEditRoute } from "./page-edit";
import { orgPermissionsRoute } from "./permissions";
import { pageHistoryRoute } from "./page-history";
import { profileRoute } from "./profile";
import { pageBareRoute, pageRoute, spaceHomeRoute, spaceRoute, spaceSettingsRoute } from "./space";
import { spaceNewRoute, spacesRoute } from "./spaces";
import { RouteError, rootRoute } from "./root";
import { searchRoute } from "./search";
import { ssoRoute } from "./sso";
import { themeEditRoute, themeNewRoute } from "./theme-editor";
import { themesRoute } from "./themes";
import { tokensRoute } from "./tokens";

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([
    homeRoute,
    spacesRoute,
    spaceNewRoute,
    spaceRoute.addChildren([spaceHomeRoute, pageRoute, pageBareRoute, pageEditRoute, pageHistoryRoute, spaceSettingsRoute, spaceLabelRoute]),
    searchRoute,
    labelRoute,
    themesRoute,
    themeNewRoute,
    themeEditRoute,
    tokensRoute,
    profileRoute,
    notificationSettingsRoute,
    watchingRoute,
    ssoRoute,
    orgPermissionsRoute,
    armatureSettingsRoute,
    auditRoute,
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
