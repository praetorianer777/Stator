import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import type { Attachment } from "@/api/attachments";
import { DocView } from "@/features/editor/DocView";
import { PublicLinkContext, PublicReadingContext } from "@/features/editor/publicReading";
import type { Doc } from "@/features/editor/schema";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { AttachmentPanel } from "./AttachmentPanel";
import { Lightbox } from "./Lightbox";
import { mediaItems, mediaKind, type LightboxItem } from "./lightboxItems";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const pageId = "0195f000-0000-7000-8000-0000000000a1";
const file = (over: Partial<Attachment>): Attachment => ({
  id: "0195f000-0000-7000-8000-0000000000f1",
  pageId,
  fileName: "harbour.png",
  contentType: "image/png",
  size: 2048,
  width: 640,
  height: 480,
  uploadedByName: "Ada Lovelace",
  createdAt: "2026-09-29T09:00:00Z",
  version: 1,
  versions: 1,
  restoredFrom: null,
  editedFrom: null,
  preview: "none",
  ...over,
});
const harbour = file({});
const notes = file({ id: "0195f000-0000-7000-8000-0000000000f2", fileName: "notes.txt", contentType: "text/plain" });
const tour = file({ id: "0195f000-0000-7000-8000-0000000000f3", fileName: "tour.mp4", contentType: "video/mp4", width: null, height: null });
const lighthouse = file({ id: "0195f000-0000-7000-8000-0000000000f4", fileName: "lighthouse.jpg", contentType: "image/jpeg" });

const items: LightboxItem[] = mediaItems([harbour, notes, tour, lighthouse]);

function Opener({ list, start = 0 }: { list: LightboxItem[]; start?: number }) {
  const [open, setOpen] = useState(false);
  return (
    <main>
      <h1>Plan</h1>
      <button type="button" onClick={() => setOpen(true)}>
        Open
      </button>
      {open && <Lightbox items={list} start={start} onClose={() => setOpen(false)} />}
    </main>
  );
}

async function opened(list: LightboxItem[], start = 0) {
  const user = userEvent.setup();
  render(<Opener list={list} start={start} />);
  await user.click(screen.getByRole("button", { name: "Open" }));
  return { user, dialog: screen.getByRole("dialog") };
}

const zoomOf = (dialog: HTMLElement) => dialog.querySelector("[data-lightbox-image]")?.getAttribute("data-zoom");

describe("which files the lightbox shows", () => {
  it("are the pictures and videos the browser shows in place, in their order", () => {
    expect([harbour, notes, tour].map((f) => mediaKind(f.contentType))).toEqual(["image", null, "video"]);
    expect(mediaKind("image/svg+xml")).toBeNull();
    expect(items.map((each) => each.name)).toEqual(["harbour.png", "tour.mp4", "lighthouse.jpg"]);
    expect(items[0]).toMatchObject({
      src: `/api/v1/attachments/${harbour.id}?inline=1`,
      download: `/api/v1/attachments/${harbour.id}`,
      downloadName: "harbour.png",
    });
  });
});

describe("the lightbox", () => {
  it("is a dialog named by its file, which takes focus and gives it back on Escape", async () => {
    const { user, dialog } = await opened([items[0]!]);
    expect(dialog).toHaveAccessibleName("harbour.png");
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(within(dialog).getByRole("img", { name: "harbour.png" })).toHaveAttribute("src", items[0]!.src);
    expect(within(dialog).getByRole("group", { name: "harbour.png, zoomable" })).toHaveFocus();
    expect(within(dialog).getByRole("link", { name: "Download harbour.png" })).toHaveAttribute("download", "harbour.png");
    expect(within(dialog).queryByRole("button", { name: "Next" })).toBeNull();
    expect(await axeViolations()).toEqual([]);

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("button", { name: "Open" })).toHaveFocus();
  });

  it("zooms by its buttons and by keys, and says how far", async () => {
    const { user, dialog } = await opened([items[0]!]);
    expect(zoomOf(dialog)).toBe("100");
    await user.click(within(dialog).getByRole("button", { name: "Zoom in" }));
    expect(zoomOf(dialog)).toBe("150");
    await user.keyboard("+");
    expect(zoomOf(dialog)).toBe("225");
    expect(within(dialog).getByRole("status")).toHaveTextContent("harbour.png, 225%");
    await user.keyboard("-");
    expect(zoomOf(dialog)).toBe("150");
    await user.click(within(dialog).getByRole("button", { name: "Fit to the window, now at 150%" }));
    expect(zoomOf(dialog)).toBe("100");
    await user.click(within(dialog).getByRole("button", { name: "Zoom out" }));
    expect(zoomOf(dialog)).toBe("100");
    for (let i = 0; i < 10; i++) await user.keyboard("=");
    expect(zoomOf(dialog)).toBe("800");
    await user.keyboard("0");
    expect(zoomOf(dialog)).toBe("100");
  });

  it("zooms by the wheel and by a double click, and fits again on the second", async () => {
    const { dialog } = await opened([items[0]!]);
    const stage = within(dialog).getByRole("group");
    fireEvent.wheel(stage, { deltaY: -200 });
    expect(Number(zoomOf(dialog))).toBeGreaterThan(100);
    fireEvent.doubleClick(stage);
    expect(zoomOf(dialog)).toBe("100");
    fireEvent.doubleClick(stage);
    expect(zoomOf(dialog)).toBe("250");
  });

  it("steps through several with buttons and arrow keys, going round at the ends", async () => {
    const { user, dialog } = await opened(items, 2);
    expect(dialog).toHaveAccessibleName("lighthouse.jpg");
    expect(within(dialog).getByText("3 of 3")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Next" }));
    expect(dialog).toHaveAccessibleName("harbour.png");
    expect(within(dialog).getByRole("status")).toHaveTextContent("1 of 3, harbour.png, 100%");
    await user.keyboard("{ArrowLeft}");
    expect(dialog).toHaveAccessibleName("lighthouse.jpg");
    await user.click(within(dialog).getByRole("button", { name: "Previous" }));
    expect(dialog).toHaveAccessibleName("tour.mp4");
  });

  it("pans a zoomed picture with the arrow keys instead of stepping", async () => {
    const { user, dialog } = await opened(items);
    await user.click(within(dialog).getByRole("button", { name: "Zoom in" }));
    await user.keyboard("{ArrowRight}");
    expect(dialog).toHaveAccessibleName("harbour.png");
    await user.keyboard("0{ArrowRight}");
    expect(dialog).toHaveAccessibleName("tour.mp4");
  });

  it("plays a video in the browser's own player, with no zoom", async () => {
    const { user, dialog } = await opened([items[1]!]);
    const player = dialog.querySelector("video");
    expect(player).toHaveAttribute("src", `/api/v1/attachments/${tour.id}?inline=1`);
    expect(player).toHaveAttribute("controls");
    expect(player).toHaveFocus();
    expect(within(dialog).queryByRole("button", { name: "Zoom in" })).toBeNull();
    await user.tab();
    expect(within(dialog).getByRole("link", { name: "Download tour.mp4" })).toHaveFocus();
    await user.tab();
    expect(within(dialog).getByRole("button", { name: "Close" })).toHaveFocus();
    await user.tab();
    expect(player).toHaveFocus();
  });

  it("says in a sentence when a file cannot be shown, and still offers the download", async () => {
    const { dialog } = await opened([items[1]!, items[0]!]);
    fireEvent.error(dialog.querySelector("video")!);
    expect(within(dialog).getByRole("alert")).toHaveTextContent("tour.mp4 cannot be played in this browser. Download it to play it on your device.");
    fireEvent.click(within(dialog).getByRole("button", { name: "Next" }));
    fireEvent.error(within(dialog).getByRole("img"));
    expect(within(dialog).getByRole("alert")).toHaveTextContent(
      "harbour.png could not be shown. Reload the page to see whether it is still there, or download it.",
    );
    expect(within(dialog).queryByRole("button", { name: "Zoom in" })).toBeNull();
    expect(within(dialog).getByRole("link", { name: "Download harbour.png" })).toBeInTheDocument();
  });
});

describe("where the lightbox opens", () => {
  it("opens a page's picture from its file list, stepping through the page's other media", async () => {
    stubApi({ [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [harbour, notes, tour, lighthouse] } } });
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <main>
          <h1>Plan</h1>
          <AttachmentPanel pageId={pageId} editable={false} />
        </main>
      </QueryClientProvider>,
    );
    expect(await screen.findByRole("link", { name: "Open tour.mp4" })).toHaveAttribute("href", `/api/v1/attachments/${tour.id}?inline=1`);
    expect(screen.queryByRole("button", { name: "Preview notes.txt" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Preview tour.mp4" }));
    const dialog = screen.getByRole("dialog", { name: "tour.mp4" });
    expect(within(dialog).getByText("2 of 3")).toBeInTheDocument();
  });

  const picture = "0195f000-0000-7000-8000-0000000000b1";
  const clip = "0195f000-0000-7000-8000-0000000000b2";
  const doc: Doc = {
    type: "doc",
    content: [
      { type: "image", attrs: { attachmentId: picture, alt: "The harbour at dawn", width: null } },
      { type: "paragraph", content: [{ type: "attachment", attrs: { attachmentId: clip, fileName: "tour.webm" } }] },
      { type: "paragraph", content: [{ type: "attachment", attrs: { attachmentId: notes.id, fileName: "notes.txt" } }] },
    ],
  };

  it("opens a page's picture and plays a video it names, through the reader's own reads", async () => {
    const user = userEvent.setup();
    render(
      <main>
        <h1>Plan</h1>
        <PublicReadingContext value="acme">
          <PublicLinkContext value="t0k3n">
            <DocView doc={doc} />
          </PublicLinkContext>
        </PublicReadingContext>
      </main>,
    );
    expect(screen.queryByRole("button", { name: "Play notes.txt" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "View The harbour at dawn larger" }));
    let dialog = screen.getByRole("dialog", { name: "The harbour at dawn" });
    expect(within(dialog).getByRole("img", { name: "The harbour at dawn" })).toHaveAttribute(
      "src",
      `/api/v1/public/acme/links/t0k3n/attachments/${picture}?inline=1`,
    );
    expect(within(dialog).getByRole("link", { name: "Download The harbour at dawn" })).toHaveAttribute(
      "href",
      `/api/v1/public/acme/links/t0k3n/attachments/${picture}`,
    );
    await user.keyboard("{Escape}");

    await user.click(screen.getByRole("button", { name: "Play tour.webm" }));
    dialog = screen.getByRole("dialog", { name: "tour.webm" });
    expect(dialog.querySelector("video")).toHaveAttribute("src", `/api/v1/public/acme/links/t0k3n/attachments/${clip}?inline=1`);
    expect(await axeViolations()).toEqual([]);
  });
});
