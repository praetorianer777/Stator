import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SpaceExport, SpaceImport } from "@/api/spaceTransfers";
import type * as config from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aSpace } from "@/test/spaces";
import { stubUploads, upload } from "@/test/xhr";

// The pages follow the worker by the clock; these tests run its clock fast.
vi.mock("@/config", async (original) => ({ ...(await original<typeof config>()), SPACE_TRANSFER_POLL_MS: 20 }));

afterEach(() => vi.unstubAllGlobals());

const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const } };
const space = aSpace();

function anExport(over: Partial<SpaceExport> = {}): SpaceExport {
  return {
    id: "e-1",
    spaceKey: "DOCS",
    format: "archive",
    state: "queued",
    progress: { done: 0, total: 0 },
    requestedBy: "Ada Lovelace",
    mine: true,
    requestedAt: "2026-10-07T10:00:00Z",
    startedAt: null,
    finishedAt: null,
    fileName: null,
    size: null,
    expiresAt: null,
    failure: null,
    message: null,
    ...over,
  };
}

function anImport(over: Partial<SpaceImport> = {}): SpaceImport {
  return {
    id: "i-1",
    key: "LANDED",
    name: null,
    source: "archive",
    state: "queued",
    progress: { done: 0, total: 0 },
    spaceKey: null,
    report: null,
    failure: null,
    message: null,
    requestedAt: "2026-10-07T10:00:00Z",
    startedAt: null,
    finishedAt: null,
    ...over,
  };
}

describe("exporting a space", () => {
  it("asks the worker for the format chosen, follows it, and offers the file", async () => {
    let jobs: SpaceExport[] = [];
    let looks = 0;
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/exports": () => {
        if (jobs.length && ++looks > 2)
          jobs = [anExport({ format: "html", state: "done", fileName: "docs-html.zip", size: 2048, expiresAt: "2026-10-08T10:00:00Z" })];
        return { status: 200, body: { exports: jobs } };
      },
      "POST /spaces/DOCS/exports": () => {
        jobs = [anExport({ format: "html", state: "running", progress: { done: 1, total: 4 } })];
        return { status: 202, body: { export: jobs[0] } };
      },
    });
    await renderAt("/s/DOCS/settings?tab=export");
    expect(await screen.findByText("This space has not been exported yet.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("radio", { name: /HTML pages/ }));
    await userEvent.click(screen.getByRole("button", { name: "Export space" }));
    const link = await screen.findByRole("link", { name: "Download" });
    expect(link).toHaveAttribute("href", "/api/v1/space-exports/e-1/file");
    expect(sent.filter((r) => r.method === "POST").map((r) => r.body)).toEqual([{ format: "html" }]);
    expect(screen.getByRole("button", { name: "Export space" })).toBeEnabled();
    expect(await axeViolations()).toEqual([]);
  });

  it("says why an export failed, and that an old one is gone", async () => {
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/exports": {
        status: 200,
        body: {
          exports: [
            anExport({
              id: "e-2",
              state: "failed",
              failure: "forbidden",
              message: "Only administrators of the space export it, and you no longer administer it.",
            }),
            anExport({ id: "e-3", state: "expired" }),
          ],
        },
      },
    });
    await renderAt("/s/DOCS/settings?tab=export");
    expect(await screen.findByText(/you no longer administer it/)).toBeInTheDocument();
    expect(screen.getByText(/The file was deleted after its time/)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Download" })).toBeNull();
  });

  it("is not offered to somebody who does not administer the space", async () => {
    stubApi({ "GET /spaces/DOCS": { status: 200, body: { space: aSpace({ can: { ...space.can, administer: false } }) } } });
    await renderAt("/s/DOCS/settings?tab=export", { me: member });
    expect(await screen.findByRole("tab", { name: "Details" })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("tab", { name: "Export" })).toBeNull();
  });
});

describe("importing a space", () => {
  it("uploads the archive with the key asked for, follows the worker and shows what did not come across", async () => {
    const uploads = stubUploads();
    let looks = 0;
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [] } },
      "GET /space-imports/i-1": () =>
        ++looks < 3
          ? { status: 200, body: { import: anImport({ state: "running", progress: { done: 2, total: 5 } }) } }
          : {
              status: 200,
              body: {
                import: anImport({
                  state: "done",
                  spaceKey: "LANDED",
                  report: {
                    pages: 4,
                    versions: 9,
                    files: 2,
                    comments: 1,
                    templates: 0,
                    calendars: 0,
                    people: [{ name: "Grace Hopper", email: "grace@elsewhere.test" }],
                    groups: ["Auditors"],
                    dropped: [{ page: "", permission: "administer", subject: "Grace Hopper <grace@elsewhere.test>" }],
                    reattributed: 3,
                    droppedReactions: 1,
                    mentionsAsText: 2,
                    lost: [],
                    lostCount: 0,
                  },
                }),
              },
            },
    });
    await renderAt("/spaces");
    await userEvent.click(await screen.findByRole("button", { name: "Import space" }));
    const file = new File(["zip"], "docs-space.zip", { type: "application/zip" });
    await userEvent.upload(document.querySelector<HTMLInputElement>("[data-import-file]")!, file);
    await userEvent.type(screen.getByLabelText("Key"), "landed");
    await userEvent.click(screen.getByRole("button", { name: "Import space" }));
    const sent = upload(uploads, 0);
    expect(sent.url).toBe("/api/v1/space-imports?key=LANDED");
    expect(sent.file.name).toBe("docs-space.zip");
    sent.respond(202, { import: anImport() });
    const report = await screen.findByText("Grace Hopper <grace@elsewhere.test>");
    const card = report.closest("[data-import-report]") as HTMLElement;
    expect(within(card).getByText("Auditors")).toBeInTheDocument();
    expect(within(card).getByText(/Administer for Grace Hopper/)).toBeInTheDocument();
    expect(within(card).getByText("3 versions, comments and files are now in your name.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open LANDED" })).toHaveAttribute("href", "/s/LANDED");
    expect(await axeViolations()).toEqual([]);
  });

  it("says in a sentence why an archive is refused, by the field it is about", async () => {
    const uploads = stubUploads();
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [] } } });
    await renderAt("/spaces/import");
    await userEvent.upload(document.querySelector<HTMLInputElement>("[data-import-file]")!, new File(["x"], "notes.zip"));
    await userEvent.click(screen.getByRole("button", { name: "Import space" }));
    upload(uploads, 0).respond(422, {
      error: { code: "validation_failed", message: "This file is no zip archive.", fields: { file: "This file is no zip archive. Export the space again." } },
    });
    expect(await screen.findByText("This file is no zip archive. Export the space again.")).toBeInTheDocument();
  });

  it("is not offered to somebody who may not create spaces", async () => {
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [] } } });
    await renderAt("/spaces", { me: member });
    await waitFor(() => expect(screen.getByRole("heading", { name: "Spaces" })).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Import space" })).toBeNull();
  });
});
