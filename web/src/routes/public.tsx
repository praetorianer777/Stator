import { Outlet, createRoute } from "@tanstack/react-router";
import { PublicPageLookup, PublicPageScreen, PublicShell, PublicSearchScreen, PublicSiteScreen, PublicSpaceScreen } from "@/features/public/PublicScreens";
import { rootRoute } from "./root";

/**
 * What an organization lets anybody read, without signing in: outside the
 * app's shell and its sign-in guard, named by the organization's slug.
 */
export const publicRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/public/$org",
  component: function PublicLayout() {
    const { org } = publicRoute.useParams();
    return (
      <PublicShell org={org}>
        <Outlet />
      </PublicShell>
    );
  },
});

export const publicSiteRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/",
  component: function PublicSite() {
    const { org } = publicSiteRoute.useParams();
    return <PublicSiteScreen org={org} />;
  },
});

export const publicSpaceRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/s/$spaceKey",
  component: function PublicSpace() {
    const { org, spaceKey } = publicSpaceRoute.useParams();
    return <PublicSpaceScreen org={org} spaceKey={spaceKey} />;
  },
});

/** A page by its space, id and slug; only the id finds it. */
export const publicPageRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/s/$spaceKey/p/$pageId/$slug",
  component: function PublicPage() {
    const { org, pageId } = publicPageRoute.useParams();
    return <PublicPageScreen org={org} pageId={pageId} />;
  },
});

/** A page by its id alone, as an included page names it; its full address follows. */
export const publicPageBareRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/p/$pageId",
  component: function PublicPageBare() {
    const { org, pageId } = publicPageBareRoute.useParams();
    return <PublicPageLookup org={org} pageId={pageId} />;
  },
});

interface PublicSearch {
  q?: string;
  page?: number;
}

export const publicSearchRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/search",
  validateSearch: (search: Record<string, unknown>): PublicSearch => {
    const page = Number(search.page);
    const q = typeof search.q === "string" || typeof search.q === "number" ? String(search.q) : "";
    return { ...(q ? { q } : {}), ...(Number.isInteger(page) && page > 1 ? { page } : {}) };
  },
  component: function PublicSearchRoute() {
    const { org } = publicSearchRoute.useParams();
    const { q = "", page = 1 } = publicSearchRoute.useSearch();
    return <PublicSearchScreen org={org} q={q} page={page} />;
  },
});

export const publicRoutes = publicRoute.addChildren([publicSiteRoute, publicSpaceRoute, publicPageRoute, publicPageBareRoute, publicSearchRoute]);
