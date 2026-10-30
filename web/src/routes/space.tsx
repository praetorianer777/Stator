import { Outlet, createRoute, redirect } from "@tanstack/react-router";
import { pageQuery } from "@/api/pages";
import { spaceQuery } from "@/api/spaces";
import { STALE_REVIEW_FROM } from "@/config";
import { PageScreen } from "@/features/pages/PageScreen";
import { movedSpaceAddress } from "@/features/spaces/settingsSections";
import { pageSlug } from "@/lib/slug";
import { appRoute } from "./app";
import { rootRoute } from "./root";

/** Everything in a space hangs off its key, which is loaded first so a wrong key fails once, in a sentence. */
export const spaceRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/spaces/$spaceKey",
  beforeLoad: ({ params, location }) => {
    const key = params.spaceKey.toUpperCase();
    if (key !== params.spaceKey) {
      throw redirect({ href: location.href.replace(`/spaces/${params.spaceKey}`, `/spaces/${key}`), replace: true });
    }
  },
  loader: ({ context, params }) => context.queryClient.ensureQueryData(spaceQuery(params.spaceKey)),
  component: Outlet,
});

/** A thread to bring into view below the page, as a notification links to it, and whether the stale report sent the reader. */
interface PageAddress {
  thread?: string;
  from?: typeof STALE_REVIEW_FROM;
}

function pageAddress(search: Record<string, unknown>): PageAddress {
  const out: PageAddress = {};
  if (typeof search.thread === "string" && search.thread !== "") out.thread = search.thread;
  if (search.from === STALE_REVIEW_FROM) out.from = STALE_REVIEW_FROM;
  return out;
}

/** The space's own address shows its home page. */
export const spaceHomeRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/",
  validateSearch: pageAddress,
  loader: async ({ context, params }) => {
    const space = await context.queryClient.ensureQueryData(spaceQuery(params.spaceKey));
    await context.queryClient.ensureQueryData(pageQuery(space.homePageId));
    return space;
  },
  component: function SpaceHome() {
    const space = spaceHomeRoute.useLoaderData();
    const { thread, from } = spaceHomeRoute.useSearch();
    return <PageScreen pageId={space.homePageId} thread={thread} reviewing={from === STALE_REVIEW_FROM} />;
  },
});

/** A page by its id; the slug is for people, and a stale one is put right. */
export const pageRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/p/$pageId/$slug",
  validateSearch: pageAddress,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, params, deps, location }) => {
    const { page, space } = await context.queryClient.ensureQueryData(pageQuery(params.pageId));
    if (page.home) throw redirect({ to: "/spaces/$spaceKey", params: { spaceKey: space.key }, search: deps, replace: true });
    const slug = pageSlug(page.title);
    if (params.slug !== slug || params.spaceKey !== space.key) {
      throw redirect({
        to: "/spaces/$spaceKey/p/$pageId/$slug",
        params: { spaceKey: space.key, pageId: page.id, slug },
        search: deps,
        hash: location.hash,
        replace: true,
      });
    }
  },
  component: function PageRoute() {
    const { pageId } = pageRoute.useParams();
    const { thread, from } = pageRoute.useSearch();
    return <PageScreen pageId={pageId} thread={thread} reviewing={from === STALE_REVIEW_FROM} />;
  },
});

/** A page by its id alone, as a mail links to it; the slug is put in. */
export const pageBareRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/p/$pageId",
  validateSearch: pageAddress,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, params, deps, location }) => {
    const { page, space } = await context.queryClient.ensureQueryData(pageQuery(params.pageId));
    if (page.home) throw redirect({ to: "/spaces/$spaceKey", params: { spaceKey: space.key }, search: deps, replace: true });
    throw redirect({
      to: "/spaces/$spaceKey/p/$pageId/$slug",
      params: { spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) },
      search: deps,
      hash: location.hash,
      replace: true,
    });
  },
});

/**
 * A space's address from before spaces moved to /spaces/, which mails, issues
 * and pages people keep still hold: sent on with its search and hash.
 */
export const movedSpaceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/s/$",
  beforeLoad: ({ params, location }) => {
    throw redirect({ href: movedSpaceAddress(params._splat ?? "", location.searchStr, location.hash), replace: true });
  },
});
