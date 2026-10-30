import { createRoute } from "@tanstack/react-router";
import { HubSettings } from "@/features/hub/HubSettings";
import { adminRoute } from "./admin";

export const hubSettingsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/hub", component: HubSettings });
