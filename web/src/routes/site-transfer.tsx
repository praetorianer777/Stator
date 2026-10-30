import { createRoute } from "@tanstack/react-router";
import { SiteTransferSettings } from "@/features/siteTransfer/SiteTransferSettings";
import { adminRoute } from "./admin";

/** The whole installation as one archive, and filling it from one. */
export const siteTransferRoute = createRoute({ getParentRoute: () => adminRoute, path: "/transfer", component: SiteTransferSettings });
