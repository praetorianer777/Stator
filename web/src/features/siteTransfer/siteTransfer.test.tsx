import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SiteExport, SiteImport, SiteImportReport } from "@/api/siteTransfers";
import type * as config from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { stubUploads, upload } from "@/test/xhr";

// The page follows the worker by the clock; these tests run its clock fast.
vi.mock("@/config", async (original) => ({ ...(await original<typeof config>()), SPACE_TRANSFER_POLL_MS: 20 }));

afterEach(() => vi.unstubAllGlobals());

const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const } };

function anExport(over: Partial<SiteExport> = {}): SiteExport {
  return {
    id: "e-1",
    state: "queued",
    progress: { done: 0, total: 0 },
    requestedBy: "Ada Lovelace",
    mine: true,
    requestedAt: "2026-10-12T10:00:00Z",
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

function anImport(over: Partial<SiteImport> = {}): SiteImport {
  return {
    id: "i-1",
    state: "queued",
    progress: { done: 0, total: 0 },
    requestedBy: "Ada Lovelace",
    report: null,
    failure: null,
    message: null,
    requestedAt: "2026-10-12T10:00:00Z",
    startedAt: null,
    finishedAt: null,
    ...over,
  };
}

const report: SiteImportReport = {
  spaces: 2,
  pages: 14,
  versions: 30,
  files: 3,
  comments: 5,
  drafts: 2,
  templates: 1,
  themes: 1,
  webhooks: 1,
  people: 6,
  matched: 1,
  groups: 2,
  auditEntries: 120,
  kept: ["signIn"],
  dropped: [{ page: "Plans", permission: "view", subject: "Writers" }],
  reattributed: 0,
  droppedReactions: 0,
  notCarried: [],
};

describe("exporting the organization", () => {
  it("asks the worker for the archive, follows it, offers the file and says what stays behind", async () => {
    let jobs: SiteExport[] = [];
    let looks = 0;
    const sent = stubApi({
      "GET /site/imports": { status: 200, body: { imports: [] } },
      "GET /site/exports": () => {
        if (jobs.length && ++looks > 2)
          jobs = [anExport({ state: "done", fileName: "acme-organization-2026-10-12.zip", size: 4096, expiresAt: "2026-10-13T10:00:00Z" })];
        return { status: 200, body: { exports: jobs } };
      },
      "POST /site/exports": () => {
        jobs = [anExport({ state: "running", progress: { done: 1, total: 4 } })];
        return { status: 202, body: { export: jobs[0] } };
      },
    });
    await renderAt("/settings/transfer");
    expect(await screen.findByText("The organization has not been exported yet.")).toBeInTheDocument();
    expect(screen.getByText("Passwords.")).toBeInTheDocument();
    expect(screen.getByText("Webhook signing secrets.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Export organization" }));
    const link = await screen.findByRole("link", { name: "Download" });
    expect(link).toHaveAttribute("href", "/api/v1/site/exports/e-1/file");
    expect(sent.filter((r) => r.method === "POST").map((r) => r.path)).toEqual(["/site/exports"]);
    expect(screen.getByRole("button", { name: "Export organization" })).toBeEnabled();
    expect(await axeViolations()).toEqual([]);
  });

  it("tells a member who exports and imports the organization", async () => {
    stubApi({});
    await renderAt("/settings/transfer", { me: member });
    expect(await screen.findByText(/Only an administrator of the organization exports or imports all of it/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Export organization" })).toBeNull();
  });
});

describe("importing an organization", () => {
  it("uploads the archive, follows the worker and shows what came across", async () => {
    const uploads = stubUploads();
    let jobs: SiteImport[] = [];
    let looks = 0;
    stubApi({
      "GET /site/exports": { status: 200, body: { exports: [] } },
      "GET /site/imports": () => {
        if (jobs.length && ++looks > 2) jobs = [anImport({ state: "done", report })];
        return { status: 200, body: { imports: jobs } };
      },
      "GET /spaces": { status: 200, body: { spaces: [] } },
    });
    await renderAt("/settings/transfer");
    await screen.findByText("The organization has not been exported yet.");
    const file = new File(["zip"], "acme-organization.zip", { type: "application/zip" });
    await userEvent.upload(document.querySelector<HTMLInputElement>("[data-site-import-file]")!, file);
    await userEvent.click(screen.getByRole("button", { name: "Import organization" }));
    const sent = upload(uploads, 0);
    expect(sent.url).toBe("/api/v1/site/imports");
    expect(sent.file.name).toBe("acme-organization.zip");
    jobs = [anImport({ state: "running", progress: { done: 2, total: 9 } })];
    sent.respond(202, { import: jobs[0] });
    const card = (await screen.findByText("What came across")).closest("[data-site-import-report]") as HTMLElement;
    expect(within(card).getByText("Spaces").nextElementSibling).toHaveTextContent("2");
    expect(within(card).getByText("This organization kept its own sign-in provider.")).toBeInTheDocument();
    expect(within(card).getByText(/Webhooks came across switched off/)).toBeInTheDocument();
    expect(within(card).getByText(/View list for Writers on Plans/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says in a sentence why an organization with spaces takes no archive", async () => {
    const uploads = stubUploads();
    stubApi({
      "GET /site/exports": { status: 200, body: { exports: [] } },
      "GET /site/imports": { status: 200, body: { imports: [] } },
    });
    await renderAt("/settings/transfer");
    await screen.findByText("The organization has not been exported yet.");
    await userEvent.upload(document.querySelector<HTMLInputElement>("[data-site-import-file]")!, new File(["zip"], "a.zip", { type: "application/zip" }));
    await userEvent.click(screen.getByRole("button", { name: "Import organization" }));
    upload(uploads, 0).respond(409, {
      error: { code: "org_not_empty", message: "This organization has spaces already, and an organization's archive is imported only into one without any." },
    });
    expect(await screen.findByText(/has spaces already/)).toBeInTheDocument();
  });
});
