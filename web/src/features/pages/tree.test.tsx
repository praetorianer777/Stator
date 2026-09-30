import { afterEach, describe, expect, it, vi } from "vitest";
import { createEvent, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TreeNode } from "@/api/tree";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { dropPlacement, dropZone } from "./PageTree";
import { placesFor } from "./PlaceDialog";

afterEach(() => vi.unstubAllGlobals());

// Every page view notes a visit, which is not the change a test makes.
const isChange = (r: { method: string; path: string }) => r.method === "POST" && !r.path.endsWith("/visit");

const space = aSpace();
const home = aPage();
const ids = { one: "0195f000-0000-7000-8000-0000000000a1", two: "0195f000-0000-7000-8000-0000000000a2", oneA: "0195f000-0000-7000-8000-0000000000b1" };
const node = (id: string, parentId: string, title: string, hasChildren = false): TreeNode => ({ id, parentId, title, hasChildren, unpublished: false, restricted: false });
const levels: Record<string, TreeNode[]> = {
  home: [node(ids.one, home.id, "One", true), node(ids.two, home.id, "Two")],
  [ids.one]: [node(ids.oneA, ids.one, "One A")],
};
const oneA = aPage({
  id: ids.oneA,
  title: "One A",
  home: false,
  parentId: ids.one,
  ancestors: [
    { id: home.id, title: "Handbook", home: true },
    { id: ids.one, title: "One", home: false },
  ],
});

/** The API a space with a small tree answers with; more answers are added on top. */
function stubTree(more: Record<string, Answer | ((request: Request) => Answer)> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    [`GET /pages/${ids.oneA}`]: { status: 200, body: { page: oneA, space } },
    "GET /spaces/DOCS/pages": (request) => ({ status: 200, body: { pages: levels[new URL(request.url).searchParams.get("parent") ?? "home"] ?? [] } }),
    "GET /spaces/DOCS/outline": {
      status: 200,
      body: {
        pages: [
          { id: home.id, parentId: null, title: "Handbook", depth: 0 },
          { id: ids.one, parentId: home.id, title: "One", depth: 1 },
          { id: ids.oneA, parentId: ids.one, title: "One A", depth: 2 },
          { id: ids.two, parentId: home.id, title: "Two", depth: 1 },
        ],
      },
    },
    ...more,
  });
}

const item = (title: string) => screen.getByRole("treeitem", { name: title });

describe("where a dragged page lands", () => {
  it("goes beside a row at its edges and under it in its middle", () => {
    expect(dropZone(2, 32)).toBe("before");
    expect(dropZone(16, 32)).toBe("inside");
    expect(dropZone(30, 32)).toBe("after");
    const two = node(ids.two, home.id, "Two");
    expect(dropPlacement(two, "inside")).toEqual({ parentId: ids.two });
    expect(dropPlacement(two, "before")).toEqual({ parentId: home.id, beforeId: ids.two });
    expect(dropPlacement(two, "after")).toEqual({ parentId: home.id, afterId: ids.two });
  });

  it("offers a move every place but the page itself and what is below it", () => {
    const outline = [
      { id: "h", parentId: null, title: "Home", depth: 0 },
      { id: "a", parentId: "h", title: "A", depth: 1 },
      { id: "a1", parentId: "a", title: "A1", depth: 2 },
      { id: "b", parentId: "h", title: "B", depth: 1 },
    ];
    const page = { id: "a", title: "A", spaceKey: "DOCS" };
    expect(placesFor(outline, page, true, true).map((each) => each.id)).toEqual(["h", "b"]);
    expect(placesFor(outline, page, true, false)).toHaveLength(4);
    expect(placesFor(outline, page, false, true)).toHaveLength(4);
  });
});

describe("the page tree", () => {
  it("is walked with the arrow keys and opens a level as it goes", async () => {
    stubTree();
    const router = await renderAt("/s/DOCS");
    await screen.findByRole("tree", { name: "Pages in Handbook" });
    const one = await screen.findByRole("treeitem", { name: "One" });
    expect(one).toHaveAttribute("tabindex", "0");
    expect(one).toHaveAttribute("aria-expanded", "false");
    one.focus();

    await userEvent.keyboard("{ArrowDown}");
    expect(item("Two")).toHaveFocus();
    await userEvent.keyboard("{ArrowUp}{ArrowRight}");
    expect(await screen.findByRole("treeitem", { name: "One A" })).toBeInTheDocument();
    expect(item("One")).toHaveAttribute("aria-expanded", "true");
    await userEvent.keyboard("{ArrowRight}");
    expect(item("One A")).toHaveFocus();
    expect(item("One A")).toHaveAttribute("aria-level", "2");
    await userEvent.keyboard("{ArrowLeft}");
    expect(item("One")).toHaveFocus();
    await userEvent.keyboard("{ArrowLeft}");
    expect(screen.queryByRole("treeitem", { name: "One A" })).toBeNull();

    await userEvent.keyboard("{End}{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${ids.two}/two`));
  });

  it("opens the way to the page being read and marks it", async () => {
    stubTree();
    await renderAt(`/s/DOCS/p/${ids.oneA}/one-a`);
    const current = await screen.findByRole("treeitem", { name: "One A" });
    expect(current).toHaveAttribute("aria-selected", "true");
    expect(current).toHaveAttribute("tabindex", "0");
    const crumbs = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(crumbs).getByRole("link", { name: "Handbook" })).toHaveAttribute("href", "/s/DOCS");
    expect(within(crumbs).getByRole("link", { name: "One" })).toHaveAttribute("href", `/s/DOCS/p/${ids.one}/one`);
    expect(await axeViolations()).toEqual([]);
  });

  it("moves a page dropped on the middle of another under it", async () => {
    const sent = stubTree({ [`POST /pages/${ids.two}/move`]: { status: 200, body: { page: aPage({ id: ids.two, title: "Two", home: false }) } } });
    await renderAt("/s/DOCS");
    const two = await screen.findByRole("treeitem", { name: "Two" });
    const oneRow = item("One").querySelector("[data-tree-row]")!;
    const dataTransfer = { setData: vi.fn(), effectAllowed: "", dropEffect: "" };

    fireEvent.dragStart(two, { dataTransfer });
    const over = createEvent.dragOver(oneRow, { dataTransfer, clientY: 0 });
    fireEvent(oneRow, over);
    expect(over.defaultPrevented).toBe(true);
    fireEvent.drop(oneRow, { dataTransfer, clientY: 0 });

    await waitFor(() => expect(sent.some(isChange)).toBe(true));
    expect(sent.find(isChange)?.body).toEqual({ parentId: ids.one });
    expect(await screen.findByText("Moved Two.")).toBeInTheDocument();
  });

  it("refuses a page dropped onto itself or below itself", async () => {
    const sent = stubTree();
    await renderAt(`/s/DOCS/p/${ids.oneA}/one-a`);
    const below = (await screen.findByRole("treeitem", { name: "One A" })).querySelector("[data-tree-row]")!;
    const one = item("One");
    const dataTransfer = { setData: vi.fn(), effectAllowed: "", dropEffect: "" };
    fireEvent.dragStart(one, { dataTransfer });
    const over = createEvent.dragOver(below, { dataTransfer, clientY: 0 });
    fireEvent(below, over);
    expect(over.defaultPrevented).toBe(false);
    fireEvent.drop(below, { dataTransfer, clientY: 0 });
    expect(sent.some(isChange)).toBe(false);
  });

  it("moves a page from the keyboard through a dialog", async () => {
    const sent = stubTree({
      [`POST /pages/${ids.two}/move`]: { status: 200, body: { page: aPage({ id: ids.two, title: "Two", home: false, parentId: ids.one }) } },
    });
    const router = await renderAt("/s/DOCS");
    (await screen.findByRole("treeitem", { name: "Two" })).focus();
    await userEvent.keyboard("m");
    const dialog = await screen.findByRole("dialog", { name: "Move Two" });
    const parent = within(dialog).getByLabelText("Put it under");
    await waitFor(() => expect(within(parent).getAllByRole("option")).toHaveLength(3));
    expect(within(parent).queryByRole("option", { name: /Two/ })).toBeNull();
    await userEvent.selectOptions(parent, ids.one);
    await userEvent.click(within(dialog).getByRole("button", { name: "Move page" }));

    await waitFor(() => expect(sent.some(isChange)).toBe(true));
    expect(sent.find(isChange)?.body).toEqual({ parentId: ids.one, withChildren: true });
    await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${ids.two}/two`));
  });
});

describe("a new page", () => {
  it("is named in a dialog and opens in the editor", async () => {
    const made = aPage({ id: "0195f000-0000-7000-8000-0000000000c1", title: "Onboarding", home: false, parentId: home.id });
    const sent = stubTree({ "POST /pages": { status: 201, body: { page: made } } });
    const router = await renderAt("/s/DOCS");
    await userEvent.click(await screen.findByRole("button", { name: "New page" }));
    const dialog = await screen.findByRole("dialog", { name: "New page under Handbook" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Create page" }));
    expect(await within(dialog).findByText("A page needs a title.")).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText("Title"), "Onboarding");
    await userEvent.click(within(dialog).getByRole("button", { name: "Create page" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${made.id}/onboarding/edit`));
    expect(sent.find(isChange)?.body).toEqual({ parentId: home.id, title: "Onboarding" });
  });
});
