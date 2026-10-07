import { createRoute } from "@tanstack/react-router";
import { PrintLinkedPageScreen, PrintPageScreen, PrintPublicPageScreen } from "@/features/print/PrintScreens";
import { rootRoute } from "./root";

// What the render service opens to print a page as PDF: outside the shell and
// its sign-in guard, since the service signs in with a token on each API call
// rather than a session, and a public print has nobody signed in at all.

export const printPageRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/print/p/$pageId",
  component: function PrintPage() {
    const { pageId } = printPageRoute.useParams();
    return <PrintPageScreen pageId={pageId} />;
  },
});

export const printPublicPageRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/print/public/$org/p/$pageId",
  component: function PrintPublicPage() {
    const { org, pageId } = printPublicPageRoute.useParams();
    return <PrintPublicPageScreen org={org} pageId={pageId} />;
  },
});

export const printLinkRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/print/public/$org/link/$token",
  component: function PrintLink() {
    const { org, token } = printLinkRoute.useParams();
    return <PrintLinkedPageScreen org={org} token={token} />;
  },
});
