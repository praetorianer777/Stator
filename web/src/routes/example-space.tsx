import { createRoute } from "@tanstack/react-router";
import { ExampleSpaceSettings } from "@/features/spaces/ExampleSpace";
import { adminRoute } from "./admin";

export const exampleSpaceRoute = createRoute({ getParentRoute: () => adminRoute, path: "/example-space", component: ExampleSpaceSettings });
