import { afterEach, describe, expect, it, vi } from "vitest";
import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor, type JSONContent } from "@tiptap/core";
import { Slice } from "@tiptap/pm/model";
import { NodeSelection } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import type { Attachment, Progress } from "@/api/attachments";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { stubUploads, upload as sentUpload } from "@/test/xhr";
import { createAttachmentIndex } from "./attachmentIndex";
import { attachmentNode, imageAlt, imageWidth } from "./attachments";
import { editorExtensions } from "./extensions";
import type { DocNode } from "./schema";

let editor: Editor | undefined;
afterEach(() => {
  editor?.destroy();
  editor = undefined;
  vi.unstubAllGlobals();
});

const pageId = "0195f000-0000-7000-8000-0000000000a1";
const stored = (over: Partial<Attachment>): Attachment => ({
  id: "0195f000-0000-7000-8000-0000000000f1",
  pageId,
  fileName: "screen.png",
  contentType: "image/png",
  size: 2048,
  width: 800,
  height: 600,
  uploadedByName: "Ada Lovelace",
  createdAt: "2026-09-29T09:00:00Z",
  version: 1,
  versions: 1,
  ...over,
});
const shot = stored({});
const plan = stored({ id: "0195f000-0000-7000-8000-0000000000f2", fileName: "plan.pdf", contentType: "application/pdf", width: null, height: null });

/** An upload the test answers by hand, with what it was sent. */
function manualUpload() {
  const calls: Array<{ file: File; progress: Progress; resolve: (made: Attachment | null) => void }> = [];
  const upload = (file: File, progress: Progress) => new Promise<Attachment | null>((resolve) => calls.push({ file, progress, resolve }));
  return { upload, calls };
}

function make(options: Parameters<typeof editorExtensions>[0], content: JSONContent = { type: "doc", content: [{ type: "paragraph" }] }) {
  const element = document.createElement("div");
  document.body.append(element);
  editor = new Editor({ element, extensions: editorExtensions(options), content });
  return editor;
}

function find(doc: DocNode, type: string): DocNode[] {
  const out: DocNode[] = doc.type === type ? [doc] : [];
  for (const child of doc.content ?? []) out.push(...find(child, type));
  return out;
}

function clipboard(files: File[], html = ""): DataTransfer {
  return { files, types: ["Files", ...(html ? ["text/html"] : [])], getData: (type: string) => (type === "text/html" ? html : "") } as unknown as DataTransfer;
}

function paste(e: Editor, data: DataTransfer): boolean {
  const event = { clipboardData: data } as unknown as ClipboardEvent;
  return Boolean(e.view.someProp("handlePaste", (f) => f(e.view as EditorView, event, Slice.empty)));
}

function drop(e: Editor, data: DataTransfer, moved = false): boolean {
  const event = { dataTransfer: data, clientX: 0, clientY: 0, preventDefault: () => {} } as unknown as DragEvent;
  return Boolean(e.view.someProp("handleDrop", (f) => f(e.view as EditorView, event, Slice.empty, moved)));
}

const png = () => new File(["png"], "screen.png", { type: "image/png" });
const pdf = () => new File(["%PDF"], "plan.pdf", { type: "application/pdf" });

describe("files in the editor", () => {
  it("uploads a pasted picture first, shows how far it is, and inserts it as an image on 201", async () => {
    const { upload, calls } = manualUpload();
    const e = make({ upload }, { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "before after" }] }] });
    e.commands.setTextSelection(8);
    expect(paste(e, clipboard([png()]))).toBe(true);
    expect(calls.map((c) => c.file.name)).toEqual(["screen.png"]);
    const placeholder = e.view.dom.querySelector("[data-upload-placeholder]");
    expect(placeholder).toHaveTextContent("Uploading screen.png, 0%");
    calls[0]?.progress(0.5);
    expect(placeholder).toHaveTextContent("Uploading screen.png, 50%");
    // Typing before it while it goes up moves where it lands.
    e.commands.insertContentAt(1, "Say ");
    calls[0]?.resolve(shot);
    await waitFor(() => expect(find(e.getJSON() as DocNode, "image")).toHaveLength(1));
    expect(e.getJSON().content).toEqual([
      { type: "paragraph", content: [{ type: "text", text: "Say before " }] },
      { type: "image", attrs: { attachmentId: shot.id, alt: null, width: null } },
      { type: "paragraph", content: [{ type: "text", text: "after" }] },
    ]);
    expect(e.view.dom.querySelector("[data-upload-placeholder]")).toBeNull();
    expect(e.view.dom.querySelector("figure[data-image] img")).toHaveAttribute("src", `/api/v1/attachments/${shot.id}?inline=1`);
  });

  it("inserts any other file as a chip in the line", async () => {
    const { upload, calls } = manualUpload();
    const e = make({ upload }, { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "See " }] }] });
    e.commands.setTextSelection(5);
    expect(paste(e, clipboard([pdf()]))).toBe(true);
    calls[0]?.resolve(plan);
    await waitFor(() => expect(find(e.getJSON() as DocNode, "attachment")).toHaveLength(1));
    expect(find(e.getJSON() as DocNode, "paragraph")[0]?.content?.[1]).toEqual({ type: "attachment", attrs: { attachmentId: plan.id, fileName: "plan.pdf" } });
    expect(e.view.dom.querySelector("[data-attachment-chip]")).toHaveTextContent("plan.pdf");
    expect(e.getText()).toContain("See plan.pdf");
  });

  it("takes dropped files too", async () => {
    const { upload, calls } = manualUpload();
    const e = make({ upload });
    expect(drop(e, clipboard([pdf()]))).toBe(true);
    calls[0]?.resolve(plan);
    await waitFor(() => expect(find(e.getJSON() as DocNode, "attachment")).toHaveLength(1));
  });

  it("leaves a moved selection, pasted markup and a refused file alone", async () => {
    const { upload, calls } = manualUpload();
    const e = make({ upload });
    expect(drop(e, clipboard([png()]), true)).toBe(false);
    expect(paste(e, clipboard([png()], "<table><tr><td>A copied table</td></tr></table>"))).toBe(false);
    expect(calls).toHaveLength(0);
    // A picture copied out of a browser brings markup with nothing to read in it.
    expect(paste(e, clipboard([png()], '<img src="https://elsewhere.test/a.png">'))).toBe(true);
    calls[0]?.resolve(null);
    await waitFor(() => expect(e.view.dom.querySelector("[data-upload-placeholder]")).toBeNull());
    expect(find(e.getJSON() as DocNode, "image")).toHaveLength(0);
  });

  it("takes no files at all when it has nowhere to send them", () => {
    const e = make({});
    expect(paste(e, clipboard([png()]))).toBe(false);
    expect(drop(e, clipboard([png()]))).toBe(false);
    expect(e.commands.uploadFiles([png()])).toBe(false);
  });

  it("uploads what the picker gives it at the caret", async () => {
    const { upload, calls } = manualUpload();
    const e = make({ upload });
    expect(e.commands.uploadFiles([png(), pdf()])).toBe(true);
    expect(calls.map((c) => c.file.name)).toEqual(["screen.png", "plan.pdf"]);
    calls[1]?.resolve(plan);
    calls[0]?.resolve(shot);
    await waitFor(() => expect(find(e.getJSON() as DocNode, "image")).toHaveLength(1));
    await waitFor(() => expect(find(e.getJSON() as DocNode, "attachment")).toHaveLength(1));
  });

  it("keeps only pictures of this site's attachments when markup is pasted", () => {
    const e = make({});
    e.commands.setContent(
      `<p>x</p><img src="https://elsewhere.test/a.png" alt="hotlinked"><img data-attachment-id="${shot.id}" alt="ours" width="300"><img data-attachment-id="not-an-id">`,
    );
    expect(find(e.getJSON() as DocNode, "image")).toEqual([{ type: "image", attrs: { attachmentId: shot.id, alt: "ours", width: 300 } }]);
  });

  it("draws a file the page no longer has as missing, and follows the list as it changes", () => {
    const index = createAttachmentIndex();
    const e = make(
      { attachments: index },
      {
        type: "doc",
        content: [
          { type: "paragraph", content: [{ type: "attachment", attrs: { attachmentId: plan.id, fileName: "plan.pdf" } }] },
          { type: "image", attrs: { attachmentId: shot.id, alt: "Screen", width: 240 } },
        ],
      },
    );
    const chip = () => e.view.dom.querySelector("[data-attachment-chip]");
    expect(chip()).toHaveTextContent("plan.pdf");
    expect(e.view.dom.querySelector("figure[data-image] img")).toHaveStyle({ width: "240px" });
    act(() => index.set(new Set([shot.id])));
    expect(chip()).toHaveTextContent("plan.pdf (file deleted)");
    expect(chip()).toHaveAttribute("data-missing");
    expect(e.view.dom.querySelector("figure[data-image] img")).not.toBeNull();
    act(() => index.set(new Set([plan.id])));
    expect(chip()).not.toHaveAttribute("data-missing");
    expect(e.view.dom.querySelector("figure[data-image] img")).toBeNull();
    expect(e.view.dom.querySelector("[data-image-missing]")).toHaveTextContent("This image was deleted.");
  });

  it("sets alternative text and width only as the API takes them", () => {
    expect(imageWidth(480)).toBe(480);
    expect(imageWidth("300")).toBe(300);
    for (const bad of [0, -1, 4001, 1.5, "wide", null]) expect(imageWidth(bad), String(bad)).toBeNull();
    expect(imageAlt("")).toBeNull();
    expect([...(imageAlt("a".repeat(600)) ?? "")]).toHaveLength(500);
    expect(attachmentNode(plan)).toEqual({ type: "attachment", attrs: { attachmentId: plan.id, fileName: "plan.pdf" } });
    expect(attachmentNode(stored({ contentType: "image/svg+xml" })).type).toBe("attachment");
  });
});

describe("files in the page editor", () => {
  const space = aSpace();
  const page = aPage({
    id: pageId,
    title: "Notes",
    home: false,
    parentId: space.homePageId,
    ancestors: [{ id: space.homePageId, title: "Handbook", home: true }],
  });

  function stubEditor() {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/outline": { status: 200, body: { pages: [] } },
      [`GET /pages/${pageId}`]: { status: 200, body: { page, space } },
      [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [shot] } },
      [`GET /pages/${pageId}/draft`]: { status: 200, body: { draft: null } },
      // The editor autosaves a draft once a picture goes in.
      [`PUT /pages/${pageId}/draft`]: {
        status: 200,
        body: { draft: { pageId, title: page.title, body: page.body, baseVersion: page.version, updatedAt: "2026-09-29T10:00:00Z" } },
      },
    });
    return stubUploads();
  }

  async function pageEditor(): Promise<EditorView> {
    await renderAt(`/s/DOCS/p/${pageId}/notes/edit`);
    const box = await screen.findByRole("textbox", { name: "Page content" }, { timeout: 5000 });
    const view = (box as HTMLElement & { editor?: Editor }).editor?.view;
    if (!view) throw new Error("The page editor has no editor view.");
    return view;
  }

  it("sends a pasted picture to the page and draws it once stored", async () => {
    const uploads = stubEditor();
    const view = await pageEditor();
    act(() => void view.someProp("handlePaste", (f) => f(view, { clipboardData: clipboard([png()]) } as unknown as ClipboardEvent, Slice.empty)));
    expect(sentUpload(uploads, 0).url).toBe(`/api/v1/pages/${pageId}/attachments`);
    act(() => sentUpload(uploads, 0).progress(1, 4));
    expect(view.dom.querySelector("[data-upload-placeholder]")).toHaveTextContent("Uploading screen.png, 25%");
    act(() => sentUpload(uploads, 0).respond(201, { attachment: shot }));
    await waitFor(() => expect(view.dom.querySelector("figure[data-image] img")).toHaveAttribute("src", `/api/v1/attachments/${shot.id}?inline=1`));
  });

  it("offers alternative text and width for a selected picture, and passes axe", async () => {
    const uploads = stubEditor();
    const view = await pageEditor();
    act(() => void view.someProp("handlePaste", (f) => f(view, { clipboardData: clipboard([png()]) } as unknown as ClipboardEvent, Slice.empty)));
    act(() => sentUpload(uploads, 0).respond(201, { attachment: shot }));
    await waitFor(() => expect(view.dom.querySelector("figure[data-image]")).not.toBeNull());
    let at = -1;
    view.state.doc.descendants((node, pos) => {
      if (node.type.name === "image") at = pos;
    });
    act(() => view.dispatch(view.state.tr.setSelection(NodeSelection.create(view.state.doc, at))));
    const alt = await screen.findByRole("textbox", { name: "Alternative text" });
    await userEvent.type(alt, "The login screen");
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Width" }), "Medium");
    const image = view.state.doc.nodeAt(at);
    expect(image?.attrs).toEqual({ attachmentId: shot.id, alt: "The login screen", width: 480 });
    expect(view.dom.querySelector("figure[data-image] img")).toHaveAttribute("alt", "The login screen");
    expect(screen.getByRole("button", { name: "Attach files" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says why a picture was refused, above the editor", async () => {
    const uploads = stubEditor();
    const view = await pageEditor();
    act(() => void view.someProp("handlePaste", (f) => f(view, { clipboardData: clipboard([png()]) } as unknown as ClipboardEvent, Slice.empty)));
    act(() => sentUpload(uploads, 0).respond(413, { error: { code: "too_large", message: "A file may be at most 50 MB." } }));
    expect(await screen.findByRole("alert")).toHaveTextContent("screen.png (3 B) is larger than this site accepts. A file may be at most 50 MB.");
    expect(view.dom.querySelector("[data-upload-placeholder]")).toBeNull();
  });
});
