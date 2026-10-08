import { useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { Button, ErrorBanner, Spinner } from "@/components/ui";
import { useFocusReturn } from "@/components/ui/overlay";
import { pageIsDark } from "@/features/editor/DiagramViews";
import { language, t } from "@/i18n";
import { drawScene, loadExcalidraw, type ExcalidrawModule } from "./excalidraw";
import { keepScene, readScene, type SketchElement } from "./scene";

/**
 * Excalidraw over the whole window, for one sketch. Done keeps the scene and
 * the drawing made from it; Cancel, or Escape outside the canvas, leaves the
 * sketch as it was, asking first when something was changed.
 */
export function SketchDialog({ scene, onDone, onClose }: { scene: string; onDone: (scene: string, drawing: string | null) => void; onClose: () => void }) {
  const s = t.editor.sketch;
  const [lib, setLib] = useState<ExcalidrawModule | null>(null);
  const [failed, setFailed] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const api = useRef<ExcalidrawImperativeAPI | null>(null);
  const firstVersion = useRef<number | null>(null);
  const dialog = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const hintId = useId();
  const initial = useMemo(() => readScene(scene), [scene]);
  useFocusReturn(true, dialog, true);

  useEffect(() => {
    let live = true;
    loadExcalidraw().then(
      (module) => live && setLib(module),
      () => live && setFailed(true),
    );
    return () => {
      live = false;
    };
  }, []);

  const changed = useCallback(() => {
    const now = api.current;
    if (!now || !lib || firstVersion.current === null) return false;
    return lib.getSceneVersion(now.getSceneElementsIncludingDeleted()) !== firstVersion.current;
  }, [lib]);

  const cancel = useCallback(() => {
    if (changed() && !window.confirm(s.confirmDiscard)) return;
    onClose();
  }, [changed, onClose, s.confirmDiscard]);

  async function done() {
    const now = api.current;
    if (!now) return;
    const elements = now.getSceneElements() as unknown as SketchElement[];
    const kept = keepScene(elements, now.getAppState() as unknown as Record<string, unknown>);
    if (kept.leftOut) {
      // Taken off the canvas, so what Done keeps next is what is shown.
      const keptIds = new Set(readScene(kept.scene)?.elements.map((element) => element.id));
      now.updateScene({ elements: now.getSceneElements().filter((element) => keptIds.has(element.id)) });
      setProblem(s.picturesLeftOut);
      return;
    }
    if (kept.tooLarge) {
      setProblem(s.tooLarge);
      return;
    }
    setSaving(true);
    const keptScene = readScene(kept.scene);
    // Readers draw a sketch saved without its drawing for themselves.
    const drawing = keptScene && keptScene.elements.length > 0 ? await drawScene(keptScene).catch(() => null) : null;
    onDone(kept.scene, drawing);
    onClose();
  }

  // Escape is Excalidraw's own inside the canvas, where it lets go of a selection.
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "Escape" || canvas.current?.contains(event.target as Node)) return;
    event.stopPropagation();
    cancel();
  }

  return createPortal(
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={hintId}
      className="fixed inset-0 z-50 flex flex-col bg-surface text-ink"
      onKeyDown={onKeyDown}
      data-sketch-dialog=""
    >
      <div className="flex flex-wrap items-center gap-2 border-b border-border bg-surface px-3 py-2">
        <div className="min-w-0 flex-1">
          <h2 id={titleId} className="truncate text-sm font-semibold">
            {s.dialog}
          </h2>
          <p id={hintId} className="hidden text-xs text-ink-muted sm:block">
            {s.canvasHint}
          </p>
        </div>
        <Button type="button" variant="ghost" size="sm" onClick={cancel} data-action="sketch-cancel">
          {s.cancel}
        </Button>
        <Button type="button" size="sm" onClick={() => void done()} disabled={!lib || saving} data-action="sketch-done">
          {saving && <Spinner />}
          {s.done}
        </Button>
      </div>
      {problem && (
        <div className="px-3 pt-2" role="alert">
          <ErrorBanner>{problem}</ErrorBanner>
        </div>
      )}
      <div className="relative min-h-0 flex-1">
        {failed ? (
          <div className="p-4">
            <ErrorBanner>{s.loadFailed}</ErrorBanner>
          </div>
        ) : !lib ? (
          <p className="flex h-full items-center justify-center gap-2 text-sm text-ink-muted" aria-busy="true">
            <Spinner />
            {s.loading}
          </p>
        ) : (
          <div ref={canvas} className="absolute inset-0" data-sketch-canvas="">
            <lib.Excalidraw
              initialData={{
                elements: (initial?.elements ?? []) as never,
                appState: { viewBackgroundColor: initial?.appState.viewBackgroundColor },
                scrollToContent: true,
              }}
              excalidrawAPI={(instance) => {
                api.current = instance;
              }}
              onChange={(elements) => {
                firstVersion.current ??= lib.getSceneVersion(elements);
              }}
              theme={pageIsDark() ? "dark" : "light"}
              langCode={language() === "de" ? "de-DE" : "en"}
              autoFocus
              validateEmbeddable={() => false}
              UIOptions={{
                canvasActions: { loadScene: false, saveToActiveFile: false, export: false, saveAsImage: false, toggleTheme: null },
                tools: { image: false },
              }}
            >
              <lib.MainMenu>
                <lib.MainMenu.DefaultItems.ClearCanvas />
                <lib.MainMenu.DefaultItems.ChangeCanvasBackground />
                <lib.MainMenu.DefaultItems.Help />
              </lib.MainMenu>
            </lib.Excalidraw>
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}
