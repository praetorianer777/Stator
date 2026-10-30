import { afterEach, describe, expect, it, onTestFinished, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { answerConfirm, arrival, confirmDialog, renderAt, signedIn, stubApi } from "@/test/app";
import { aSpace } from "@/test/spaces";
import { createMemoryHistory } from "@tanstack/react-router";
import { createQueryClient } from "@/lib/session";
import { buildRouter } from "@/routes";
import { movedSpaceAddress, sectionsFor, settingsAddress } from "./settingsSections";

afterEach(() => vi.unstubAllGlobals());

const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const, administers: false } };
const space = aSpace();
const reader = aSpace({ can: { editPages: true, administer: false, delete: false, purgeTrash: false, addComments: true, deletePages: false } });

function tabs() {
  return within(screen.getByRole("tablist", { name: "Space settings" }))
    .getAllByRole("tab")
    .map((tab) => tab.textContent);
}

describe("a space's settings", () => {
  it("lists every section to an administrator, in the suite's order, as tabs in the header", async () => {
    stubApi({ "GET /spaces/DOCS": { status: 200, body: { space } }, "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } } });
    await renderAt("/spaces/DOCS/settings/general");
    await screen.findByLabelText("Name");
    expect(tabs()).toEqual(["General", "Workflow", "Templates", "Members and permissions", "Integrations", "Import and export", "Trash and archive"]);
    expect(document.querySelector("[data-space-settings]")).toHaveAttribute("data-page-width", "content");
    expect(document.querySelector("[data-settings-read-only]")).toBeNull();
    expect(screen.getByRole("link", { name: "Open the stale pages" })).toHaveAttribute("href", "/spaces/DOCS/settings/general/stale");
    expect(screen.getByRole("button", { name: "Archive space" })).toBeInTheDocument();
  });

  it("goes to a section by its tab, at an address of its own", async () => {
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/trash": { status: 200, body: { items: [] } },
      "GET /spaces/DOCS/archived-pages": { status: 200, body: { items: [] } },
    });
    const router = await renderAt("/spaces/DOCS/settings/general");
    await userEvent.click(await screen.findByRole("tab", { name: "Trash and archive" }));
    await arrival(router, "/spaces/DOCS/settings/trash");
    expect(await screen.findByText("The trash is empty.")).toBeInTheDocument();
    expect(screen.getByText("Nothing is archived in this space.")).toBeInTheDocument();
  });

  it("shows a member what they may see, read-only, and says who to ask", async () => {
    stubApi({ "GET /spaces/DOCS": { status: 200, body: { space: reader } }, "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } } });
    await renderAt("/spaces/DOCS/settings/general", { me: member });
    expect(await screen.findByText("Only the space's administrators change its settings. Ask one of them, or ask for the role.")).toHaveAttribute(
      "data-settings-read-only",
    );
    expect(tabs()).toEqual(["General", "Templates", "Integrations", "Trash and archive"]);
    expect(screen.getByLabelText("Name")).toHaveAttribute("readonly");
    for (const name of ["Save details", "Archive space", "Delete space"]) expect(screen.queryByRole("button", { name })).toBeNull();
    expect(screen.queryByRole("link", { name: "Open the stale pages" })).toBeNull();
  });

  it("asks in a dialog before deleting the space", async () => {
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "DELETE /spaces/DOCS": { status: 204 },
      "GET /spaces": { status: 200, body: { spaces: [] } },
    });
    const router = await renderAt("/spaces/DOCS/settings/general");
    await userEvent.click(await screen.findByRole("button", { name: "Delete space" }));
    expect(await confirmDialog()).toHaveAccessibleName("Delete space Handbook?");
    await answerConfirm(false);
    expect(sent.some((r) => r.method === "DELETE")).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "Delete space" }));
    await answerConfirm(true);
    await arrival(router, "/spaces");
    expect(sent.some((r) => r.method === "DELETE" && r.path === "/spaces/DOCS")).toBe(true);
  });

  it("opens the section an old tab named", async () => {
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/review-rules": { status: 200, body: { rules: [] } },
    });
    const router = await renderAt("/spaces/DOCS/settings?tab=reviews");
    await arrival(router, "/spaces/DOCS/settings/workflow");
    expect(await screen.findByRole("tab", { name: "Workflow" })).toHaveAttribute("aria-selected", "true");
  });
});

describe("the palette", () => {
  it("offers the settings of the space the reader is in", async () => {
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space: reader } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /recent-pages": { status: 200, body: { pages: [] } },
      "GET /search/quick": { status: 200, body: { pages: [] } },
    });
    const router = await renderAt("/spaces/DOCS/settings/templates", { me: member });
    await screen.findByRole("tab", { name: "Templates" });
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.type(screen.getByRole("combobox"), "sett");
    const option = await screen.findByRole("option", { name: /Handbook \/ Settings/ });
    expect(option).toHaveAttribute("data-quick-search-place", "space-settings");
    await userEvent.click(option);
    await arrival(router, "/spaces/DOCS/settings/general");
  });
});

describe("a space's old addresses", () => {
  it("send /s/<key> and its pages on to /spaces/<key>, search and hash kept", async () => {
    // jsdom draws nothing, so it cannot scroll; the router's going to the guests is what is seen.
    const scrolled = vi.fn();
    Element.prototype.scrollIntoView = scrolled;
    onTestFinished(() => void delete (Element.prototype as Partial<Element>).scrollIntoView);
    stubApi({ "GET /spaces/DOCS": { status: 200, body: { space } }, "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } } });
    const router = await renderAt("/s/docs/settings?tab=guests");
    await arrival(router, "/spaces/DOCS/settings/members");
    expect(router.state.location.hash).toBe("guests");
    await waitFor(() => expect(scrolled).toHaveBeenCalled());
    expect(scrolled.mock.contexts[0]).toHaveAttribute("id", "guests");
  });

  it("leave /spaces/new and /spaces/import to the app, and /spaces/NEW to the space of that key", () => {
    const router = buildRouter(
      createQueryClient(() => {}),
      createMemoryHistory({ initialEntries: ["/"] }),
    );
    const last = (path: string) => router.matchRoutes(path).at(-1)?.routeId;
    expect(last("/spaces/new")).toMatch(/\/spaces\/new$/);
    expect(last("/spaces/import")).toMatch(/\/spaces\/import$/);
    expect(last("/spaces/new/personal")).toMatch(/\/spaces\/new\/personal$/);
    expect(last("/spaces/NEW")).toMatch(/\/spaces\/\$spaceKey\/$/);
    expect(last("/spaces/IMPORT/settings/general")).toMatch(/\/settings\/general$/);
  });

  it("are worked out from what followed /s/", () => {
    expect(movedSpaceAddress("DOCS")).toBe("/spaces/DOCS");
    expect(movedSpaceAddress("DOCS/p/p-1/runbook", "?thread=t-1", "c-2")).toBe("/spaces/DOCS/p/p-1/runbook?thread=t-1#c-2");
    expect(movedSpaceAddress("DOCS/blog", "?year=2026")).toBe("/spaces/DOCS/blog?year=2026");
    expect(movedSpaceAddress("DOCS/settings", "?tab=archive")).toBe("/spaces/DOCS/settings?tab=archive");
    expect(movedSpaceAddress("DOCS/settings/stale")).toBe("/spaces/DOCS/settings/general/stale");
    expect(movedSpaceAddress("DOCS/settings/templates/new")).toBe("/spaces/DOCS/settings/templates/new");
  });

  it("lead every old tab to its section", () => {
    const cases: Array<[string | undefined, string]> = [
      [undefined, "/spaces/DOCS/settings/general"],
      ["details", "/spaces/DOCS/settings/general"],
      ["reviews", "/spaces/DOCS/settings/workflow"],
      ["templates", "/spaces/DOCS/settings/templates"],
      ["permissions", "/spaces/DOCS/settings/members"],
      ["guests", "/spaces/DOCS/settings/members#guests"],
      ["shortcuts", "/spaces/DOCS/settings/integrations"],
      ["export", "/spaces/DOCS/settings/export"],
      ["trash", "/spaces/DOCS/settings/trash"],
      ["archive", "/spaces/DOCS/settings/trash#archive"],
      ["nonsense", "/spaces/DOCS/settings/general"],
    ];
    for (const [tab, to] of cases) expect(settingsAddress("DOCS", tab)).toBe(to);
  });

  it("hide what only administrators use from everybody else", () => {
    expect(sectionsFor(reader)).toEqual(["general", "templates", "integrations", "trash"]);
    expect(sectionsFor(space)).toHaveLength(7);
  });
});
