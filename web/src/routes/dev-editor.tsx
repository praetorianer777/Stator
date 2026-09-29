import { createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";

// Lazy like the page editor, so a reader's first load carries no editor.
export const devEditorRoute = createRoute({ getParentRoute: () => appRoute, path: "/dev/editor" }).lazy(() =>
  import("./dev-editor.lazy").then((module) => module.Route),
);
