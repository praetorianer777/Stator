import { useEffect, useMemo, useState } from "react";
import { SKETCH_FILE_NAME } from "@/config";
import { Lightbox } from "@/features/attachments/Lightbox";
import type { LightboxItem } from "@/features/attachments/lightboxItems";
import { t } from "@/i18n";
import { cleanDrawing, drawingSrc } from "./drawing";
import { hasDrawing, readScene, sketchTitle, type SketchScene } from "./scene";

// Kept apart from the editor's node, so a reader sees a sketch without the
// editor, and without Excalidraw whenever the sketch carries its drawing.

/** How far a sketch is from being shown. */
export type SketchState = { kind: "drawn"; svg: string } | { kind: "drawing" } | { kind: "empty" } | { kind: "unreadable" };

/**
 * A sketch's drawing: the one saved with it, cleaned again before it is
 * shown, or, for one nobody has drawn since it was imported, drawn here.
 */
export function useSketchDrawing(sceneAttr: unknown, drawingAttr: unknown): SketchState {
  const drawingText = typeof drawingAttr === "string" ? drawingAttr : "";
  const sceneText = typeof sceneAttr === "string" ? sceneAttr : "";
  const saved = useMemo(() => (drawingText ? cleanDrawing(drawingText) : null), [drawingText]);
  const scene = useMemo(() => (saved ? null : readScene(sceneText)), [saved, sceneText]);
  const needed = !saved && hasDrawing(scene);
  const [drawn, setDrawn] = useState<{ scene: SketchScene; state: SketchState } | null>(null);
  useEffect(() => {
    if (!needed || !scene) return;
    let live = true;
    void import("./excalidraw")
      .then(({ drawScene }) => drawScene(scene))
      .then(
        (svg) => live && setDrawn({ scene, state: svg ? { kind: "drawn", svg } : { kind: "unreadable" } }),
        () => live && setDrawn({ scene, state: { kind: "unreadable" } }),
      );
    return () => {
      live = false;
    };
  }, [needed, scene]);
  if (saved) return { kind: "drawn", svg: saved };
  if (!scene) return { kind: "unreadable" };
  if (!needed) return { kind: "empty" };
  return drawn?.scene === scene ? drawn.state : { kind: "drawing" };
}

/** A sketch's drawing as a picture, its title the words that stand for it. */
export function SketchPicture({ svg, title, onOpen }: { svg: string; title: string | null; onOpen?: () => void }) {
  const picture = <img className="doc-sketch-image" src={drawingSrc(svg)} alt={t.editor.sketch.name(title ?? "")} data-sketch-image="" />;
  if (!onOpen) return picture;
  return (
    <button type="button" className="doc-sketch-open" aria-label={t.editor.sketch.openLarger(title ?? "")} onClick={onOpen} data-action="open-sketch">
      {picture}
    </button>
  );
}

/** What stands in a sketch's place while it is drawn, or when it cannot be. */
export function SketchPending({ state }: { state: SketchState }) {
  if (state.kind === "drawing") {
    return (
      <p className="doc-sketch-note" data-sketch-state="drawing" aria-busy="true">
        {t.editor.sketch.drawing}
      </p>
    );
  }
  if (state.kind === "unreadable") {
    return (
      <p className="doc-sketch-note doc-sketch-error" data-sketch-state="unreadable">
        {t.editor.sketch.unreadable}
      </p>
    );
  }
  return null;
}

/** A sketch as readers see it, opened larger in the lightbox. */
export function SketchFigure({ attrs }: { attrs: Record<string, unknown> | undefined }) {
  const state = useSketchDrawing(attrs?.scene, attrs?.drawing);
  const title = sketchTitle(attrs?.title);
  const [open, setOpen] = useState(false);
  if (state.kind === "empty") return null;
  const name = t.editor.sketch.name(title ?? "");
  const item: LightboxItem | null =
    state.kind === "drawn"
      ? {
          id: SKETCH_FILE_NAME,
          name: title ?? t.editor.sketch.dialog,
          kind: "image",
          src: drawingSrc(state.svg),
          download: drawingSrc(state.svg),
          downloadName: SKETCH_FILE_NAME,
          alt: name,
        }
      : null;
  return (
    <figure className="doc-sketch" data-sketch="" aria-label={title ? undefined : name}>
      {state.kind === "drawn" ? <SketchPicture svg={state.svg} title={title} onOpen={() => setOpen(true)} /> : <SketchPending state={state} />}
      {title && <figcaption className="doc-sketch-title">{title}</figcaption>}
      {open && item && <Lightbox items={[item]} onClose={() => setOpen(false)} />}
    </figure>
  );
}
