import { createRoute } from "@tanstack/react-router";
import { Webhooks } from "@/features/webhooks/Webhooks";
import { adminRoute } from "./admin";

/** Where the installation's events are posted, signed, with a log of every attempt, for its administrators. */
export const webhooksRoute = createRoute({ getParentRoute: () => adminRoute, path: "/webhooks", component: Webhooks });
