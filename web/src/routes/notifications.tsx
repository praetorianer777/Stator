import { createRoute } from "@tanstack/react-router";
import { NotificationSettings } from "@/features/notifications/NotificationSettings";
import { WatchingList } from "@/features/watching/WatchingList";
import { appRoute } from "./app";

/** Where the caller chooses what they hear about; every notification mail links here. */
export const notificationSettingsRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings/notifications", component: NotificationSettings });

/** The pages and spaces the caller watches. */
export const watchingRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings/watching", component: WatchingList });
