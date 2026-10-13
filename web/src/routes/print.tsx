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
  path: "/print/public/p/$pageId",
  component: function PrintPublicPage() {
    const { pageId } = printPublicPageRoute.useParams();
    return <PrintPublicPageScreen pageId={pageId} />;
  },
});

export const printLinkRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/print/public/link/$token",
  component: function PrintLink() {
    const { token } = printLinkRoute.useParams();
    return <PrintLinkedPageScreen token={token} />;
  },
});
