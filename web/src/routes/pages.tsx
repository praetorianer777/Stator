import { createRoute, redirect } from "@tanstack/react-router";
import { guestSpaceOf, meQuery } from "@/api/auth";
import { hubQuery } from "@/api/hub";
import { HomeScreen } from "@/features/home/HomeScreen";
import { pageSlug } from "@/lib/slug";
import { appRoute } from "./app";

export const homeRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/",
  // Where everybody lands on the hub, the start is the hub; a hub that does
  // not answer leaves the reader's own home, which needs nothing of it.
  beforeLoad: async ({ context }) => {
    // A guest's whole organization is their one space, so that is where they start.
    const own = guestSpaceOf(await context.queryClient.ensureQueryData(meQuery).catch(() => undefined));
    if (own) throw redirect({ to: "/s/$spaceKey", params: { spaceKey: own.key }, replace: true });
    const hub = await context.queryClient.ensureQueryData(hubQuery).catch(() => undefined);
    const page = hub?.landing ? hub.page : null;
    if (page) {
      throw redirect({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: page.spaceKey, pageId: page.id, slug: pageSlug(page.title) }, replace: true });
    }
  },
  component: HomeScreen,
});

/** The reader's own home, reachable when everybody lands on the hub. */
export const personalHomeRoute = createRoute({ getParentRoute: () => appRoute, path: "/home", component: HomeScreen });
