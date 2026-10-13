import { Outlet, createRoute, redirect } from "@tanstack/react-router";
import { namesTheSite } from "@/api/public";
import { PUBLIC_PATH } from "@/config";
import {
  NotPublic,
  PublicLinkScreen,
  PublicPageLookup,
  PublicPageScreen,
  PublicShell,
  PublicSearchScreen,
  PublicSiteScreen,
  PublicSpaceScreen,
} from "@/features/public/PublicScreens";
import { rootRoute } from "./root";

/** What the organization lets anybody read, without signing in: outside the app's shell and its sign-in guard. */
export const publicRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/public",
  component: function PublicLayout() {
    return (
      <PublicShell>
        <Outlet />
      </PublicShell>
    );
  },
});

export const publicSiteRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/",
  component: PublicSiteScreen,
});

export const publicSpaceRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/s/$spaceKey",
  component: function PublicSpace() {
    const { spaceKey } = publicSpaceRoute.useParams();
    return <PublicSpaceScreen spaceKey={spaceKey} />;
  },
});

/** A page by its space, id and slug; only the id finds it. */
export const publicPageRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/s/$spaceKey/p/$pageId/$slug",
  component: function PublicPage() {
    const { pageId } = publicPageRoute.useParams();
    return <PublicPageScreen pageId={pageId} />;
  },
});

/** A page by its id alone, as an included page names it; its full address follows. */
export const publicPageBareRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/p/$pageId",
  component: function PublicPageBare() {
    const { pageId } = publicPageBareRoute.useParams();
    return <PublicPageLookup pageId={pageId} />;
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
    const { q = "", page = 1 } = publicSearchRoute.useSearch();
    return <PublicSearchScreen q={q} page={page} />;
  },
});

/**
 * The one page a public link opens, outside the public shell: it reads
 * whether or not the organization opens any space to anybody.
 */
export const publicLinkRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/public/link/$token",
  component: function PublicLink() {
    const { token } = publicLinkRoute.useParams();
    return <PublicLinkScreen token={token} />;
  },
});

// An address shared while the public pages named the organization is sent on
// when the slug is this organization's, which only the API knows, and is not
// found otherwise. The fixed paths above win over it.
export const oldPublicRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/public/$org/$",
  beforeLoad: async ({ params, location }) => {
    if (await namesTheSite(params.org)) {
      const rest = params._splat ? `/${params._splat}` : "";
      throw redirect({ href: `${PUBLIC_PATH}${rest}${location.searchStr}${location.hash ? `#${location.hash}` : ""}`, replace: true });
    }
  },
  component: function OldPublicAddress() {
    return (
      <main className="mx-auto max-w-6xl px-4 py-6">
        <NotPublic next="/" />
      </main>
    );
  },
});

export const publicRoutes = publicRoute.addChildren([publicSiteRoute, publicSpaceRoute, publicPageRoute, publicPageBareRoute, publicSearchRoute]);
