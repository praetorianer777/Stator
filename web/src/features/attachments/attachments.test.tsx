import { afterEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Attachment } from "@/api/attachments";
import type { Doc } from "@/features/editor/schema";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { stubUploads, upload } from "@/test/xhr";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const space = aSpace();
const home = aPage();
const pageId = "0195f000-0000-7000-8000-0000000000a1";
const file = (over: Partial<Attachment>): Attachment => ({
  id: "0195f000-0000-7000-8000-0000000000f1",
  pageId,
  fileName: "plan.pdf",
  contentType: "application/pdf",
  size: 3 * 1024 * 1024,
  width: null,
  height: null,
  uploadedByName: "Ada Lovelace",
  createdAt: "2026-09-29T09:00:00Z",
  ...over,
});
const plan = file({});
const shot = file({ id: "0195f000-0000-7000-8000-0000000000f2", fileName: "screen.png", contentType: "image/png", size: 2048, width: 800, height: 600 });
const archive = file({ id: "0195f000-0000-7000-8000-0000000000f3", fileName: "logs.zip", contentType: "application/zip", size: 512 });
const gone = "0195f000-0000-7000-8000-0000000000ff";

const body: Doc = {
  type: "doc",
  content: [
    {
      type: "paragraph",
      content: [
        { type: "text", text: "See " },
        { type: "attachment", attrs: { attachmentId: plan.id, fileName: "plan.pdf" } },
      ],
    },
    { type: "image", attrs: { attachmentId: shot.id, alt: "The login screen", width: 480 } },
    { type: "paragraph", content: [{ type: "attachment", attrs: { attachmentId: gone, fileName: "old.pdf" } }] },
    { type: "image", attrs: { attachmentId: gone, alt: "An old picture", width: null } },
  ],
};

function stubPage({
  edit = true,
  attachments = [plan, shot, archive],
  more = {},
}: {
  edit?: boolean;
  attachments?: Attachment[];
  more?: Record<string, Answer | ((request: Request) => Answer)>;
} = {}) {
  let list = attachments;
  const page = aPage({
    id: pageId,
    title: "Notes",
    home: false,
    parentId: home.id,
    body,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    can: { edit, delete: edit, restrict: edit, comment: true },
  });
  const where = aSpace({ can: { ...space.can, editPages: edit } });
  const sent = stubApi({
    "GET /spaces": { status: 200, body: { spaces: [where] } },
    "GET /spaces/DOCS": { status: 200, body: { space: where } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    "GET /spaces/DOCS/outline": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page, space: where } },
    [`GET /pages/${pageId}/attachments`]: () => ({ status: 200, body: { attachments: list } }),
    ...more,
  });
  return { sent, setList: (next: Attachment[]) => (list = next) };
}

const panel = async () => within(await screen.findByRole("region", { name: /Attachments/ }));

describe("the attachments of a page", () => {
  it("lists every file with its size and who attached it, and links to the bytes", async () => {
    stubPage();
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    const list = await panel();
    expect(await list.findByRole("heading", { name: "Attachments (3)" })).toBeInTheDocument();
    const row = (await list.findByText("plan.pdf")).closest("li")!;
    expect(row).toHaveTextContent("3.0 MB");
    expect(row).toHaveTextContent("Ada Lovelace");
    expect(within(row).getByRole("link", { name: "Open plan.pdf" })).toHaveAttribute("href", `/api/v1/attachments/${plan.id}?inline=1`);
    const download = within(row).getByRole("link", { name: "Download plan.pdf" });
    expect(download).toHaveAttribute("href", `/api/v1/attachments/${plan.id}`);
    expect(download).toHaveAttribute("download", "plan.pdf");
    const zip = list.getByText("logs.zip").closest("a")!;
    expect(zip).toHaveAccessibleName("Download logs.zip");
    expect(zip).not.toHaveAttribute("target");
    expect(await axeViolations()).toEqual([]);
  });

  it("draws the document's pictures and files, and the deleted ones as missing", async () => {
    stubPage();
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    const doc = await screen.findByText((_, el) => el?.hasAttribute("data-doc") ?? false);
    await waitFor(() => expect(within(doc).getByText("old.pdf (file deleted)")).toBeInTheDocument());
    const picture = within(doc).getByRole("img", { name: "The login screen" });
    expect(picture).toHaveAttribute("src", `/api/v1/attachments/${shot.id}?inline=1`);
    expect(picture).toHaveStyle({ width: "480px" });
    expect(within(doc).getByRole("link", { name: "Download plan.pdf" })).toHaveAttribute("href", `/api/v1/attachments/${plan.id}`);
    expect(within(doc).queryByRole("img", { name: "An old picture" })).toBeNull();
    expect(within(doc).getByText("This image was deleted. Remove it from the page, or attach the picture again.")).toBeInTheDocument();

    // A picture whose bytes do not come is drawn missing too.
    fireEvent.error(picture);
    expect(within(doc).queryByRole("img", { name: "The login screen" })).toBeNull();
  });

  it("uploads picked files side by side, with progress, and lists them when stored", async () => {
    const { setList } = stubPage({ attachments: [] });
    const uploads = stubUploads();
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    const list = await panel();
    expect(await list.findByText("Nothing attached yet. Drop files here, or choose them with Attach files.")).toBeInTheDocument();
    const input = document.querySelector<HTMLInputElement>("[data-attachment-input]")!;
    await userEvent.upload(input, [new File(["%PDF"], "plan.pdf", { type: "application/pdf" }), new File(["zip"], "logs.zip", { type: "application/zip" })]);
    expect(uploads.map((u) => u.file.name)).toEqual(["plan.pdf", "logs.zip"]);
    expect(uploads[0]?.url).toBe(`/api/v1/pages/${pageId}/attachments`);

    act(() => upload(uploads, 0).progress(40, 100));
    expect(list.getByRole("progressbar", { name: "Uploading plan.pdf, 40%" })).toHaveAttribute("value", "40");
    expect(list.getByRole("progressbar", { name: "Uploading logs.zip, 0%" })).toBeInTheDocument();

    setList([plan]);
    act(() => upload(uploads, 0).respond(201, { attachment: plan }));
    expect(await list.findByText("plan.pdf", { selector: "a" })).toBeInTheDocument();
    expect(list.queryByRole("progressbar", { name: /plan\.pdf/ })).toBeNull();
    expect(await screen.findByText("Attached plan.pdf.")).toBeInTheDocument();
    setList([archive, plan]);
    act(() => upload(uploads, 1).respond(201, { attachment: archive }));
    expect(await list.findByRole("heading", { name: "Attachments (2)" })).toBeInTheDocument();
    expect(list.queryByRole("progressbar")).toBeNull();
  });

  it("takes files dropped on the panel", async () => {
    stubPage({ attachments: [] });
    const uploads = stubUploads();
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    const region = await screen.findByRole("region", { name: /Attachments/ });
    const dropped = new File(["png"], "screen.png", { type: "image/png" });
    const dataTransfer = { types: ["Files"], files: [dropped], dropEffect: "none" };
    fireEvent.dragEnter(region, { dataTransfer });
    expect(within(region).getByText("Drop files here to attach them to the page.")).toBeInTheDocument();
    fireEvent.dragOver(region, { dataTransfer });
    fireEvent.drop(region, { dataTransfer });
    expect(uploads.map((u) => u.file.name)).toEqual(["screen.png"]);
    expect(within(region).queryByText("Drop files here to attach them to the page.")).toBeNull();
  });

  it("says what is wrong with a file too large, and what to do about it", async () => {
    stubPage({ attachments: [] });
    const uploads = stubUploads();
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    await panel();
    const input = document.querySelector<HTMLInputElement>("[data-attachment-input]")!;
    await userEvent.upload(input, new File(["x".repeat(3000)], "huge.iso", { type: "application/octet-stream" }));
    act(() => upload(uploads, 0).respond(413, { error: { code: "too_large", message: "A file may be at most 50 MB." } }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "huge.iso (2.9 KB) is larger than this site accepts. A file may be at most 50 MB. Make the file smaller, or split it into parts, and attach it again.",
    );
    expect(await axeViolations()).toEqual([]);

    // The next attempt starts clean.
    await userEvent.upload(input, new File(["ok"], "small.txt", { type: "text/plain" }));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("deletes a file for good, only after asking", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValue(true);
    const { sent, setList } = stubPage({
      more: { [`DELETE /attachments/${archive.id}`]: { status: 204 } },
    });
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    const list = await panel();
    const remove = await list.findByRole("button", { name: "Delete logs.zip" });
    await userEvent.click(remove);
    expect(confirm).toHaveBeenLastCalledWith(
      "Delete logs.zip for good? It cannot be restored, and the page and its older versions will show it as a missing file.",
    );
    expect(sent.some((r) => r.method === "DELETE")).toBe(false);
    setList([plan, shot]);
    await userEvent.click(remove);
    await waitFor(() => expect(list.queryByText("logs.zip")).toBeNull());
    expect(sent.some((r) => r.method === "DELETE" && r.path === `/attachments/${archive.id}`)).toBe(true);
    expect(await screen.findByText("Deleted logs.zip.")).toBeInTheDocument();
  });

  it("shows a reader who may not edit the files, but no way to add or delete them", async () => {
    stubPage({ edit: false });
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    const list = await panel();
    expect(await list.findByText("plan.pdf")).toBeInTheDocument();
    expect(list.queryByRole("button", { name: "Attach files" })).toBeNull();
    expect(list.queryByRole("button", { name: /Delete/ })).toBeNull();
    expect(document.querySelector("[data-attachment-input]")).toBeNull();
    const region = await screen.findByRole("region", { name: /Attachments/ });
    fireEvent.drop(region, { dataTransfer: { types: ["Files"], files: [new File(["x"], "x.txt")] } });
    expect(list.queryByRole("progressbar")).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });
});
