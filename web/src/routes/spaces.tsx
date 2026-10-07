import { createRoute } from "@tanstack/react-router";
import { NewSpaceForm } from "@/features/spaces/NewSpaceForm";
import { SpaceImportPage } from "@/features/spaces/SpaceImport";
import { SpaceDirectory } from "@/features/spaces/SpaceDirectory";
import { appRoute } from "./app";

export const spacesRoute = createRoute({ getParentRoute: () => appRoute, path: "/spaces", component: SpaceDirectory });
export const spaceNewRoute = createRoute({ getParentRoute: () => appRoute, path: "/spaces/new", component: NewSpaceForm });
export const spaceImportRoute = createRoute({ getParentRoute: () => appRoute, path: "/spaces/import", component: SpaceImportPage });
export const personalSpaceNewRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/spaces/new/personal",
  component: () => <NewSpaceForm personal />,
});
