import { createRoute } from "@tanstack/react-router";
import { NOTIFICATIONS_PATH } from "@/config";
import { NotificationSettings } from "@/features/notifications/NotificationSettings";
import { NotificationsPage } from "@/features/notifications/NotificationsPage";
import { WatchingList } from "@/features/watching/WatchingList";
import { appRoute } from "./app";

/** Everything the caller was told, the bell's panel at full length. */
export const notificationsRoute = createRoute({ getParentRoute: () => appRoute, path: NOTIFICATIONS_PATH, component: NotificationsPage });

/** Where the caller chooses what they hear about; every notification mail links here. */
export const notificationSettingsRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings/notifications", component: NotificationSettings });

/** The pages and spaces the caller watches. */
export const watchingRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings/watching", component: WatchingList });
