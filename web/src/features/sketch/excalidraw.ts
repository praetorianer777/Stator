import type * as Excalidraw from "@excalidraw/excalidraw";
import { SKETCH_ASSET_PATH, SKETCH_PADDING_PX } from "@/config";
import { cleanDrawing } from "./drawing";
import type { SketchScene } from "./scene";

declare global {
  interface Window {
    /** Where Excalidraw fetches its fonts from; unset, it asks a public CDN. */
    EXCALIDRAW_ASSET_PATH?: string | string[];
  }
}

export type ExcalidrawModule = typeof Excalidraw;

let loaded: Promise<ExcalidrawModule> | null = null;

/**
 * Excalidraw, its styles and its fonts' address. It is large, so it loads
 * when a sketch is opened or has to be drawn, never with the application.
 */
export function loadExcalidraw(): Promise<ExcalidrawModule> {
  // Before the module runs, since it reads the path once it draws text.
  window.EXCALIDRAW_ASSET_PATH = SKETCH_ASSET_PATH;
  loaded ??= Promise.all([import("@excalidraw/excalidraw"), import("@excalidraw/excalidraw/index.css")])
    .then(([module]) => module)
    .catch((err: unknown) => {
      loaded = null;
      throw err;
    });
  return loaded;
}

/**
 * A scene drawn as the SVG a sketch keeps: on its background, its fonts
 * written into it, light, and cleaned to the API's rules. Null when the
 * drawing is too large to keep, which readers then draw for themselves.
 */
export async function drawScene(scene: SketchScene): Promise<string | null> {
  const { exportToSvg } = await loadExcalidraw();
  const svg: SVGSVGElement = await exportToSvg({
    elements: scene.elements as never,
    appState: { viewBackgroundColor: scene.appState.viewBackgroundColor, exportBackground: true, exportWithDarkMode: false },
    files: null,
    exportPadding: SKETCH_PADDING_PX,
  });
  return cleanDrawing(new XMLSerializer().serializeToString(svg));
}
