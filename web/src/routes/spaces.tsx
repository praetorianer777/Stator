import { createRoute } from "@tanstack/react-router";
import { NewSpaceForm } from "@/features/spaces/NewSpaceForm";
import { SpaceImportPage } from "@/features/spaces/SpaceImport";
import { SpaceDirectory } from "@/features/spaces/SpaceDirectory";
import { appRoute } from "./app";

export const spacesRoute = createRoute({ getParentRoute: () => appRoute, path: "/spaces", component: SpaceDirectory });
// A space's key is upper case and may be NEW or IMPORT, so these pages match
// only as written, and /spaces/NEW is that space.
export const spaceNewRoute = createRoute({ getParentRoute: () => appRoute, path: "/spaces/new", caseSensitive: true, component: NewSpaceForm });
export const spaceImportRoute = createRoute({ getParentRoute: () => appRoute, path: "/spaces/import", caseSensitive: true, component: SpaceImportPage });
export const personalSpaceNewRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/spaces/new/personal",
  caseSensitive: true,
  component: () => <NewSpaceForm personal />,
});
