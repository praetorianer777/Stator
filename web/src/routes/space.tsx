import { Outlet, createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { pageQuery } from "@/api/pages";
import { spaceQuery } from "@/api/spaces";
import { PageScreen } from "@/features/pages/PageScreen";
import { SpaceSettings, type SettingsTab } from "@/features/spaces/SpaceSettings";
import { pageSlug } from "@/lib/slug";
import { appRoute } from "./app";

/** Everything in a space hangs off its key, which is loaded first so a wrong key fails once, in a sentence. */
export const spaceRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/s/$spaceKey",
  beforeLoad: ({ params, location }) => {
    const key = params.spaceKey.toUpperCase();
    if (key !== params.spaceKey) {
      throw redirect({ href: location.href.replace(`/s/${params.spaceKey}`, `/s/${key}`), replace: true });
    }
  },
  loader: ({ context, params }) => context.queryClient.ensureQueryData(spaceQuery(params.spaceKey)),
  component: Outlet,
});

/** The space's own address shows its home page. */
export const spaceHomeRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/",
  loader: async ({ context, params }) => {
    const space = await context.queryClient.ensureQueryData(spaceQuery(params.spaceKey));
    await context.queryClient.ensureQueryData(pageQuery(space.homePageId));
    return space;
  },
  component: function SpaceHome() {
    const space = spaceHomeRoute.useLoaderData();
    return <PageScreen pageId={space.homePageId} />;
  },
});

/** A page by its id; the slug is for people, and a stale one is put right. */
export const pageRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/p/$pageId/$slug",
  loader: async ({ context, params }) => {
    const { page, space } = await context.queryClient.ensureQueryData(pageQuery(params.pageId));
    if (page.home) throw redirect({ to: "/s/$spaceKey", params: { spaceKey: space.key }, replace: true });
    const slug = pageSlug(page.title);
    if (params.slug !== slug || params.spaceKey !== space.key) {
      throw redirect({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: space.key, pageId: page.id, slug }, replace: true });
    }
  },
  component: function PageRoute() {
    const { pageId } = pageRoute.useParams();
    return <PageScreen pageId={pageId} />;
  },
});

const SETTINGS_TABS: SettingsTab[] = ["details", "permissions", "trash"];

export const spaceSettingsRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/settings",
  validateSearch: (search: Record<string, unknown>): { tab?: SettingsTab } =>
    SETTINGS_TABS.includes(search.tab as SettingsTab) && search.tab !== "details" ? { tab: search.tab as SettingsTab } : {},
  component: function SpaceSettingsRoute() {
    const { spaceKey } = spaceSettingsRoute.useParams();
    const { tab = "details" } = spaceSettingsRoute.useSearch();
    const navigate = useNavigate({ from: spaceSettingsRoute.fullPath });
    return <SpaceSettings spaceKey={spaceKey} tab={tab} onTab={(next) => navigate({ search: next === "details" ? {} : { tab: next }, replace: true })} />;
  },
});
