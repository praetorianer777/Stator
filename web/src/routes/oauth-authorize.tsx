import { createRoute, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import { ConsentPage } from "@/features/oauth/ConsentPage";
import { appRoute } from "./app";

// Behind the sign-in like every page: a visitor goes there first with this
// whole address as next, and comes back to the same request.

export const oauthAuthorizeRoute = createRoute({ getParentRoute: () => appRoute, path: "/oauth/authorize", component: OAuthAuthorize });

function OAuthAuthorize() {
  const router = useRouter();
  // The app's query exactly as it sent it, read once: the router's own search
  // is parsed and written again, and the API reads the original.
  const [query] = useState(() => router.history.location.search.replace(/^\?/, ""));
  return <ConsentPage query={query} />;
}
