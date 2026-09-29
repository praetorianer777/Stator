import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TrashItem } from "@/api/trash";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const space = aSpace();
const home = aPage();
const item = (over: Partial<TrashItem>): TrashItem => ({
  id: "0195f000-0000-7000-8000-0000000000e1",
  title: "Old plans",
  trashedAt: "2026-09-29T09:00:00Z",
  trashedByName: "Ada Lovelace",
  pages: 3,
  parentTitle: "Handbook",
  parentInTree: true,
  ...over,
});
const gone = item({});
const orphan = item({ id: "0195f000-0000-7000-8000-0000000000e2", title: "Notes", pages: 1, parentTitle: "Old plans", parentInTree: false });
const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const } };

describe("the trash", () => {
  it("lists what was deleted and where it goes back, and restores it", async () => {
    let items = [gone, orphan];
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/trash": () => ({ status: 200, body: { items } }),
      [`POST /spaces/DOCS/trash/${gone.id}/restore`]: () => {
        items = [orphan];
        return { status: 200, body: { page: aPage({ id: gone.id, title: gone.title, home: false }) } };
      },
    });
    await renderAt("/s/DOCS/settings?tab=trash");
    const row = (await screen.findByText("Old plans")).closest("tr")!;
    expect(row).toHaveTextContent("3 pages");
    expect(row).toHaveTextContent("Under Handbook");
    expect(screen.getByText("Notes").closest("tr")).toHaveTextContent("Under the home page, where it was is gone");
    expect(screen.getByRole("tab", { name: "Trash" })).toHaveAttribute("aria-selected", "true");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(row).getByRole("button", { name: "Restore Old plans" }));
    expect(await screen.findByText("Restored Old plans.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "POST" && r.path === `/spaces/DOCS/trash/${gone.id}/restore`)).toBe(true);
    await waitFor(() => expect(screen.queryByText("Old plans", { selector: "span" })).toBeNull());
  });

  it("lets an administrator purge an item and empty the trash, after asking", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValue(true);
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/trash": { status: 200, body: { items: [gone] } },
      [`DELETE /spaces/DOCS/trash/${gone.id}`]: { status: 204 },
      "DELETE /spaces/DOCS/trash": { status: 204 },
    });
    await renderAt("/s/DOCS/settings?tab=trash");
    const purge = await screen.findByRole("button", { name: "Delete Old plans for good" });
    await userEvent.click(purge);
    expect(sent.some((r) => r.method === "DELETE")).toBe(false);
    await userEvent.click(purge);
    expect(await screen.findByText("Deleted Old plans for good.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Empty trash" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === "/spaces/DOCS/trash")).toBe(true));
    expect(confirm).toHaveBeenCalledTimes(3);
  });

  it("offers a member restoring but not purging", async () => {
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space: aSpace({ can: { editPages: true, administer: false, delete: false, purgeTrash: false, addComments: true, deletePages: true } }) } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/trash": { status: 200, body: { items: [gone] } },
    });
    await renderAt("/s/DOCS/settings?tab=trash", { me: member });
    expect(await screen.findByRole("button", { name: "Restore Old plans" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete Old plans for good" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Empty trash" })).toBeNull();
  });
});

describe("moving a page to the trash", () => {
  it("asks first and lands on the page it was under", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const parent = { id: "0195f000-0000-7000-8000-0000000000f1", title: "Plans", home: false };
    const page = aPage({
      id: "0195f000-0000-7000-8000-0000000000f2",
      title: "Old plans",
      home: false,
      parentId: parent.id,
      ancestors: [{ id: home.id, title: "Handbook", home: true }, parent],
    });
    const sent = stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      [`GET /pages/${page.id}`]: { status: 200, body: { page, space } },
      [`GET /pages/${parent.id}`]: { status: 200, body: { page: aPage({ ...parent, parentId: home.id }), space } },
      [`DELETE /pages/${page.id}`]: { status: 204 },
    });
    const router = await renderAt(`/s/DOCS/p/${page.id}/old-plans`);
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Move to trash" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${parent.id}/plans`));
    expect(sent.some((r) => r.method === "DELETE" && r.path === `/pages/${page.id}`)).toBe(true);
  });

  it("is not offered for the home page", async () => {
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    });
    await renderAt("/s/DOCS");
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    expect(await screen.findByRole("menuitem", { name: "Copy" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Move to trash" })).toBeNull();
  });
});
