import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Archive, ArchiveItem } from "@/api/archive";
import type { Page } from "@/api/pages";
import type { Hit } from "@/api/search";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const home = aPage();
const space = aSpace();
const plansId = "0195f000-0000-7000-8000-0000000000c1";
const oldId = "0195f000-0000-7000-8000-0000000000c2";
const PLANS = `/s/DOCS/p/${plansId}/plans`;
const OLD = `/s/DOCS/p/${oldId}/old-roadmap`;
const reader = { edit: false, delete: false, restrict: false, comment: false, archive: false };
const ofPlans: Archive = {
  page: { id: plansId, title: "Plans", home: false },
  space: false,
  archivedAt: "2026-09-30T09:00:00Z",
  archivedByName: "Ada Lovelace",
};
const item: ArchiveItem = {
  id: plansId,
  title: "Plans",
  archivedAt: "2026-09-30T09:00:00Z",
  archivedByName: "Ada Lovelace",
  pages: 3,
  parentTitle: "Handbook",
};

function stubPage(id: string, title: string, page: Partial<Page>, more: Record<string, Answer | ((request: Request) => Answer)> = {}) {
  const shown = aPage({ id, title, home: false, parentId: home.id, ancestors: [{ id: home.id, title: "Handbook", home: true }], ...page });
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${id}`]: { status: 200, body: { page: shown, space } },
    [`GET /pages/${id}/attachments`]: { status: 200, body: { attachments: [] } },
    [`POST /pages/${id}/visit`]: { status: 204 },
    ...more,
  });
}

describe("an archived page", () => {
  it("says so in a banner, offers no edit, and lets an administrator unarchive it", async () => {
    let archived: Archive | null = ofPlans;
    stubPage(
      plansId,
      "Plans",
      {},
      {
        [`GET /pages/${plansId}`]: () => ({
          status: 200,
          body: {
            page: aPage({
              id: plansId,
              title: "Plans",
              home: false,
              parentId: home.id,
              ancestors: [{ id: home.id, title: "Handbook", home: true }],
              archived,
              can: archived ? { ...reader, archive: true } : { edit: true, delete: true, restrict: true, comment: true, archive: true },
            }),
            space,
          },
        }),
        [`DELETE /pages/${plansId}/archive`]: () => {
          archived = null;
          return { status: 200, body: { page: aPage({ id: plansId, title: "Plans", home: false }) } };
        },
      },
    );
    await renderAt(PLANS);
    const banner = (await screen.findByText(/This page is archived/)).closest("[data-archived-banner]")!;
    expect(banner).toHaveAttribute("data-archived-banner", "page");
    expect(banner).toHaveTextContent("Archived by Ada Lovelace on");
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.getByText("Archived", { selector: "[data-archived-mark]" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(banner as HTMLElement).getByRole("button", { name: "Unarchive" }));
    await waitFor(() => expect(screen.queryByText(/This page is archived/)).toBeNull());
    expect(await screen.findByRole("button", { name: "Edit" })).toBeInTheDocument();
  });

  it("below an archived page points to the page that was archived", async () => {
    stubPage(oldId, "Old roadmap", { archived: ofPlans, can: { ...reader, archive: true } });
    await renderAt(OLD);
    const banner = (await screen.findByText(/It was archived together with Plans above it/)).closest("[data-archived-banner]")!;
    expect(banner).toHaveAttribute("data-archived-banner", "with");
    expect(within(banner as HTMLElement).queryByRole("button", { name: "Unarchive" })).toBeNull();
    expect(within(banner as HTMLElement).getByRole("link", { name: "Go to Plans" })).toHaveAttribute("href", PLANS);
  });

  it("in an archived space sends its administrators to the space's settings", async () => {
    stubPage(oldId, "Old roadmap", {
      archived: { page: null, space: true, archivedAt: "2026-09-30T09:00:00Z", archivedByName: "" },
      can: reader,
    });
    await renderAt(OLD);
    const banner = (await screen.findByText(/This whole space is archived/)).closest("[data-archived-banner]")!;
    expect(banner).toHaveTextContent("Archived on");
    expect(within(banner as HTMLElement).getByRole("link", { name: "Open the space settings" })).toHaveAttribute("href", "/s/DOCS/settings");
  });

  it("is archived from the page menu after asking", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValue(true);
    const sent = stubPage(
      plansId,
      "Plans",
      {},
      { [`PUT /pages/${plansId}/archive`]: { status: 200, body: { page: aPage({ id: plansId, archived: ofPlans }) } } },
    );
    await renderAt(PLANS);
    const pick = async () => {
      await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
      await userEvent.click(screen.getByRole("menuitem", { name: "Archive" }));
    };
    await pick();
    expect(sent.some((r) => r.method === "PUT")).toBe(false);
    await pick();
    await waitFor(() => expect(sent.some((r) => r.method === "PUT" && r.path === `/pages/${plansId}/archive`)).toBe(true));
    expect(confirm).toHaveBeenLastCalledWith(expect.stringContaining("Archive Plans and every page below it?"));
  });

  it("is not offered to somebody who may not archive", async () => {
    stubPage(plansId, "Plans", { can: { edit: true, delete: true, restrict: true, comment: true, archive: false } });
    await renderAt(PLANS);
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    expect(screen.queryByRole("menuitem", { name: "Archive" })).toBeNull();
  });

  it("refuses an edit in a sentence that says what to do", async () => {
    stubPage(plansId, "Plans", { archived: ofPlans, can: { ...reader, archive: true } });
    await renderAt(`${PLANS}/edit`);
    expect(
      await screen.findByText("This page is archived, so nobody can edit it. Ask an administrator of the space to unarchive it first."),
    ).toBeInTheDocument();
  });
});

describe("the space's archive", () => {
  it("lists what is archived to every reader, and unarchives for administrators", async () => {
    let items = [item];
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/archived-pages": () => ({ status: 200, body: { items } }),
      [`DELETE /pages/${plansId}/archive`]: () => {
        items = [];
        return { status: 200, body: { page: aPage({ id: plansId, title: "Plans", home: false }) } };
      },
    });
    await renderAt("/s/DOCS/settings?tab=archive");
    const row = (await screen.findByRole("link", { name: "Plans" })).closest("tr")!;
    expect(row).toHaveTextContent("3 pages");
    expect(row).toHaveTextContent("by Ada Lovelace");
    expect(row).toHaveTextContent("Handbook");
    expect(screen.getByRole("tab", { name: "Archive" })).toHaveAttribute("aria-selected", "true");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(row).getByRole("button", { name: "Unarchive Plans" }));
    expect(await screen.findByText("Unarchived Plans.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE" && r.path === `/pages/${plansId}/archive`)).toBe(true);
    expect(await screen.findByText("Nothing is archived in this space.")).toBeInTheDocument();
  });

  it("offers a reader no way to unarchive", async () => {
    stubApi({
      "GET /spaces/DOCS": {
        status: 200,
        body: { space: aSpace({ can: { editPages: true, administer: false, delete: false, purgeTrash: false, addComments: true, deletePages: true } }) },
      },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/archived-pages": { status: 200, body: { items: [item] } },
    });
    await renderAt("/s/DOCS/settings?tab=archive");
    expect(await screen.findByRole("link", { name: "Plans" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Unarchive Plans" })).toBeNull();
  });

  it("archives the whole space from its details, after asking, and brings it back", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    let current = space;
    const sent = stubApi({
      "GET /spaces/DOCS": () => ({ status: 200, body: { space: current } }),
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "PUT /spaces/DOCS/archive": () => {
        current = aSpace({ archivedAt: "2026-10-01T09:00:00Z", archivedByName: "Ada Lovelace" });
        return { status: 200, body: { space: current } };
      },
      "DELETE /spaces/DOCS/archive": () => {
        current = space;
        return { status: 200, body: { space: current } };
      },
    });
    await renderAt("/s/DOCS/settings");
    await userEvent.click(await screen.findByRole("button", { name: "Archive space" }));
    expect(await screen.findByText(/Ada Lovelace archived this space on/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Unarchive space" }));
    expect(await screen.findByText("The space is unarchived.")).toBeInTheDocument();
    expect(sent.filter((r) => r.path === "/spaces/DOCS/archive").map((r) => r.method)).toEqual(["PUT", "DELETE"]);
  });
});

describe("the lists that leave archived things out", () => {
  it("shows archived spaces only when asked, marked", async () => {
    const archivedSpace = aSpace({ id: "0195f000-0000-7000-8000-00000000d0c6", key: "OLD", name: "Old days", archivedAt: "2026-10-01T09:00:00Z" });
    const sent = stubApi({
      "GET /spaces": (request) => ({
        status: 200,
        body: { spaces: new URL(request.url).searchParams.get("archived") === "true" ? [space, archivedSpace] : [space] },
      }),
    });
    await renderAt("/spaces");
    expect(await screen.findByRole("link", { name: "Handbook" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Old days" })).toBeNull();
    await userEvent.click(screen.getByRole("switch", { name: "Show archived spaces" }));
    const row = (await screen.findByRole("link", { name: "Old days" })).closest("tr")!;
    expect(within(row).getByText("Archived")).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/spaces" && r.method === "GET")).toBe(true);
  });

  it("asks search for archived pages only when the filter is on, and marks them", async () => {
    const hit: Hit = {
      type: "page",
      page: { id: plansId, title: "Plans", spaceKey: "DOCS", spaceName: "Handbook" },
      attachmentId: null,
      commentId: null,
      title: [{ text: "Plans", match: true }],
      snippet: [],
      labels: [],
      verified: false,
      archived: true,
      updatedAt: "2026-09-29T08:00:00Z",
      updatedByName: "Ada Lovelace",
    };
    const asked: string[] = [];
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /people": { status: 200, body: { people: [] } },
      "GET /search": (request) => {
        const archived = new URL(request.url).searchParams.get("archived");
        asked.push(archived ?? "");
        return { status: 200, body: { hits: archived === "true" ? [hit] : [], total: archived === "true" ? 1 : 0, limit: 20, offset: 0 } };
      },
    });
    await renderAt("/search?q=plans");
    expect(await screen.findByText("Nothing matches")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("checkbox", { name: "Include archived pages" }));
    const row = (await screen.findByText("Plans", { selector: "mark, span" })).closest("li")!;
    expect(within(row).getByText("Archived")).toBeInTheDocument();
    expect(asked).toEqual(["", "true"]);
  });
});
