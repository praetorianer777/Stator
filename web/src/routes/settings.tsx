import { createRoute } from "@tanstack/react-router";
import { SettingsHome } from "@/features/settings/SettingsHome";
import { appRoute } from "./app";

export const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings", component: SettingsHome });
