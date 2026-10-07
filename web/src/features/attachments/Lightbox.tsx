import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent, type MouseEvent, type PointerEvent } from "react";
import { createPortal } from "react-dom";
import { Button, ButtonLink, ErrorBanner, IconButton } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { LIGHTBOX_DOUBLE_ZOOM, LIGHTBOX_MAX_ZOOM, LIGHTBOX_PAN_STEP_PX, LIGHTBOX_SWIPE_PX, LIGHTBOX_WHEEL_ZOOM_PER_PX, LIGHTBOX_ZOOM_STEP } from "@/config";
import { t } from "@/i18n";
import type { LightboxItem } from "./lightboxItems";
import { FITTED, clampView, isZoomed, panBy, step, zoomAt, zoomPercent, type Bounds, type Point, type View } from "./zoom";

// A wheel that scrolls by lines or pages reports how many, not pixels.
const WHEEL_LINE_PX = 16;

type Gesture = { kind: "pan"; last: Point } | { kind: "swipe"; start: Point } | { kind: "pinch"; distance: number; middle: Point } | null;

function distance(a: Point, b: Point): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

function middle(a: Point, b: Point): Point {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
}

/**
 * Pictures and videos over the whole window, one at a time: a picture zooms
 * and pans by buttons, keys, wheel and fingers, a video plays in the
 * browser's own player. Given several, it steps through them.
 */
export function Lightbox({ items, start = 0, onClose }: { items: readonly LightboxItem[]; start?: number; onClose: () => void }) {
  const l = t.lightbox;
  const [index, setIndex] = useState(() => Math.min(Math.max(start, 0), Math.max(items.length - 1, 0)));
  const [view, setView] = useState<View>(FITTED);
  const [failed, setFailed] = useState(false);
  const dialog = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const picture = useRef<HTMLImageElement>(null);
  const video = useRef<HTMLVideoElement>(null);
  const pointers = useRef(new Map<number, Point>());
  const gesture = useRef<Gesture>(null);
  const titleId = useId();
  const hintId = useId();
  useEscape(true, onClose);
  useFocusReturn(true, dialog, true);

  const item = items[index];
  const many = items.length > 1;
  const zoomable = item?.kind === "image" && !failed;
  const zoomed = isZoomed(view);

  // After the trap has put focus on the first button: the picture or the
  // player is what a keyboard comes to use.
  useEffect(() => {
    (video.current ?? stage.current)?.focus({ preventScroll: true });
  }, []);

  const go = useCallback(
    (by: number) => {
      setIndex((at) => step(at, by, items.length));
      setView(FITTED);
      setFailed(false);
      gesture.current = null;
      pointers.current.clear();
    },
    [items.length],
  );

  const bounds = useCallback((): Bounds | null => {
    const room = stage.current;
    const image = picture.current;
    if (!room || !image) return null;
    return { width: room.clientWidth, height: room.clientHeight, imageWidth: image.offsetWidth, imageHeight: image.offsetHeight };
  }, []);

  const change = useCallback(
    (next: (view: View, bounds: Bounds) => View) => {
      const b = bounds();
      if (b) setView((current) => next(current, b));
    },
    [bounds],
  );

  // From the window to the middle of the room, where the zoom measures from.
  const inRoom = useCallback((x: number, y: number): Point => {
    const rect = stage.current?.getBoundingClientRect();
    return rect ? { x: x - rect.left - rect.width / 2, y: y - rect.top - rect.height / 2 } : { x: 0, y: 0 };
  }, []);

  useEffect(() => {
    const room = stage.current;
    if (!room || !zoomable) return;
    // Not a React handler: those are passive, and the page must not scroll.
    function onWheel(event: WheelEvent) {
      event.preventDefault();
      const px = event.deltaMode === WheelEvent.DOM_DELTA_PIXEL ? event.deltaY : event.deltaY * WHEEL_LINE_PX;
      change((v, b) => zoomAt(v, Math.exp(-px * LIGHTBOX_WHEEL_ZOOM_PER_PX), inRoom(event.clientX, event.clientY), b));
    }
    room.addEventListener("wheel", onWheel, { passive: false });
    return () => room.removeEventListener("wheel", onWheel);
  }, [zoomable, change, inRoom]);

  useEffect(() => {
    const refit = () => change((v, b) => clampView(v, b));
    window.addEventListener("resize", refit);
    return () => window.removeEventListener("resize", refit);
  }, [change]);

  if (!item) return null;

  const zoomBy = (factor: number) => change((v, b) => zoomAt(v, factor, { x: 0, y: 0 }, b));

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    // The player's own keys seek and change the volume.
    if (event.target instanceof HTMLVideoElement || event.altKey || event.ctrlKey || event.metaKey) return;
    const pan = (dx: number, dy: number) => change((v, b) => panBy(v, dx, dy, b));
    switch (event.key) {
      case "+":
      case "=":
        if (!zoomable) return;
        zoomBy(LIGHTBOX_ZOOM_STEP);
        break;
      case "-":
        if (!zoomable) return;
        zoomBy(1 / LIGHTBOX_ZOOM_STEP);
        break;
      case "0":
        if (!zoomable) return;
        setView(FITTED);
        break;
      case "ArrowLeft":
      case "ArrowRight": {
        const sign = event.key === "ArrowLeft" ? 1 : -1;
        if (zoomed) pan(sign * LIGHTBOX_PAN_STEP_PX, 0);
        else if (many) go(-sign);
        else return;
        break;
      }
      case "ArrowUp":
      case "ArrowDown":
        if (!zoomed) return;
        pan(0, (event.key === "ArrowUp" ? 1 : -1) * LIGHTBOX_PAN_STEP_PX);
        break;
      default:
        return;
    }
    event.preventDefault();
  }

  function onPointerDown(event: PointerEvent<HTMLDivElement>) {
    if (!zoomable || (event.target as Element).closest("button")) return;
    event.currentTarget.setPointerCapture?.(event.pointerId);
    const at = { x: event.clientX, y: event.clientY };
    pointers.current.set(event.pointerId, at);
    const [a, b] = [...pointers.current.values()];
    if (a && b) gesture.current = { kind: "pinch", distance: distance(a, b), middle: middle(a, b) };
    else if (zoomed) gesture.current = { kind: "pan", last: at };
    else if (event.pointerType !== "mouse") gesture.current = { kind: "swipe", start: at };
    else gesture.current = null;
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    if (!pointers.current.has(event.pointerId)) return;
    const at = { x: event.clientX, y: event.clientY };
    pointers.current.set(event.pointerId, at);
    const now = gesture.current;
    if (now?.kind === "pinch") {
      const [a, b] = [...pointers.current.values()];
      if (!a || !b) return;
      const spread = distance(a, b);
      const centre = middle(a, b);
      const factor = now.distance > 0 ? spread / now.distance : 1;
      const moved = { x: centre.x - now.middle.x, y: centre.y - now.middle.y };
      gesture.current = { kind: "pinch", distance: spread, middle: centre };
      change((v, bounds) => panBy(zoomAt(v, factor, inRoom(centre.x, centre.y), bounds), moved.x, moved.y, bounds));
    } else if (now?.kind === "pan") {
      gesture.current = { kind: "pan", last: at };
      change((v, b) => panBy(v, at.x - now.last.x, at.y - now.last.y, b));
    }
  }

  function onPointerEnd(event: PointerEvent<HTMLDivElement>) {
    const at = pointers.current.get(event.pointerId);
    pointers.current.delete(event.pointerId);
    const now = gesture.current;
    if (now?.kind === "swipe" && at && event.type === "pointerup") {
      const dx = event.clientX - now.start.x;
      const dy = event.clientY - now.start.y;
      if (many && Math.abs(dx) >= LIGHTBOX_SWIPE_PX && Math.abs(dx) > Math.abs(dy)) go(dx < 0 ? 1 : -1);
    }
    const [left] = [...pointers.current.values()];
    // A pinch that lifts one finger carries on as a pan with the other.
    gesture.current = left && now?.kind === "pinch" ? { kind: "pan", last: left } : null;
  }

  function onDoubleClick(event: MouseEvent<HTMLDivElement>) {
    if (!zoomable || (event.target as Element).closest("button")) return;
    if (zoomed) setView(FITTED);
    else change((v, b) => zoomAt(v, LIGHTBOX_DOUBLE_ZOOM, inRoom(event.clientX, event.clientY), b));
  }

  const percent = zoomPercent(view);
  const position = many ? l.position(index + 1, items.length) : "";

  return createPortal(
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      className="fixed inset-0 z-50 flex flex-col bg-surface-sunken text-ink"
      onKeyDown={onKeyDown}
      data-lightbox=""
      data-lightbox-item={item.id}
    >
      <div className="flex flex-wrap items-center gap-1 border-b border-border bg-surface px-3 py-2 sm:gap-2">
        <h2 id={titleId} className="min-w-0 flex-1 truncate text-sm font-semibold" data-lightbox-title="">
          {item.name}
        </h2>
        {many && (
          <span className="text-sm text-ink-muted tabular-nums" data-lightbox-position="">
            {position}
          </span>
        )}
        {zoomable && (
          <span className="flex items-center gap-1">
            <IconButton icon={<Icon.ZoomOut />} label={l.zoomOut} size="sm" onClick={() => zoomBy(1 / LIGHTBOX_ZOOM_STEP)} data-action="zoom-out" />
            <Button
              size="sm"
              variant="ghost"
              className="min-w-14 tabular-nums"
              aria-label={l.fit(percent)}
              title={l.fit(percent)}
              onClick={() => setView(FITTED)}
              data-action="zoom-fit"
              data-zoom={percent}
            >
              {`${percent}%`}
            </Button>
            <IconButton
              icon={<Icon.ZoomIn />}
              label={l.zoomIn}
              size="sm"
              onClick={() => zoomBy(LIGHTBOX_ZOOM_STEP)}
              aria-disabled={view.scale >= LIGHTBOX_MAX_ZOOM || undefined}
              data-action="zoom-in"
            />
          </span>
        )}
        <ButtonLink
          href={item.download}
          download={item.downloadName || true}
          variant="ghost"
          size="sm"
          icon={<Icon.Download />}
          aria-label={t.attachments.download(item.name)}
          title={t.attachments.download(item.name)}
          data-action="download-lightbox"
        />
        <IconButton icon={<Icon.X />} label={t.common.close} size="sm" onClick={onClose} data-focus-last="" data-action="close-lightbox" />
      </div>

      {/* biome-ignore lint/a11y/useSemanticElements: a fieldset groups form controls; this is the room a picture zooms in, focused for its keys */}
      <div
        ref={stage}
        role="group"
        aria-label={zoomable ? l.stage(item.name) : item.name}
        aria-describedby={zoomable ? hintId : undefined}
        tabIndex={zoomable ? 0 : -1}
        className="relative flex min-h-0 flex-1 items-center justify-center overflow-hidden p-2 outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-inset sm:p-6"
        style={zoomable ? { touchAction: "none", cursor: zoomed ? "grab" : "zoom-in" } : undefined}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerEnd}
        onPointerCancel={onPointerEnd}
        onDoubleClick={onDoubleClick}
        data-lightbox-stage=""
      >
        {failed ? (
          <div className="max-w-md">
            <ErrorBanner>{item.kind === "video" ? l.videoFailed(item.name) : l.imageFailed(item.name)}</ErrorBanner>
          </div>
        ) : item.kind === "image" ? (
          <img
            key={item.id}
            ref={picture}
            src={item.src}
            alt={item.alt || item.name}
            draggable={false}
            className="max-h-full max-w-full select-none object-contain"
            style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
            onError={() => setFailed(true)}
            data-lightbox-image=""
            data-zoom={percent}
          />
        ) : (
          // biome-ignore lint/a11y/useMediaCaption: a file someone uploaded comes with no captions to offer
          <video
            key={item.id}
            ref={video}
            src={item.src}
            controls
            playsInline
            preload="metadata"
            tabIndex={0}
            aria-label={item.name}
            className="max-h-full max-w-full bg-surface"
            onError={() => setFailed(true)}
            data-lightbox-video=""
          />
        )}
        {many && (
          <>
            <IconButton
              icon={<Icon.ChevronLeft />}
              label={l.previous}
              variant="secondary"
              className="absolute top-1/2 left-2 -translate-y-1/2"
              onClick={() => go(-1)}
              data-action="lightbox-previous"
            />
            <IconButton
              icon={<Icon.ChevronRight />}
              label={l.next}
              variant="secondary"
              className="absolute top-1/2 right-2 -translate-y-1/2"
              onClick={() => go(1)}
              data-action="lightbox-next"
            />
          </>
        )}
      </div>
      {zoomable && (
        <p id={hintId} className="sr-only">
          {many ? l.hintMany : l.hint}
        </p>
      )}
      <p role="status" className="sr-only" data-lightbox-status="">
        {zoomable ? l.status(item.name, position, percent) : l.status(item.name, position)}
      </p>
    </div>,
    document.body,
  );
}
