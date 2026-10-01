import { afterEach, describe, expect, it, vi } from "vitest";
import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { stubUploads, upload } from "@/test/xhr";
import { importProblem } from "./MarkdownDialogs";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const pageId = "0195f000-0000-7000-8000-0000000000a1";
const madeId = "0195f000-0000-7000-8000-0000000000b1";
const childId = "0195f000-0000-7000-8000-0000000000b2";

function stubPage({ edit = true }: { edit?: boolean } = {}) {
  const home = aPage();
  const page = aPage({
    id: pageId,
    title: "Guide",
    home: false,
    parentId: home.id,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    can: { edit, delete: edit, restrict: edit, comment: true },
  });
  const space = aSpace({ can: { ...aSpace().can, editPages: edit } });
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page, space } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
  });
}

async function openMenuItem(name: string) {
  await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
  await userEvent.click(await screen.findByRole("menuitem", { name }));
  return within(await screen.findByRole("dialog"));
}

/** A file as a folder picker hands it over, with the path it had in the folder. */
function inFolder(path: string, content: string, type = "text/markdown"): File {
  const file = new File([content], path.split("/").pop()!, { type });
  Object.defineProperty(file, "webkitRelativePath", { value: path });
  return file;
}

describe("exporting a page as Markdown", () => {
  it("offers the page alone, with its files, or with the pages below it, as downloads", async () => {
    stubPage({ edit: false });
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    const dialog = await openMenuItem("Export as Markdown");
    const download = dialog.getByRole("link", { name: "Download" });
    expect(download).toHaveAttribute("href", `/api/v1/pages/${pageId}/export`);
    expect(download).toHaveAttribute("download");
    await userEvent.click(dialog.getByRole("radio", { name: /the pages below it/ }));
    expect(download).toHaveAttribute("href", `/api/v1/pages/${pageId}/export?subtree=true`);
    await userEvent.click(dialog.getByRole("radio", { name: /one Markdown file/ }));
    expect(download).toHaveAttribute("href", `/api/v1/pages/${pageId}/markdown`);
    expect(await axeViolations()).toEqual([]);
    download.addEventListener("click", (event) => event.preventDefault());
    await userEvent.click(download);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("is open to readers, and importing is not", async () => {
    stubPage({ edit: false });
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    expect(await screen.findByRole("menuitem", { name: "Export as Markdown" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Import Markdown" })).toBeNull();
  });
});

describe("importing Markdown under a page", () => {
  it("sends a folder's files with their paths and lists the pages it made", async () => {
    stubPage();
    const uploads = stubUploads();
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    const dialog = await openMenuItem("Import Markdown");
    expect(dialog.getByRole("button", { name: "Import" })).toBeDisabled();
    const folder = document.querySelector<HTMLInputElement>("[data-markdown-folder]")!;
    expect(folder).toHaveAttribute("webkitdirectory");
    await userEvent.upload(folder, [
      inFolder("handbook/README.md", "# Handbook\n"),
      inFolder("handbook/setup.md", "# Setup\n"),
      inFolder("handbook/logo.png", "png", "image/png"),
    ]);
    expect(dialog.getByText("3 files chosen")).toBeInTheDocument();
    expect(dialog.getByText("handbook/setup.md")).toBeInTheDocument();
    await userEvent.click(dialog.getByRole("button", { name: "Import" }));

    const sent = upload(uploads, 0);
    expect(sent.url).toBe(`/api/v1/pages/${pageId}/import`);
    expect(sent.form.getAll("file").map((f) => (f as File).name)).toEqual(["handbook/README.md", "handbook/setup.md", "handbook/logo.png"]);
    act(() => sent.progress(30, 100));
    expect(dialog.getByRole("progressbar", { name: "Sending the files, 30%" })).toBeInTheDocument();
    act(() =>
      sent.respond(201, {
        pages: [
          { id: madeId, parentId: pageId, title: "Handbook", depth: 1 },
          { id: childId, parentId: madeId, title: "Setup", depth: 2 },
        ],
        warnings: ["handbook/setup.md: The link to ../x.md points outside the import, so only its words were kept."],
      }),
    );
    expect(await dialog.findByText("Imported 2 pages.")).toBeInTheDocument();
    expect(dialog.getByRole("link", { name: "Setup" })).toHaveAttribute("href", `/s/DOCS/p/${childId}/setup`);
    expect(dialog.getByText(/points outside the import/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says what is wrong before sending, and shows the server's refusal", async () => {
    stubPage();
    const uploads = stubUploads();
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    const dialog = await openMenuItem("Import Markdown");
    const input = document.querySelector<HTMLInputElement>("[data-markdown-files]")!;
    await userEvent.upload(input, [new File(["png"], "logo.png", { type: "image/png" })]);
    await userEvent.click(dialog.getByRole("button", { name: "Import" }));
    expect(await dialog.findByRole("alert")).toHaveTextContent("None of these is a Markdown file.");
    expect(uploads).toHaveLength(0);

    await userEvent.upload(input, [new File(["# A"], "a.md", { type: "text/markdown" })]);
    expect(dialog.queryByRole("alert")).toBeNull();
    await userEvent.click(dialog.getByRole("button", { name: "Import" }));
    act(() =>
      upload(uploads, 0).respond(422, {
        error: { code: "validation_failed", message: 'The upload names the path "../a.md", which leaves its folder; upload the folder itself.' },
      }),
    );
    expect(await dialog.findByRole("alert")).toHaveTextContent("which leaves its folder");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(dialog.getByRole("button", { name: "Import" }));
    act(() => upload(uploads, 1).fail());
    expect(await dialog.findByRole("alert")).toHaveTextContent("The upload broke off.");
  });

  it("works from the keyboard and gives focus back when it closes", async () => {
    stubPage();
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    const trigger = await screen.findByRole("button", { name: "Page actions" });
    trigger.focus();
    await userEvent.keyboard("{Enter}");
    const item = await screen.findByRole("menuitem", { name: "Import Markdown" });
    while (document.activeElement !== item) await userEvent.keyboard("{ArrowDown}");
    await userEvent.keyboard("{Enter}");
    const dialog = await screen.findByRole("dialog", { name: "Import Markdown under Guide" });
    expect(dialog).toContainElement(document.activeElement as HTMLElement);
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });
});

describe("what an import is checked for before it is sent", () => {
  it("needs Markdown or an archive, within the limit", () => {
    expect(importProblem([new File(["x"], "a.png")])).toMatch(/None of these/);
    expect(importProblem([new File(["x"], "a.MD")])).toBeUndefined();
    expect(importProblem([new File(["x"], "docs.zip")])).toBeUndefined();
    const huge = new File(["x"], "a.md");
    Object.defineProperty(huge, "size", { value: 101 * 1024 * 1024 });
    expect(importProblem([huge])).toMatch(/100 MB/);
  });
});
