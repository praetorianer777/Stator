import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Attachment } from "@/api/attachments";
import { GALLERY_CAPTION_MAX_LENGTH, GALLERY_COLUMNS, GALLERY_DEFAULT_COLUMNS, GALLERY_MAX_IMAGES } from "@/config";
import { KnownAttachmentsContext } from "@/features/editor/attachmentIndex";
import { DocView } from "@/features/editor/DocView";
import { PublicReadingContext } from "@/features/editor/publicReading";
import type { Doc, DocNode } from "@/features/editor/schema";
import { stubApi } from "@/test/app";
import { allowlist, problems } from "@/test/allowlist";
import { axeViolations } from "@/test/axe";
import { stubUploads, upload } from "@/test/xhr";
import { addPictures, galleryJSON, galleryOf, movePicture, type GallerySettings } from "./gallery";
import { GalleryDialog } from "./GalleryDialog";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const pageId = "0195f000-0000-7000-8000-0000000000a1";
const harbourId = "0195f000-0000-7000-8000-0000000000f1";
const lighthouseId = "0195f000-0000-7000-8000-0000000000f2";
const pierId = "0195f000-0000-7000-8000-0000000000f3";

const file = (over: Partial<Attachment>): Attachment => ({
  id: harbourId,
  pageId,
  fileName: "harbour.png",
  contentType: "image/png",
  size: 2048,
  width: 640,
  height: 480,
  uploadedByName: "Ada Lovelace",
  createdAt: "2026-10-07T09:00:00Z",
  version: 1,
  versions: 1,
  restoredFrom: null,
  preview: "none",
  ...over,
});

const gallery = (columns: number, ...pictures: [string, string | null][]) => ({
  type: "gallery",
  attrs: { columns },
  content: pictures.map(([attachmentId, caption]) => ({ type: "galleryImage", attrs: { attachmentId, caption } })),
});

describe("a gallery's settings", () => {
  it("are read from the node, leaving out pictures of no file and a row the API would refuse", () => {
    const node = {
      attrs: { columns: 7 },
      content: [
        { type: "galleryImage", attrs: { attachmentId: harbourId, caption: "  " } },
        { type: "galleryImage", attrs: { attachmentId: "../etc/passwd", caption: "x" } },
        { type: "image", attrs: { attachmentId: pierId } },
        { type: "galleryImage", attrs: { attachmentId: lighthouseId, caption: "The lighthouse" } },
      ],
    };
    expect(galleryOf(node)).toEqual({
      columns: GALLERY_DEFAULT_COLUMNS,
      pictures: [
        { attachmentId: harbourId, caption: null },
        { attachmentId: lighthouseId, caption: "The lighthouse" },
      ],
    });
  });

  it("are written as a node the server takes, or as nothing without a picture", () => {
    expect(galleryJSON({ columns: 2, pictures: [] })).toBeNull();
    const long = "a".repeat(GALLERY_CAPTION_MAX_LENGTH + 5);
    const many = Array.from({ length: GALLERY_MAX_IMAGES + 3 }, () => ({ attachmentId: harbourId, caption: long }));
    const json = galleryJSON({ columns: 4, pictures: many })!;
    expect(json.content).toHaveLength(GALLERY_MAX_IMAGES);
    expect(json.content?.[0]?.attrs?.caption).toHaveLength(GALLERY_CAPTION_MAX_LENGTH);
    expect(problems({ type: "doc", content: [json as DocNode] })).toEqual([]);
  });

  it("take the server's bounds", () => {
    const spec = allowlist.nodes.gallery!;
    expect([spec.attrs?.columns?.min, spec.attrs?.columns?.max]).toEqual([Math.min(...GALLERY_COLUMNS), Math.max(...GALLERY_COLUMNS)]);
    expect([spec.minContent, spec.maxContent]).toEqual([1, GALLERY_MAX_IMAGES]);
    expect(allowlist.nodes.galleryImage?.attrs?.caption?.maxLength).toBe(GALLERY_CAPTION_MAX_LENGTH);
  });

  it("move a picture within the list, and add each file once up to the most a gallery holds", () => {
    expect(movePicture(["a", "b", "c"], 2, 0)).toEqual(["c", "a", "b"]);
    expect(movePicture(["a", "b", "c"], 0, 3)).toEqual(["a", "b", "c"]);
    const one = [{ attachmentId: harbourId, caption: "Kept" }];
    expect(addPictures(one, [harbourId, pierId, pierId])).toEqual([...one, { attachmentId: pierId, caption: null }]);
    const full = Array.from({ length: GALLERY_MAX_IMAGES }, (_, i) => ({ attachmentId: `id-${i}`, caption: null }));
    expect(addPictures(full, [pierId])).toHaveLength(GALLERY_MAX_IMAGES);
  });
});

describe("a gallery in a page", () => {
  const doc: Doc = {
    type: "doc",
    content: [gallery(2, [harbourId, "The harbour"], [pierId, "A pier of another page"], [lighthouseId, null])] as Doc["content"],
  };

  function shown(known?: Set<string>) {
    return render(
      <main>
        <h1>Plan</h1>
        <PublicReadingContext value="acme">
          <KnownAttachmentsContext value={known}>
            <DocView doc={doc} />
          </KnownAttachmentsContext>
        </PublicReadingContext>
      </main>,
    );
  }

  it("shows the pictures the reader may download and steps through them in the lightbox", async () => {
    const user = userEvent.setup();
    shown(new Set([harbourId, lighthouseId]));
    const list = screen.getByRole("list", { name: "Gallery of 2 pictures" });
    expect(list).toHaveAttribute("data-columns", "2");
    expect(within(list).getAllByRole("listitem")).toHaveLength(2);
    expect(within(list).queryByText("A pier of another page")).toBeNull();
    expect(within(list).getByText("The harbour").tagName).toBe("FIGCAPTION");
    expect(list.querySelector("img")).toHaveAttribute("src", `/api/v1/public/acme/attachments/${harbourId}?inline=1`);
    expect(list.querySelector("img")).toHaveAttribute("loading", "lazy");

    await user.click(screen.getByRole("button", { name: "View picture larger, 2 of 2" }));
    const dialog = screen.getByRole("dialog", { name: "Picture" });
    expect(within(dialog).getByText("2 of 2")).toBeInTheDocument();
    await user.keyboard("{ArrowRight}");
    expect(dialog).toHaveAccessibleName("The harbour");
    expect(within(dialog).getByRole("img", { name: "The harbour" })).toHaveAttribute("src", `/api/v1/public/acme/attachments/${harbourId}?inline=1`);
    await user.click(within(dialog).getByRole("button", { name: "Previous" }));
    expect(dialog).toHaveAccessibleName("Picture");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("leaves out a picture the reader's read refuses, and says so when none is left", () => {
    shown();
    const list = screen.getByRole("list", { name: "Gallery of 3 pictures" });
    // Each refusal draws the gallery again without that picture.
    for (let img = list.querySelector("img"); img; img = screen.queryByRole("list")?.querySelector("img") ?? null) fireEvent.error(img);
    expect(screen.queryByRole("list")).toBeNull();
    expect(screen.getByText("None of this gallery's pictures can be shown to you.")).toBeInTheDocument();
  });
});

describe("the gallery dialog", () => {
  const harbour = file({});
  const lighthouse = file({ id: lighthouseId, fileName: "lighthouse.jpg", contentType: "image/jpeg", createdAt: "2026-10-07T10:00:00Z" });
  const oldLighthouse = file({ id: "0195f000-0000-7000-8000-0000000000f9", fileName: "lighthouse.jpg", contentType: "image/jpeg" });
  const notes = file({ id: "0195f000-0000-7000-8000-0000000000f4", fileName: "notes.txt", contentType: "text/plain", width: null, height: null });

  function opened(initial: GallerySettings, onSave = vi.fn()) {
    stubApi({ [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [lighthouse, notes, harbour, oldLighthouse] } } });
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <main>
          <h1>Plan</h1>
          <GalleryDialog initial={initial} pageId={pageId} isNew onSave={onSave} onClose={() => {}} />
        </main>
      </QueryClientProvider>,
    );
    return { user: userEvent.setup(), onSave, dialog: screen.getByRole("dialog", { name: "Insert a gallery" }) };
  }

  it("offers the latest of each picture, and saves the chosen ones in order with captions and a row", async () => {
    const { user, onSave, dialog } = opened({ columns: GALLERY_DEFAULT_COLUMNS, pictures: [] });
    const offered = await within(dialog).findByRole("checkbox", { name: "lighthouse.jpg" });
    expect(
      within(dialog)
        .getAllByRole("checkbox")
        .map((box) => box.getAttribute("data-gallery-offer")),
    ).toEqual(["lighthouse.jpg", "harbour.png"]);
    await user.click(within(dialog).getByRole("button", { name: "Insert gallery" }));
    expect(within(dialog).getByText("Choose at least one picture, or upload one, to make a gallery.")).toBeInTheDocument();
    expect(onSave).not.toHaveBeenCalled();

    await user.click(offered);
    await user.click(within(dialog).getByRole("checkbox", { name: "harbour.png" }));
    await user.click(within(dialog).getByRole("button", { name: "Move harbour.png earlier" }));
    await user.type(within(dialog).getByRole("textbox", { name: "Caption for harbour.png" }), "The harbour");
    await user.selectOptions(within(dialog).getByRole("combobox", { name: "Pictures in a row" }), "2");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Insert gallery" }));
    expect(onSave).toHaveBeenCalledWith({
      columns: 2,
      pictures: [
        { attachmentId: harbourId, caption: "The harbour" },
        { attachmentId: lighthouseId, caption: null },
      ],
    });
  });

  it("keeps a picture the gallery had, even an earlier version, and takes one out", async () => {
    const { user, onSave, dialog } = opened({
      columns: 4,
      pictures: [
        { attachmentId: oldLighthouse.id, caption: "Before" },
        { attachmentId: harbourId, caption: null },
        { attachmentId: oldLighthouse.id, caption: "Twice" },
      ],
    });
    await within(dialog).findByRole("checkbox", { name: "harbour.png" });
    expect(within(dialog).getByRole("checkbox", { name: "lighthouse.jpg" })).not.toBeChecked();
    expect(within(dialog).getByText("In the gallery: 2 of at most 60")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Remove harbour.png from the gallery" }));
    await user.click(within(dialog).getByRole("button", { name: "Insert gallery" }));
    expect(onSave).toHaveBeenCalledWith({ columns: 4, pictures: [{ attachmentId: oldLighthouse.id, caption: "Before" }] });
  });

  it("puts uploaded pictures in the gallery, and says why one was refused", async () => {
    const sent = stubUploads();
    const { user, dialog } = opened({ columns: 3, pictures: [] });
    await within(dialog).findByRole("checkbox", { name: "harbour.png" });
    const input = dialog.querySelector<HTMLInputElement>("[data-gallery-upload]")!;
    await user.upload(input, [new File(["png"], "pier.png", { type: "image/png" }), new File(["x"], "huge.png", { type: "image/png" })]);
    expect(within(dialog).getByRole("button", { name: "Insert gallery" })).toBeDisabled();
    await act(async () => {
      upload(sent, 0).respond(201, { attachment: file({ id: pierId, fileName: "pier.png" }) });
      upload(sent, 1).respond(413, { error: { code: "too_large", message: "Files may be at most 1 MB." } });
    });
    expect(await within(dialog).findByRole("textbox", { name: "Caption for pier.png" })).toBeInTheDocument();
    expect(within(dialog).getByText(/huge\.png/)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Insert gallery" })).toBeEnabled();
  });
});
