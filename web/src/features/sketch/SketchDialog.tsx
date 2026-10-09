import { useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import type * as Y from "yjs";
import type { Awareness } from "y-protocols/awareness";
import { Button, ErrorBanner, Spinner } from "@/components/ui";
import { useFocusReturn } from "@/components/ui/overlay";
import { pageIsDark } from "@/features/editor/DiagramViews";
import { SKETCH_POINTER_INTERVAL_MS } from "@/config";
import { language, t } from "@/i18n";
import { drawScene, loadExcalidraw, type ExcalidrawModule } from "./excalidraw";
import { SketchLink, keepShared, setSketchPresence, sharedIsAhead, sharedScene, sketchCollaborators, sketchMap, type SketchPresence } from "./live";
import { SketchDrawers, useSketchDrawers } from "./presence";
import { keepElement, keepScene, readScene, type SketchElement } from "./scene";

/** A sketch drawn together: the shared draft, which of its sketches this is, and the scene the page holds for it now. */
export interface LiveCanvas {
  doc: Y.Doc;
  awareness: Awareness;
  sketchId: string;
  saved: () => unknown;
}

/**
 * Excalidraw over the whole window, for one sketch. Done keeps the scene and
 * the drawing made from it; Cancel, or Escape outside the canvas, leaves the
 * sketch as it was, asking first when something was changed. Drawn
 * together, every change is shared as it is made, so there is nothing to
 * cancel: Done, or Escape outside the canvas, leaves it.
 */
export function SketchDialog({
  scene,
  live = null,
  onDone,
  onClose,
}: {
  scene: string;
  live?: LiveCanvas | null;
  onDone: (scene: string, drawing: string | null) => void;
  onClose: () => void;
}) {
  const s = t.editor.sketch;
  const [lib, setLib] = useState<ExcalidrawModule | null>(null);
  const [failed, setFailed] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const api = useRef<ExcalidrawImperativeAPI | null>(null);
  const [ready, setReady] = useState(false);
  const link = useRef<SketchLink | null>(null);
  // How the shared scene stood when this canvas joined it: whether it was
  // ahead of the page's already, and what it held once the canvas had sent
  // its own, which Excalidraw may have given new versions as it read them.
  const joinedAt = useRef<{ ahead: boolean; scene: string | null } | null>(null);
  const liveRef = useRef(live);
  liveRef.current = live;
  const doc = live?.doc ?? null;
  const awareness = live?.awareness ?? null;
  const sketchId = live?.sketchId ?? null;
  const drawers = useSketchDrawers(awareness, sketchId);
  const firstVersion = useRef<number | null>(null);
  const dialog = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const hintId = useId();
  // Drawn together, the canvas opens on what the others drew; the page's
  // copy may be behind it.
  const initial = useMemo(() => (doc && sketchId ? sharedScene(sketchMap(doc, sketchId)) : null) ?? readScene(scene), [doc, sketchId, scene]);
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

  useEffect(() => {
    const now = api.current;
    if (!doc || !sketchId || !lib || !ready || !now) return;
    const { CaptureUpdateAction, reconcileElements, restoreElements } = lib;
    const ahead = sharedIsAhead(sketchMap(doc, sketchId), liveRef.current?.saved());
    const joined = new SketchLink(doc, sketchId, {
      elements: () => now.getSceneElementsIncludingDeleted() as unknown as SketchElement[],
      background: () => now.getAppState().viewBackgroundColor,
      apply: (remote, background) => {
        const elements = reconcileElements(now.getSceneElementsIncludingDeleted(), restoreElements(remote as never, null) as never, now.getAppState());
        // Never, so this person's undo takes back only what they did.
        now.updateScene({
          elements,
          ...(background ? { appState: { viewBackgroundColor: background } } : {}),
          captureUpdate: CaptureUpdateAction.NEVER,
        });
      },
    });
    link.current = joined;
    joinedAt.current = { ahead, scene: keepShared(joined.map)?.scene ?? null };
    return () => {
      joined.push();
      joined.destroy();
      if (link.current === joined) link.current = null;
    };
  }, [doc, sketchId, lib, ready]);

  // Where this person is on the canvas goes to the others, a pointer at a
  // time rather than at every movement.
  const presence = useRef<SketchPresence | null>(null);
  const presenceTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const tell = useCallback(
    (change: Partial<SketchPresence>, now = false) => {
      if (!awareness || !sketchId) return;
      presence.current = { ...(presence.current ?? { id: sketchId, pointer: null, button: "up", selected: [] }), ...change, id: sketchId };
      if (now) {
        clearTimeout(presenceTimer.current);
        presenceTimer.current = undefined;
        setSketchPresence(awareness, presence.current);
        return;
      }
      presenceTimer.current ??= setTimeout(() => {
        presenceTimer.current = undefined;
        if (presence.current) setSketchPresence(awareness, presence.current);
      }, SKETCH_POINTER_INTERVAL_MS);
    },
    [awareness, sketchId],
  );
  useEffect(() => {
    if (!awareness || !sketchId) return;
    tell({}, true);
    return () => {
      clearTimeout(presenceTimer.current);
      presenceTimer.current = undefined;
      presence.current = null;
      setSketchPresence(awareness, null);
    };
  }, [awareness, sketchId, tell]);

  useEffect(() => {
    const now = api.current;
    if (!awareness || !sketchId || !ready || !now) return;
    const show = () =>
      now.updateScene({
        collaborators: sketchCollaborators(awareness.getStates() as Map<number, Record<string, unknown>>, awareness.clientID, sketchId) as never,
      });
    show();
    awareness.on("change", show);
    return () => awareness.off("change", show);
  }, [awareness, sketchId, ready]);

  const cancel = useCallback(() => {
    if (changed() && !window.confirm(s.confirmDiscard)) return;
    onClose();
  }, [changed, onClose, s.confirmDiscard]);

  async function leave() {
    const now = api.current;
    const joined = link.current;
    const current = liveRef.current;
    if (!now || !joined || !current) {
      onClose();
      return;
    }
    joined.push();
    const elements = now.getSceneElements() as unknown as SketchElement[];
    if (elements.some((element) => !keepElement(element))) {
      // Never shared, so they go from this canvas alone.
      now.updateScene({ elements: now.getSceneElements().filter((element) => keepElement(element as unknown as SketchElement)) });
      setProblem(s.picturesLeftOut);
      return;
    }
    const kept = keepShared(joined.map);
    if (kept?.tooLarge) {
      setProblem(s.tooLarge);
      return;
    }
    // Whoever leaves puts the drawing in the page, when the page's is behind
    // what is on the canvas; the last to leave leaves the last word. A canvas
    // opened and left with nothing drawn leaves the page as it was.
    const drawnOn = joinedAt.current?.ahead !== false || joinedAt.current.scene !== (kept?.scene ?? null);
    if (kept && drawnOn && sharedIsAhead(joined.map, current.saved())) {
      setSaving(true);
      const keptScene = readScene(kept.scene);
      const drawing = keptScene && keptScene.elements.length > 0 ? await drawScene(keptScene).catch(() => null) : null;
      onDone(kept.scene, drawing);
    }
    onClose();
  }

  async function done() {
    if (live) {
      await leave();
      return;
    }
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
    if (live) void leave();
    else cancel();
  }

  return createPortal(
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={hintId}
      data-sketch-live={live ? "" : undefined}
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
            {live ? s.liveHint : s.canvasHint}
          </p>
        </div>
        <SketchDrawers drawers={drawers} />
        {!live && (
          <Button type="button" variant="ghost" size="sm" onClick={cancel} data-action="sketch-cancel">
            {s.cancel}
          </Button>
        )}
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
              onChange={(elements, appState) => {
                // The first change is the scene read in, which Excalidraw does
                // after it hands out its API; the canvas joins the others then.
                if (firstVersion.current === null) {
                  firstVersion.current = lib.getSceneVersion(elements);
                  setReady(true);
                }
                link.current?.changed();
                if (live) {
                  const selected = Object.keys(appState.selectedElementIds).filter((id) => appState.selectedElementIds[id]);
                  if (selected.join() !== (presence.current?.selected ?? []).join()) tell({ selected });
                }
              }}
              isCollaborating={live !== null}
              onPointerUpdate={(update) => {
                if (live) tell({ pointer: { x: update.pointer.x, y: update.pointer.y, tool: update.pointer.tool }, button: update.button });
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
