import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { createPortal } from "react-dom";
import { ApiError } from "@/api/client";
import { attachmentUrl, useEditAttachment, type Attachment } from "@/api/attachments";
import { Button, Checkbox, ErrorBanner, Field, IconButton, cx } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import {
  ANNOTATION_COLOURS,
  ANNOTATION_DEFAULT_COLOUR,
  ANNOTATION_HIT_PX,
  ANNOTATION_MAX_PIXELS,
  ANNOTATION_MIN_DRAG_PX,
  ANNOTATION_QUALITY,
  ANNOTATION_TEXT_MAX_LENGTH,
  ANNOTATION_TYPES,
} from "@/config";
import { t } from "@/i18n";
import {
  CROP,
  commit,
  cropInMiddle,
  fitCrop,
  isChanged,
  keyStep,
  mediaType,
  nextSelection,
  nudge,
  outputRect,
  rectBetween,
  redo,
  removeSelected,
  replaceShape,
  shapeAt,
  shapeInMiddle,
  startHistory,
  undo,
  type Annotation,
  type Colour,
  type History,
  type Point,
  type Shape,
  type Size,
} from "./annotation";
import { drawEditing, drawSaved, measureWith } from "./draw";

type Tool = "select" | "crop" | "arrow" | "box" | "text";

const TOOLS: ReadonlyArray<{ tool: Tool; icon: typeof Icon.Pointer }> = [
  { tool: "select", icon: Icon.Pointer },
  { tool: "crop", icon: Icon.Crop },
  { tool: "arrow", icon: Icon.Arrow },
  { tool: "box", icon: Icon.Box },
  { tool: "text", icon: Icon.Text },
];

const COLOURS = Object.keys(ANNOTATION_COLOURS) as Colour[];

const KEY_STEPS: Readonly<Record<string, [number, number]>> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };

type Drag =
  | { kind: "draw"; tool: "arrow" | "box" | "crop"; id: string; start: Point; client: Point; pointer: number }
  | { kind: "move"; id: string; last: Point; pointer: number };

/** Words being put on the picture: a new label at a place, or the label with this id changed. */
interface TextEntry {
  at: Point;
  value: string;
  id: string | null;
}

let lastShape = 0;
function shapeId(): string {
  lastShape += 1;
  return `shape-${lastShape}`;
}

function saveError(name: string, error: unknown): string {
  if (error instanceof ApiError && error.code === "network") return t.annotate.broke(name);
  const why = error instanceof ApiError ? error.fields.file || error.message : error instanceof Error ? error.message : "";
  return why ? t.annotate.refused(name, why) : t.annotate.broke(name);
}

/**
 * Crops a picture and draws arrows, boxes and text on it, then saves the
 * result as the next version of its file; the version drawn on stays as it was.
 */
export function AnnotateDialog({
  file,
  offerInPage = false,
  onSaved,
  onClose,
}: {
  file: Attachment;
  /** Opened from a picture in the editor, which may then show the edited version in its place. */
  offerInPage?: boolean;
  /** Told of the new version, and whether the picture it was opened from is to show it. */
  onSaved: (made: Attachment, showInPage: boolean) => void;
  onClose: () => void;
}) {
  const a = t.annotate;
  const titleId = useId();
  const hintId = useId();
  const textId = useId();
  const dialog = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const drag = useRef<Drag | null>(null);
  const [picture, setPicture] = useState<HTMLImageElement | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [history, setHistory] = useState<History>(() => startHistory());
  const [live, setLive] = useState<Annotation | null>(null);
  const [tool, setTool] = useState<Tool>("box");
  const [colour, setColour] = useState<Colour>(ANNOTATION_DEFAULT_COLOUR);
  const [selected, setSelected] = useState<string | null>(null);
  const [entry, setEntry] = useState<TextEntry | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [showInPage, setShowInPage] = useState(true);
  const edit = useEditAttachment(file.pageId);

  const size = useMemo<Size | null>(() => (picture ? { width: picture.naturalWidth, height: picture.naturalHeight } : null), [picture]);
  const present = history.present;
  const shown = live ?? present;
  const type = mediaType(file.contentType);

  const requestClose = useCallback(() => {
    if (size && isChanged(present, size) && !window.confirm(t.annotate.confirmDiscard)) return;
    onClose();
  }, [size, present, onClose]);
  useEscape(true, requestClose);
  useFocusReturn(true, dialog, true);

  useEffect(() => {
    const image = new Image();
    image.onload = () => {
      if (image.naturalWidth * image.naturalHeight > ANNOTATION_MAX_PIXELS)
        setLoadError(t.annotate.tooLarge(file.fileName, image.naturalWidth, image.naturalHeight));
      else setPicture(image);
    };
    image.onerror = () => setLoadError(t.annotate.failed(file.fileName));
    image.src = attachmentUrl(file.id, true);
    return () => {
      image.onload = null;
      image.onerror = null;
    };
  }, [file.id, file.fileName]);

  useLayoutEffect(() => {
    const context = canvas.current?.getContext("2d");
    if (!context || !picture || !size) return;
    drawEditing(context, picture, size, shown, selected);
  });

  const change = (next: Annotation) => {
    setProblem(null);
    setHistory((was) => commit(was, next));
  };

  const select = (id: string | null, from: Annotation = present) => {
    setSelected(id);
    const shape = from.shapes.find((s) => s.id === id);
    setEntry(shape?.kind === "text" ? { at: shape.at, value: shape.text, id: shape.id } : null);
  };

  function toPicture(clientX: number, clientY: number): { at: Point; perScreenPx: number } {
    const box = canvas.current?.getBoundingClientRect();
    if (!box || !size || box.width <= 0) return { at: { x: clientX, y: clientY }, perScreenPx: 1 };
    const perScreenPx = size.width / box.width;
    return { at: { x: (clientX - box.left) * perScreenPx, y: (clientY - box.top) * perScreenPx }, perScreenPx };
  }

  function onPointerDown(event: PointerEvent<HTMLCanvasElement>) {
    if (!size || drag.current || (event.pointerType === "mouse" && event.button !== 0)) return;
    event.preventDefault();
    stage.current?.focus({ preventScroll: true });
    const { at, perScreenPx } = toPicture(event.clientX, event.clientY);
    const measure = measureWith(event.currentTarget.getContext("2d"));
    const hit = shapeAt(present.shapes, at, ANNOTATION_HIT_PX * perScreenPx, size, measure);
    if (tool === "text") {
      if (hit?.kind === "text") select(hit.id);
      else {
        setSelected(null);
        setEntry({ at, value: "", id: null });
      }
      return;
    }
    event.currentTarget.setPointerCapture?.(event.pointerId);
    if (tool === "select") {
      const crop = present.crop;
      const id = hit?.id ?? (crop && at.x >= crop.x && at.x <= crop.x + crop.width && at.y >= crop.y && at.y <= crop.y + crop.height ? CROP : null);
      select(id);
      if (id) drag.current = { kind: "move", id, last: at, pointer: event.pointerId };
      return;
    }
    drag.current = {
      kind: "draw",
      tool,
      id: tool === "crop" ? CROP : shapeId(),
      start: at,
      client: { x: event.clientX, y: event.clientY },
      pointer: event.pointerId,
    };
  }

  function onPointerMove(event: PointerEvent<HTMLCanvasElement>) {
    const now = drag.current;
    if (!now || !size || event.pointerId !== now.pointer) return;
    const { at } = toPicture(event.clientX, event.clientY);
    if (now.kind === "move") {
      setLive((was) => nudge(was ?? present, now.id, at.x - now.last.x, at.y - now.last.y, false, size));
      now.last = at;
      return;
    }
    if (now.tool === "crop") {
      setLive({ ...present, crop: fitCrop(rectBetween(now.start, at), size) });
      return;
    }
    const shape: Shape =
      now.tool === "box"
        ? { id: now.id, kind: "box", colour, rect: rectBetween(now.start, at) }
        : { id: now.id, kind: "arrow", colour, from: now.start, to: at };
    setLive({ ...present, shapes: [...present.shapes, shape] });
  }

  function onPointerEnd(event: PointerEvent<HTMLCanvasElement>) {
    const now = drag.current;
    if (!now || event.pointerId !== now.pointer) return;
    drag.current = null;
    const dragged = now.kind === "move" || Math.hypot(event.clientX - now.client.x, event.clientY - now.client.y) >= ANNOTATION_MIN_DRAG_PX;
    if (live && dragged && event.type === "pointerup") {
      change(live);
      if (now.kind === "draw") select(now.id, live);
    }
    setLive(null);
  }

  function placeInMiddle() {
    if (!size) return;
    if (tool === "select") {
      select(nextSelection(present, selected));
    } else if (tool === "text") {
      setSelected(null);
      setEntry({ at: { x: size.width / 4, y: size.height / 2 }, value: "", id: null });
    } else if (tool === "crop") {
      const next = { ...present, crop: present.crop ?? cropInMiddle(size) };
      change(next);
      select(CROP, next);
    } else {
      const shape = shapeInMiddle(tool, shapeId(), colour, size);
      const next = { ...present, shapes: [...present.shapes, shape] };
      change(next);
      select(shape.id, next);
    }
  }

  function removeSelection() {
    const next = removeSelected(present, selected);
    if (next === present) return;
    change(next);
    select(null, next);
  }

  function onStageKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (!size || event.altKey || event.ctrlKey || event.metaKey) return;
    const step = KEY_STEPS[event.key];
    if (step && selected) {
      event.preventDefault();
      const by = keyStep(size);
      change(nudge(present, selected, step[0] * by, step[1] * by, event.shiftKey, size));
    } else if (event.key === "Enter") {
      event.preventDefault();
      placeInMiddle();
    } else if ((event.key === "Delete" || event.key === "Backspace") && selected) {
      event.preventDefault();
      removeSelection();
    }
  }

  function onDialogKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    // A text field keeps its own undo.
    if (event.target instanceof HTMLInputElement || !(event.ctrlKey || event.metaKey)) return;
    const key = event.key.toLowerCase();
    if (key === "z" || key === "y") {
      event.preventDefault();
      setHistory(key === "y" || event.shiftKey ? redo : undo);
      setLive(null);
    }
  }

  function pickColour(next: Colour) {
    setColour(next);
    const shape = present.shapes.find((s) => s.id === selected);
    if (shape && shape.colour !== next) change(replaceShape(present, { ...shape, colour: next }));
  }

  function applyText() {
    if (!entry) return;
    const text = entry.value.trim();
    const existing = present.shapes.find((s) => s.id === entry.id);
    if (existing?.kind === "text") {
      const next = text ? replaceShape(present, { ...existing, text }) : removeSelected(present, existing.id);
      change(next);
      select(text ? existing.id : null, next);
    } else if (text) {
      const label: Shape = { id: shapeId(), kind: "text", colour, at: entry.at, text };
      const next = { ...present, shapes: [...present.shapes, label] };
      change(next);
      select(label.id, next);
    } else {
      setEntry(null);
    }
    stage.current?.focus({ preventScroll: true });
  }

  async function save() {
    if (!picture || !size) return;
    if (!isChanged(present, size)) {
      setProblem(a.unchanged);
      return;
    }
    const typeName = ANNOTATION_TYPES[type] ?? type;
    const kept = outputRect(present, size);
    const out = document.createElement("canvas");
    out.width = kept.width;
    out.height = kept.height;
    const context = out.getContext("2d");
    if (!context) {
      setProblem(a.cannotSave(typeName));
      return;
    }
    drawSaved(context, picture, size, present);
    const blob = await new Promise<Blob | null>((resolve) => out.toBlob(resolve, type, ANNOTATION_QUALITY));
    // A browser that cannot write the type hands back a PNG instead.
    if (!blob || blob.type !== type) {
      setProblem(a.cannotSave(typeName));
      return;
    }
    setProblem(null);
    // Awaited rather than told by mutate's callbacks: the new version takes
    // the place of the list's row that opened this, which unmounts it.
    let made: Attachment;
    try {
      made = await edit.mutateAsync({ id: file.id, file: new File([blob], file.fileName, { type }) });
    } catch (error) {
      setProblem(saveError(file.fileName, error));
      return;
    }
    onSaved(made, offerInPage && showInPage);
  }

  const selectedName = selected === CROP ? a.tool.crop : a.tool[present.shapes.find((s) => s.id === selected)?.kind ?? ""];

  return createPortal(
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      className="fixed inset-0 z-50 flex flex-col bg-surface-sunken text-ink"
      onKeyDown={onDialogKeyDown}
      data-annotate=""
      data-annotate-item={file.id}
    >
      <div className="flex flex-wrap items-center gap-1 border-b border-border bg-surface px-3 py-2 sm:gap-2">
        <h2 id={titleId} className="min-w-0 flex-1 truncate text-sm font-semibold">
          {a.title(file.fileName)}
        </h2>
        <IconButton
          icon={<Icon.Undo />}
          label={a.undo}
          size="sm"
          disabled={history.past.length === 0}
          onClick={() => setHistory(undo)}
          data-action="annotate-undo"
        />
        <IconButton
          icon={<Icon.Redo />}
          label={a.redo}
          size="sm"
          disabled={history.future.length === 0}
          onClick={() => setHistory(redo)}
          data-action="annotate-redo"
        />
        <IconButton icon={<Icon.X />} label={t.common.close} size="sm" onClick={requestClose} data-focus-last="" data-action="annotate-close" />
      </div>

      <div className="flex flex-wrap items-center gap-2 border-b border-border bg-surface px-3 py-2">
        <fieldset className="inline-flex flex-wrap rounded-control border border-border bg-surface p-0.5">
          <legend className="sr-only">{a.tools}</legend>
          {TOOLS.map(({ tool: each, icon: ToolIcon }) => (
            <button
              key={each}
              type="button"
              aria-pressed={tool === each}
              onClick={() => {
                setTool(each);
                if (each !== "text" && entry?.id === null) setEntry(null);
              }}
              className={cx(
                "inline-flex items-center gap-1 rounded-[5px] px-2 py-1 text-sm transition-colors",
                tool === each ? "bg-accent-subtle font-medium text-accent" : "text-ink-muted hover:text-ink",
              )}
              data-annotate-tool={each}
            >
              <ToolIcon />
              {a.tool[each]}
            </button>
          ))}
        </fieldset>
        <fieldset className="inline-flex items-center gap-1">
          <legend className="sr-only">{a.colour}</legend>
          {COLOURS.map((each) => (
            <button
              key={each}
              type="button"
              aria-pressed={colour === each}
              aria-label={a.colours[each]}
              title={a.colours[each]}
              onClick={() => pickColour(each)}
              className={cx(
                "size-7 rounded-full border-2 transition-shadow",
                colour === each ? "border-ink ring-2 ring-focus ring-offset-1 ring-offset-surface" : "border-border-strong",
              )}
              style={{ backgroundColor: ANNOTATION_COLOURS[each] }}
              data-annotate-colour={each}
            />
          ))}
        </fieldset>
        <IconButton icon={<Icon.Trash />} label={a.remove} size="sm" disabled={!selected} onClick={removeSelection} data-action="annotate-remove" />
      </div>

      <div
        ref={stage}
        role="application"
        aria-label={a.stage(file.fileName)}
        aria-describedby={hintId}
        tabIndex={size ? 0 : -1}
        className="relative flex min-h-0 flex-1 items-center justify-center overflow-hidden p-2 outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-inset sm:p-6"
        onKeyDown={onStageKeyDown}
        data-annotate-stage=""
      >
        {loadError ? (
          <div className="max-w-md">
            <ErrorBanner>{loadError}</ErrorBanner>
          </div>
        ) : size ? (
          <canvas
            ref={canvas}
            width={size.width}
            height={size.height}
            className="max-h-full max-w-full bg-surface"
            style={{ touchAction: "none", cursor: tool === "select" ? "default" : tool === "text" ? "text" : "crosshair" }}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerEnd}
            onPointerCancel={onPointerEnd}
            data-annotate-canvas=""
            data-shapes={shown.shapes.length}
            data-crop={shown.crop ? `${shown.crop.x},${shown.crop.y},${shown.crop.width},${shown.crop.height}` : undefined}
          />
        ) : (
          <p role="status" className="text-sm text-ink-muted">
            {a.loading(file.fileName)}
          </p>
        )}
      </div>
      <p id={hintId} className="sr-only">
        {a.hint}
      </p>

      <div className="space-y-2 border-t border-border bg-surface px-3 py-2">
        {entry && (
          <div className="flex flex-wrap items-end gap-2" data-annotate-text="">
            <Field
              id={textId}
              label={a.textLabel}
              hint={a.textHint}
              className="w-72 max-w-full"
              value={entry.value}
              maxLength={ANNOTATION_TEXT_MAX_LENGTH}
              autoFocus
              onChange={(event) => setEntry({ ...entry, value: event.target.value })}
              onKeyDown={(event) => {
                if (event.key !== "Enter") return;
                event.preventDefault();
                applyText();
              }}
            />
            <Button size="sm" variant="secondary" onClick={applyText} data-action="annotate-apply-text">
              {entry.id ? a.changeText : a.addText}
            </Button>
          </div>
        )}
        {problem && <ErrorBanner>{problem}</ErrorBanner>}
        <div className="flex flex-wrap items-center justify-end gap-2">
          {offerInPage && (
            <Checkbox
              label={a.showInPage}
              checked={showInPage}
              onChange={(event) => setShowInPage(event.target.checked)}
              className="mr-auto"
              data-annotate-in-page=""
            />
          )}
          <Button variant="ghost" size="sm" onClick={requestClose} data-action="annotate-cancel">
            {a.cancel}
          </Button>
          <Button size="sm" icon={<Icon.Check />} loading={edit.isPending} disabled={!size} onClick={() => void save()} data-action="annotate-save">
            {a.save}
          </Button>
        </div>
      </div>
      <p role="status" className="sr-only" data-annotate-status="">
        {selected && selectedName ? a.selected(selectedName) : ""}
      </p>
    </div>,
    document.body,
  );
}
