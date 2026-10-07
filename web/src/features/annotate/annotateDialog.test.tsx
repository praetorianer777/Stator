import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Attachment } from "@/api/attachments";
import { axeViolations } from "@/test/axe";
import { stubUploads, upload } from "@/test/xhr";
import { AnnotateDialog } from "./AnnotateDialog";

const PICTURE = { width: 400, height: 300 };

const shot: Attachment = {
  id: "0195f000-0000-7000-8000-0000000000f2",
  pageId: "0195f000-0000-7000-8000-0000000000a1",
  fileName: "login.png",
  contentType: "image/png",
  size: 2048,
  width: PICTURE.width,
  height: PICTURE.height,
  uploadedByName: "Ada Lovelace",
  createdAt: "2026-10-07T09:00:00Z",
  version: 1,
  versions: 1,
  restoredFrom: null,
  editedFrom: null,
  preview: "none",
};

// jsdom loads no pictures; this one arrives at once, at the size given.
class LoadedImage {
  naturalWidth = PICTURE.width;
  naturalHeight = PICTURE.height;
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  set src(_: string) {
    queueMicrotask(() => this.onload?.());
  }
}

/** A canvas context that draws nothing and measures every letter alike, with what toBlob hands back. */
function stubCanvas(blobType: (asked: string) => string) {
  const context = new Proxy({} as Record<string | symbol, unknown>, {
    get: (target, key) => {
      if (key === "measureText") return (text: string) => ({ width: text.length * 10 });
      if (!(key in target)) target[key] = () => {};
      return target[key];
    },
    set: (target, key, value) => {
      target[key] = value;
      return true;
    },
  });
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation((() => context) as unknown as typeof HTMLCanvasElement.prototype.getContext);
  vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(function (this: HTMLCanvasElement, done: BlobCallback, type?: string) {
    done(new Blob([`${this.width}x${this.height}`], { type: blobType(type ?? "image/png") }));
  });
}

// jsdom has no pointer events, and its stand-in would drop where the pointer is.
class TestPointerEvent extends MouseEvent {
  pointerId: number;
  pointerType: string;
  constructor(type: string, init: PointerEventInit = {}) {
    super(type, init);
    this.pointerId = init.pointerId ?? 1;
    this.pointerType = init.pointerType ?? "mouse";
  }
}

function readText(blob: Blob): Promise<string> {
  return new Promise((resolve) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.readAsText(blob);
  });
}

beforeEach(() => {
  vi.stubGlobal("Image", LoadedImage);
  vi.stubGlobal("PointerEvent", TestPointerEvent);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function shown(props: Partial<Parameters<typeof AnnotateDialog>[0]> = {}) {
  const onSaved = vi.fn();
  const onClose = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <AnnotateDialog file={shot} onSaved={onSaved} onClose={onClose} {...props} />
    </QueryClientProvider>,
  );
  return { onSaved, onClose };
}

const dialog = () => screen.getByRole("dialog", { name: "Annotate login.png" });
const stage = () => within(dialog()).getByRole("application", { name: "login.png, to draw on" });
const canvas = () => dialog().querySelector("[data-annotate-canvas]") as HTMLCanvasElement;
const tool = (name: string) => within(dialog()).getByRole("button", { name });

async function ready() {
  await waitFor(() => expect(canvas()).not.toBeNull());
}

describe("the annotation editor", () => {
  it("is a labelled dialog whose picture is drawn on by keyboard, undone and redone", async () => {
    const user = userEvent.setup();
    shown();
    await ready();
    expect(canvas()).toHaveAttribute("width", "400");
    expect(tool("Box")).toHaveAttribute("aria-pressed", "true");
    expect(stage()).toHaveAccessibleDescription(/Drag on the picture/);

    stage().focus();
    await user.keyboard("{Enter}");
    expect(canvas()).toHaveAttribute("data-shapes", "1");
    expect(dialog().querySelector("[data-annotate-status]")).toHaveTextContent("Box selected.");

    await user.click(tool("Arrow"));
    stage().focus();
    await user.keyboard("{Enter}");
    expect(canvas()).toHaveAttribute("data-shapes", "2");
    await user.keyboard("{ArrowRight}{Shift>}{ArrowDown}{/Shift}{Delete}");
    expect(canvas()).toHaveAttribute("data-shapes", "1");

    await user.click(tool("Undo"));
    expect(canvas()).toHaveAttribute("data-shapes", "2");
    await user.keyboard("{Control>}z{/Control}");
    expect(canvas()).toHaveAttribute("data-shapes", "2");
    await user.click(tool("Redo"));
    await user.click(tool("Redo"));
    expect(canvas()).toHaveAttribute("data-shapes", "1");
    expect(tool("Redo")).toBeDisabled();
    expect(await axeViolations()).toEqual([]);
  });

  it("puts text where it is clicked and changes it once selected", async () => {
    const user = userEvent.setup();
    shown();
    await ready();
    await user.click(tool("Text"));
    fireEvent.pointerDown(canvas(), { clientX: 40, clientY: 50, pointerId: 1, pointerType: "mouse", button: 0 });
    const field = within(dialog()).getByRole("textbox", { name: "Text" });
    await user.type(field, "Click here{Enter}");
    expect(canvas()).toHaveAttribute("data-shapes", "1");
    expect(within(dialog()).getByRole("textbox", { name: "Text" })).toHaveValue("Click here");
    await user.clear(within(dialog()).getByRole("textbox", { name: "Text" }));
    await user.type(within(dialog()).getByRole("textbox", { name: "Text" }), "Sign in{Enter}");
    expect(canvas()).toHaveAttribute("data-shapes", "1");
    await user.click(tool("Undo"));
    expect(canvas()).toHaveAttribute("data-shapes", "1");
    await user.click(tool("Undo"));
    expect(canvas()).toHaveAttribute("data-shapes", "0");
  });

  it("draws a box and crops by dragging", async () => {
    shown();
    await ready();
    const drag = (from: [number, number], to: [number, number]) => {
      fireEvent.pointerDown(canvas(), { clientX: from[0], clientY: from[1], pointerId: 1, pointerType: "mouse", button: 0 });
      fireEvent.pointerMove(canvas(), { clientX: to[0], clientY: to[1], pointerId: 1, pointerType: "mouse" });
      fireEvent.pointerUp(canvas(), { clientX: to[0], clientY: to[1], pointerId: 1, pointerType: "mouse" });
    };
    drag([10, 10], [60, 40]);
    expect(canvas()).toHaveAttribute("data-shapes", "1");
    drag([100, 100], [101, 101]);
    expect(canvas()).toHaveAttribute("data-shapes", "1");
    fireEvent.click(tool("Crop"));
    drag([350, 250], [20, 30]);
    expect(canvas()).toHaveAttribute("data-crop", "20,30,330,220");
  });

  it("saves the picture as it is kept, in its own type, as the next version", async () => {
    const user = userEvent.setup();
    stubCanvas((asked) => asked);
    const sent = stubUploads();
    const { onSaved } = shown({ offerInPage: true });
    await ready();
    await user.click(within(dialog()).getByRole("button", { name: "Save as new version" }));
    expect(dialog()).toHaveTextContent("Draw, write or crop something first, then save.");
    expect(sent).toHaveLength(0);

    await user.click(tool("Crop"));
    stage().focus();
    await user.keyboard("{Enter}");
    expect(within(dialog()).getByRole("checkbox", { name: "Show the edited picture in this page" })).toBeChecked();
    await user.click(within(dialog()).getByRole("button", { name: "Save as new version" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    const request = upload(sent, 0);
    expect(request.url).toBe(`/api/v1/attachments/${shot.id}/edit`);
    expect(request.file.name).toBe("login.png");
    expect(request.file.type).toBe("image/png");
    expect(await readText(request.file)).toBe("320x240");
    const made = { ...shot, id: "0195f000-0000-7000-8000-0000000000f9", version: 2, versions: 2, editedFrom: 1, width: 320, height: 240 };
    request.respond(201, { attachment: made });
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(made, true));
  });

  it("says so when the browser cannot write the picture's type, or the server refuses", async () => {
    const user = userEvent.setup();
    stubCanvas(() => "image/png");
    const sent = stubUploads();
    shown({ file: { ...shot, fileName: "photo.webp", contentType: "image/webp" } });
    await waitFor(() => expect(screen.getByRole("dialog", { name: "Annotate photo.webp" })).toBeInTheDocument());
    const here = screen.getByRole("dialog", { name: "Annotate photo.webp" });
    await waitFor(() => expect(here.querySelector("[data-annotate-canvas]")).not.toBeNull());
    within(here).getByRole("application").focus();
    await user.keyboard("{Enter}");
    await user.click(within(here).getByRole("button", { name: "Save as new version" }));
    expect(here).toHaveTextContent("This browser cannot save WebP pictures.");
    expect(sent).toHaveLength(0);
  });

  it("passes the server's sentence on when the save is refused", async () => {
    const user = userEvent.setup();
    stubCanvas((asked) => asked);
    const sent = stubUploads();
    const { onSaved } = shown();
    await ready();
    stage().focus();
    await user.keyboard("{Enter}");
    await user.click(within(dialog()).getByRole("button", { name: "Save as new version" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    upload(sent, 0).respond(403, { error: { code: "forbidden", message: "You may only read this page. Ask a space administrator for edit access." } });
    await waitFor(() => expect(dialog()).toHaveTextContent("The edited login.png could not be saved: You may only read this page."));
    expect(onSaved).not.toHaveBeenCalled();
  });

  it("asks before discarding a drawing, and closes at once on an untouched picture", async () => {
    const user = userEvent.setup();
    const { onClose } = shown();
    await ready();
    await user.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(1);

    const ask = vi.spyOn(window, "confirm").mockReturnValue(false);
    stage().focus();
    await user.keyboard("{Enter}");
    await user.keyboard("{Escape}");
    expect(ask).toHaveBeenCalledWith("Discard what you drew on this picture?");
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
