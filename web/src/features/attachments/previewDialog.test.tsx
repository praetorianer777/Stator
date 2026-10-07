import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { hasPreview, previewUrl, type Attachment } from "@/api/attachments";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { AttachmentList } from "./AttachmentList";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const pageId = "0195f000-0000-7000-8000-0000000000a1";
const file = (over: Partial<Attachment>): Attachment => ({
  id: "0195f000-0000-7000-8000-0000000000f1",
  pageId,
  fileName: "budget.xlsx",
  contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  size: 2048,
  width: null,
  height: null,
  uploadedByName: "Ada Lovelace",
  createdAt: "2026-09-29T09:00:00Z",
  version: 2,
  versions: 2,
  restoredFrom: null,
  editedFrom: null,
  preview: "office",
  ...over,
});
const budget = file({});
const older = file({ id: "0195f000-0000-7000-8000-0000000000f2", version: 1 });
const report = file({
  id: "0195f000-0000-7000-8000-0000000000f3",
  fileName: "report.pdf",
  contentType: "application/pdf",
  version: 1,
  versions: 1,
  preview: "pdf",
});
const notes = file({ id: "0195f000-0000-7000-8000-0000000000f4", fileName: "notes.txt", contentType: "text/plain", version: 1, versions: 1, preview: "none" });

function shown() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Plan</h1>
        <AttachmentList pageId={pageId} editable={false} />
      </main>
    </QueryClientProvider>,
  );
}

/** jsdom has no object URLs; these stand in and say which were let go. */
function stubObjectUrls() {
  const revoked: string[] = [];
  let next = 0;
  Object.assign(URL, {
    createObjectURL: () => `blob:preview-${++next}`,
    revokeObjectURL: (url: string) => void revoked.push(url),
  });
  return revoked;
}

describe("which files have a preview", () => {
  it("are the PDFs and the office documents the server converts", () => {
    expect([budget, report, notes].map(hasPreview)).toEqual([true, true, false]);
    expect(previewUrl(budget.id)).toBe(`/api/v1/attachments/${budget.id}/preview`);
  });
});

describe("a file's preview", () => {
  it("shows the PDF in a frame of its own, and lets it go on closing", async () => {
    const sent = stubApi({
      [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [budget, report, notes, older] } },
      [`GET /attachments/${budget.id}/preview`]: { status: 200, body: "%PDF-1.7" },
    });
    const revoked = stubObjectUrls();
    const user = userEvent.setup();
    shown();
    const list = await screen.findByRole("region", { name: "Files on this page" });
    await within(list).findByRole("button", { name: "Preview budget.xlsx" });
    expect(within(list).getByRole("button", { name: "Preview report.pdf" })).toBeInTheDocument();
    expect(within(list).queryByRole("button", { name: "Preview notes.txt" })).toBeNull();

    await user.click(within(list).getByRole("button", { name: "Preview budget.xlsx" }));
    const dialog = await screen.findByRole("dialog", { name: "Preview of budget.xlsx" });
    const frame = await within(dialog).findByTitle("budget.xlsx, shown as a PDF");
    expect(frame).toHaveAttribute("src", "blob:preview-1");
    expect(sent.filter((r) => r.path.endsWith("/preview")).map((r) => r.path)).toEqual([`/attachments/${budget.id}/preview`]);
    expect(within(dialog).getByRole("link", { name: "Open in a new tab" })).toHaveAttribute("href", `/api/v1/attachments/${budget.id}/preview`);
    expect(within(dialog).getByRole("link", { name: "Download budget.xlsx" })).toHaveAttribute("download", "budget.xlsx");

    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(revoked).toEqual(["blob:preview-1"]);
  });

  it("says while an office document is converted, and shows a refusal as the server's sentence", async () => {
    let answer: (value: { status: number; body: unknown }) => void = () => {};
    stubApi({
      [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [budget, older] } },
      [`GET /attachments/${older.id}/preview`]: () => new Promise((resolve) => (answer = resolve)),
    });
    stubObjectUrls();
    const user = userEvent.setup();
    shown();
    const list = await screen.findByRole("region", { name: "Files on this page" });
    await user.click(await within(list).findByText("1 earlier version"));
    await user.click(within(list).getByRole("button", { name: "Preview budget.xlsx, version 1" }));
    const dialog = await screen.findByRole("dialog", { name: "Preview of budget.xlsx" });
    expect(within(dialog).getByRole("status")).toHaveTextContent("Converting budget.xlsx for its preview.");
    expect(within(dialog).queryByRole("link", { name: "Open in a new tab" })).toBeNull();
    // Before the frame, which axe cannot enter in jsdom.
    expect(await axeViolations()).toEqual([]);

    const refusal = "This file could not be converted for a preview. Download it to open it, or upload it again saved in another format.";
    answer({ status: 422, body: { error: { code: "preview_failed", message: refusal } } });
    await waitFor(() => expect(within(dialog).getByText(refusal)).toBeInTheDocument());
    expect(within(dialog).queryByTitle("budget.xlsx, shown as a PDF")).toBeNull();
    expect(within(dialog).getByRole("link", { name: "Download budget.xlsx" })).toBeInTheDocument();
  });
});
