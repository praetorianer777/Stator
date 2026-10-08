import { afterEach, describe, expect, it, vi } from "vitest";
import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WordImport } from "@/api/word";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { stubUploads, upload } from "@/test/xhr";
import { wordImportProblem } from "./WordImportDialog";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const pageId = "0195f000-0000-7000-8000-0000000000a1";
const madeId = "0195f000-0000-7000-8000-0000000000b1";
const folderId = "0195f000-0000-7000-8000-0000000000b2";
const importId = "0195f000-0000-7000-8000-0000000000c1";

const routes = (edit: boolean) => {
  const home = aPage();
  const page = aPage({
    id: pageId,
    title: "Guide",
    home: false,
    parentId: home.id,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    can: { edit, delete: edit, restrict: edit, comment: true, archive: false, add: edit, grantEdit: false },
  });
  const space = aSpace({ can: { ...aSpace().can, editPages: edit } });
  return {
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page, space } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
  };
};

function job(over: Partial<WordImport> = {}): WordImport {
  return {
    id: importId,
    state: "queued",
    parentId: pageId,
    done: 0,
    total: 3,
    files: [],
    skipped: [],
    failure: null,
    message: null,
    requestedAt: "2026-10-08T09:00:00Z",
    startedAt: null,
    finishedAt: null,
    ...over,
  };
}

const docx = (name: string, size = 10) => {
  const file = new File(["x".repeat(size)], name, { type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document" });
  return file;
};

async function openDialog() {
  await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
  await userEvent.click(await screen.findByRole("menuitem", { name: "Import Word documents" }));
  return within(await screen.findByRole("dialog", { name: "Import Word documents under Guide" }));
}

describe("importing Word documents under a page", () => {
  it("imports one document at once and links the page it made, with its warnings", async () => {
    stubApi(routes(true));
    const uploads = stubUploads();
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    const dialog = await openDialog();
    expect(dialog.getByRole("button", { name: "Import" })).toBeDisabled();
    await userEvent.upload(document.querySelector<HTMLInputElement>("[data-word-files]")!, [docx("Plan.docx")]);
    expect(dialog.getByText("1 file chosen")).toBeInTheDocument();
    await userEvent.click(dialog.getByRole("button", { name: "Import" }));

    const sent = upload(uploads, 0);
    expect(sent.url).toBe(`/api/v1/pages/${pageId}/import/docx`);
    act(() => sent.progress(40, 100));
    expect(dialog.getByRole("progressbar", { name: "Sending the documents, 40%" })).toBeInTheDocument();
    act(() =>
      sent.respond(201, {
        pages: [{ id: madeId, parentId: pageId, title: "Release plan", depth: 1 }],
        warnings: ["Plan.docx: The document's comments (2) were left out; they stay in the Word document."],
      }),
    );
    expect(await dialog.findByRole("link", { name: "Release plan" })).toHaveAttribute("href", `/s/DOCS/p/${madeId}/release-plan`);
    expect(dialog.getByText(/comments \(2\) were left out/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("queues several documents and follows the worker to its report", async () => {
    const answers = [
      job({ state: "running", done: 1 }),
      job({
        state: "done",
        done: 3,
        files: [
          { path: "a.docx", page: { id: madeId, parentId: pageId, title: "A", depth: 1 }, warnings: ["Charts were left out."], error: null },
          { path: "team/", page: { id: folderId, parentId: pageId, title: "team", depth: 1 }, warnings: [], error: null },
          { path: "broken.docx", page: null, warnings: [], error: "The file broken.docx cannot be a page: it is not a Word document." },
        ],
        skipped: ["team/logo.png"],
      }),
    ];
    let asked = 0;
    stubApi({
      ...routes(true),
      [`GET /word-imports/${importId}`]: () => ({ status: 200, body: { job: answers[Math.min(asked++, answers.length - 1)] } }),
    });
    const uploads = stubUploads();
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    const dialog = await openDialog();
    await userEvent.upload(document.querySelector<HTMLInputElement>("[data-word-files]")!, [docx("a.docx"), docx("broken.docx"), new File(["z"], "team.zip")]);
    await userEvent.click(dialog.getByRole("button", { name: "Import" }));
    const sent = upload(uploads, 0);
    expect(sent.url).toBe(`/api/v1/pages/${pageId}/word-imports`);
    expect(sent.form.getAll("file").map((f) => (f as File).name)).toEqual(["a.docx", "broken.docx", "team.zip"]);
    act(() => sent.respond(202, { job: job() }));
    expect(await dialog.findByText("Waiting for the import to begin.")).toBeInTheDocument();
    expect(await dialog.findByText("Imported 2 pages.", undefined, { timeout: 5000 })).toBeInTheDocument();
    expect(dialog.getByRole("link", { name: "A" })).toHaveAttribute("href", `/s/DOCS/p/${madeId}/a`);
    expect(dialog.getByText(/broken.docx cannot be a page/)).toBeInTheDocument();
    expect(dialog.getByText("Charts were left out.")).toBeInTheDocument();
    expect(dialog.getByText("team/logo.png")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says why an import stopped", async () => {
    stubApi({
      ...routes(true),
      [`GET /word-imports/${importId}`]: { status: 200, body: { job: job({ state: "failed", failure: "forbidden", message: "x" }) } },
    });
    const uploads = stubUploads();
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    const dialog = await openDialog();
    await userEvent.upload(document.querySelector<HTMLInputElement>("[data-word-files]")!, [docx("a.docx"), docx("b.docx")]);
    await userEvent.click(dialog.getByRole("button", { name: "Import" }));
    act(() => upload(uploads, 0).respond(202, { job: job() }));
    expect(await dialog.findByRole("alert", undefined, { timeout: 5000 })).toHaveTextContent("You may no longer add pages under this page");
  });

  it("shows the server's refusal, and is not offered to a reader", async () => {
    stubApi(routes(true));
    const uploads = stubUploads();
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    const dialog = await openDialog();
    await userEvent.upload(document.querySelector<HTMLInputElement>("[data-word-files]")!, [docx("old.docx")]);
    await userEvent.click(dialog.getByRole("button", { name: "Import" }));
    act(() =>
      upload(uploads, 0).respond(422, {
        error: {
          code: "validation_failed",
          message: "The file old.docx cannot be a page: it is not a Word document; save it from Word as a .docx and import it again.",
        },
      }),
    );
    expect(await dialog.findByRole("alert")).toHaveTextContent("old.docx cannot be a page");
  });

  it("is not offered to a reader", async () => {
    stubApi(routes(false));
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    expect(await screen.findByRole("menuitem", { name: "Export as Markdown" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Import Word documents" })).toBeNull();
  });
});

describe("what a Word import is checked for before it is sent", () => {
  it("needs documents or an archive, within the limits", () => {
    expect(wordImportProblem([new File(["x"], "a.doc")])).toMatch(/None of these is a Word document/);
    expect(wordImportProblem([new File(["x"], "a.DOCX")])).toBeUndefined();
    expect(wordImportProblem([new File(["x"], "docs.zip")])).toBeUndefined();
    const huge = new File(["x"], "a.docx");
    Object.defineProperty(huge, "size", { value: 51 * 1024 * 1024 });
    expect(wordImportProblem([huge])).toMatch(/50 MB/);
    expect(wordImportProblem(Array.from({ length: 51 }, (_, i) => new File(["x"], `${i}.docx`)))).toMatch(/at most 50/);
  });
});
