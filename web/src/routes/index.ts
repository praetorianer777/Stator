import { createElement } from "react";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { Skeleton } from "@/components/ui";
import { devEditorRoute } from "./dev-editor";
import { homeRoute, searchRoute, spacesRoute } from "./pages";
import { RouteError, rootRoute } from "./root";
import { themeEditRoute, themeNewRoute } from "./theme-editor";
import { themesRoute } from "./themes";

const routeTree = rootRoute.addChildren([homeRoute, spacesRoute, searchRoute, themesRoute, themeNewRoute, themeEditRoute, devEditorRoute]);

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

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
