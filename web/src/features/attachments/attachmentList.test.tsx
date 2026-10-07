import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Attachment } from "@/api/attachments";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { allowlist } from "@/test/allowlist";
import { stubUploads, upload } from "@/test/xhr";
import { AttachmentList } from "./AttachmentList";
import { byName, versionNote } from "./versions";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
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
  version: 1,
  versions: 1,
  restoredFrom: null,
  editedFrom: null,
  preview: "office",
  ...over,
});
const third = file({ id: "0195f000-0000-7000-8000-0000000000f3", version: 3, versions: 2, uploadedByName: "Grace Hopper", createdAt: "2026-10-02T09:00:00Z" });
const notes = file({
  id: "0195f000-0000-7000-8000-0000000000f2",
  fileName: "notes.txt",
  contentType: "text/plain",
  size: 12,
  createdAt: "2026-10-01T09:00:00Z",
  preview: "none",
});
const first = file({ versions: 2, fileName: "Budget.XLSX" });

function shown(editable: boolean) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Plan</h1>
        <AttachmentList pageId={pageId} editable={editable} />
      </main>
    </QueryClientProvider>,
  );
}

describe("files by name", () => {
  it("put a name's versions under its latest, whatever the case, in the order the page lists them", () => {
    expect(byName([third, notes, first]).map((g) => [g.latest.id, g.earlier.map((e) => e.id)])).toEqual([
      [third.id, [first.id]],
      [notes.id, []],
    ]);
    expect(allowlist.nodes.attachmentList).toBeDefined();
  });
});

describe("the list of files", () => {
  it("shows each file's latest version with its size, uploader and version, and the earlier ones on asking", async () => {
    stubApi({ [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [third, notes, first] } } });
    const user = userEvent.setup();
    shown(false);
    const list = await screen.findByRole("region", { name: "Files on this page" });
    const rows = await within(list).findAllByRole("listitem");
    expect(rows.filter((li) => li.hasAttribute("data-listed-file")).map((li) => li.getAttribute("data-listed-file"))).toEqual(["budget.xlsx", "notes.txt"]);
    const budget = rows[0]!;
    expect(budget).toHaveTextContent(/2.0 KB · Version 3 · Grace Hopper, Oct 2, 2026/);
    expect(within(budget).getByRole("link", { name: "Download budget.xlsx" })).toBeInTheDocument();
    expect(within(list).queryByRole("button", { name: "Upload files" })).toBeNull();
    await user.click(within(budget).getByText("1 earlier version"));
    expect(within(budget).getByRole("link", { name: "Download Budget.XLSX, version 1" })).toBeVisible();
    expect(await axeViolations()).toEqual([]);
  });

  it("uploads from the block, and says which version a file became", async () => {
    stubApi({ [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [notes] } } });
    const uploads = stubUploads();
    const user = userEvent.setup();
    shown(true);
    const list = await screen.findByRole("region", { name: "Files on this page" });
    await within(list).findByRole("link", { name: "Open notes.txt" });
    expect(within(list).getByText("A file with the name of one already here becomes its next version.")).toBeInTheDocument();
    const input = list.querySelector<HTMLInputElement>("[data-attachment-list-input]")!;
    await user.upload(input, new File(["new"], "notes.txt", { type: "text/plain" }));
    expect(upload(uploads, 0).url).toBe(`/api/v1/pages/${pageId}/attachments`);
    act(() => upload(uploads, 0).respond(201, { attachment: { ...notes, id: "0195f000-0000-7000-8000-0000000000f4", version: 2, versions: 2 } }));
    await waitFor(() => expect(within(list).getByRole("status")).toHaveTextContent("Uploaded notes.txt as version 2."));
  });

  it("restores an earlier version for whoever may edit, and says what the restore refused", async () => {
    const restored = file({ id: "0195f000-0000-7000-8000-0000000000f5", version: 4, versions: 3, restoredFrom: 1 });
    let listed = [third, first];
    const sent = stubApi({
      [`GET /pages/${pageId}/attachments`]: () => ({ status: 200, body: { attachments: listed } }),
      [`POST /attachments/${first.id}/restore`]: () => {
        listed = [restored, third, first];
        return { status: 201, body: { attachment: restored } };
      },
    });
    const user = userEvent.setup();
    shown(true);
    const list = await screen.findByRole("region", { name: "Files on this page" });
    await user.click(await within(list).findByText("1 earlier version"));
    await user.click(within(list).getByRole("button", { name: "Restore Budget.XLSX, version 1" }));
    await waitFor(() => expect(within(list).getByRole("status")).toHaveTextContent("Restored version 1 of Budget.XLSX as version 4."));
    expect(sent.some((r) => r.method === "POST" && r.path === `/attachments/${first.id}/restore`)).toBe(true);
    await waitFor(() => expect(within(list).getByText(/Version 4, restored from version 1/)).toBeInTheDocument());

    cleanup();
    stubApi({
      [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [third, first] } },
      [`POST /attachments/${first.id}/restore`]: {
        status: 409,
        body: { error: { code: "already_latest", message: "Version 1 of Budget.XLSX is already the latest; restore an earlier version instead." } },
      },
    });
    shown(true);
    const again = await screen.findByRole("region", { name: "Files on this page" });
    await user.click(await within(again).findByText("1 earlier version"));
    await user.click(within(again).getByRole("button", { name: "Restore Budget.XLSX, version 1" }));
    expect(await within(again).findByRole("alert")).toHaveTextContent("restore an earlier version instead.");
  });

  it("offers a reader no restore", async () => {
    stubApi({ [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [third, first] } } });
    const user = userEvent.setup();
    shown(false);
    const list = await screen.findByRole("region", { name: "Files on this page" });
    await user.click(await within(list).findByText("1 earlier version"));
    expect(within(list).queryByRole("button", { name: /^Restore/ })).toBeNull();
  });

  it("says which version a file is and where a restored one came from", () => {
    expect(versionNote({ version: 1, restoredFrom: null, editedFrom: null })).toBe("");
    expect(versionNote({ version: 2, restoredFrom: null, editedFrom: null })).toBe("Version 2");
    expect(versionNote({ version: 5, restoredFrom: 2, editedFrom: null })).toBe("Version 5, restored from version 2");
    expect(versionNote({ version: 3, restoredFrom: null, editedFrom: 1 })).toBe("Version 3, edited from version 1");
  });

  it("says when there are no files, and asks an unsaved page to be saved first", async () => {
    stubApi({ [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } } });
    shown(true);
    expect(await screen.findByText("No files on this page yet. Upload one with Upload files.")).toBeInTheDocument();
    cleanup();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <AttachmentList pageId={undefined} editable />
      </QueryClientProvider>,
    );
    expect(screen.getByText("Save the page once to list its files here.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Upload files" })).toBeNull();
  });
});
