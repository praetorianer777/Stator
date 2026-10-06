import { createRoute } from "@tanstack/react-router";
import { ExampleSpaceSettings } from "@/features/spaces/ExampleSpace";
import { appRoute } from "./app";

export const exampleSpaceRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings/example-space", component: ExampleSpaceSettings });
