import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import type { Watch, Watching } from "@/api/watching";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const home = aPage();
const pageId = "0195f000-0000-7000-8000-0000000000c1";
const PATH = `/s/DOCS/p/${pageId}/runbook`;
const none: Watching = { page: false, subtree: false, inherited: null };

function stubPage({
  watching = none,
  space = aSpace(),
  page = {},
  more = {},
}: {
  watching?: Watching;
  space?: Space;
  page?: Partial<Page>;
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const shown = aPage({
    id: pageId,
    title: "Runbook",
    home: false,
    parentId: home.id,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    watching,
    ...page,
  });
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page: shown, space } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
    [`POST /pages/${pageId}/visit`]: { status: 204 },
    ...more,
  });
}

async function openMenu() {
  await userEvent.click(await screen.findByRole("button", { name: /^(Watch|Watching)$/ }));
  return screen.getByRole("menu", { name: "Watching Runbook" });
}

describe("the watch button", () => {
  it("watches the page alone, or with every page below it", async () => {
    let watching = none;
    const shown = aPage({ id: pageId, title: "Runbook", home: false, parentId: home.id, ancestors: [{ id: home.id, title: "Handbook", home: true }] });
    const sent = stubPage({
      more: {
        [`GET /pages/${pageId}`]: () => ({ status: 200, body: { page: { ...shown, watching }, space: aSpace() } }),
        [`PUT /pages/${pageId}/watch`]: async (request) => {
          watching = { ...none, ...((await request.json()).subtree ? { subtree: true } : { page: true }) };
          return { status: 200, body: { watching } };
        },
      },
    });
    await renderAt(PATH);
    expect(await screen.findByRole("button", { name: "Watch" })).toHaveAttribute("aria-haspopup", "menu");
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Watch this page" }));
    await waitFor(() => expect(sent.find((each) => each.method === "PUT")?.body).toEqual({}));
    expect(await screen.findByRole("button", { name: "Watching" })).toBeInTheDocument();
    const menu = await openMenu();
    expect(within(menu).getByRole("menuitem", { name: "Watch this page (on)" })).toBeInTheDocument();
    await userEvent.click(within(menu).getByRole("menuitem", { name: "Watch this page and every page below it" }));
    await waitFor(() => expect(sent.filter((each) => each.method === "PUT")[1]?.body).toEqual({ subtree: true }));
  });

  it("stops watching the page, and watches the whole space or stops that", async () => {
    const sent = stubPage({
      watching: { ...none, page: true },
      more: {
        [`DELETE /pages/${pageId}/watch`]: { status: 204 },
        "PUT /spaces/DOCS/watch": { status: 204 },
      },
    });
    await renderAt(PATH);
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Stop watching this page" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/pages/${pageId}/watch`)).toBe(true));
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Watch the whole space" }));
    await waitFor(() => expect(sent.some((each) => each.method === "PUT" && each.path === "/spaces/DOCS/watch")).toBe(true));
  });

  it("says what covers the page when a watch above it does", async () => {
    stubPage({ watching: { ...none, inherited: { kind: "subtree", page: { id: home.id, title: "Handbook" } } } });
    await renderAt(PATH);
    const menu = await openMenu();
    expect(within(menu).getByRole("menuitem", { name: "Covered by your watch on Handbook and the pages below it" })).toHaveAttribute("aria-disabled", "true");
    expect(within(menu).queryByRole("menuitem", { name: "Stop watching this page" })).toBeNull();
    expect(screen.getByRole("button", { name: "Watching" })).toBeInTheDocument();
  });

  it("offers to stop watching the space the reader watches", async () => {
    const sent = stubPage({ space: aSpace({ watching: true }), more: { "DELETE /spaces/DOCS/watch": { status: 204 } } });
    await renderAt(PATH);
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Stop watching the space" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === "/spaces/DOCS/watch")).toBe(true));
  });

  it("lists who is watching, and through what", async () => {
    stubPage({
      more: {
        [`GET /pages/${pageId}/watchers`]: {
          status: 200,
          body: {
            watchers: [
              { userId: "u1", name: "Ann Archer", email: "ann@stator.test", via: "page", viaPage: null },
              { userId: "u2", name: "Bob Builder", email: "bob@stator.test", via: "subtree", viaPage: { id: home.id, title: "Handbook" } },
              { userId: "u3", name: "Cleo Carter", email: "cleo@stator.test", via: "space", viaPage: null },
            ],
            total: 3,
            limit: 100,
            offset: 0,
          },
        },
      },
    });
    await renderAt(PATH);
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Who is watching" }));
    const dialog = await screen.findByRole("dialog", { name: "Who is watching Runbook" });
    const rows = await within(dialog).findAllByRole("listitem");
    expect(rows.map((row) => row.textContent)).toEqual([
      expect.stringContaining("Ann ArcherWatches this page"),
      expect.stringContaining("Bob BuilderWatches Handbook and the pages below it"),
      expect.stringContaining("Cleo CarterWatches the whole space"),
    ]);
    expect(await axeViolations()).toEqual([]);
  });

  it("says in a sentence when watching could not be changed", async () => {
    stubPage({ more: { [`PUT /pages/${pageId}/watch`]: { status: 500, body: { error: { code: "internal", message: "Broken." } } } } });
    await renderAt(PATH);
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Watch this page" }));
    expect(await screen.findByText("Watching could not be changed. Try again in a moment.")).toBeInTheDocument();
  });

  it("is not offered on a page nobody else can see yet", async () => {
    stubPage({ page: { unpublished: true, version: 0 } });
    await renderAt(PATH);
    await screen.findByRole("heading", { name: /Runbook/ });
    expect(screen.queryByRole("button", { name: /^(Watch|Watching)$/ })).toBeNull();
  });
});

const watches: Watch[] = [
  { kind: "subtree", spaceKey: "DOCS", spaceName: "Handbook", page: { id: pageId, title: "Runbook" }, createdAt: "2026-09-30T08:00:00Z" },
  { kind: "space", spaceKey: "OPS", spaceName: "Operations", page: null, createdAt: "2026-09-29T08:00:00Z" },
];

describe("the watching list", () => {
  it("lists the reader's watches and stops one", async () => {
    let listed = watches;
    const sent = stubApi({
      "GET /spaces": { status: 200, body: { spaces: [aSpace()] } },
      "GET /watches": () => ({ status: 200, body: { watches: listed, total: listed.length, limit: 20, offset: 0 } }),
      "DELETE /spaces/OPS/watch": () => {
        listed = watches.slice(0, 1);
        return { status: 204 };
      },
    });
    await renderAt("/settings/watching");
    const table = await screen.findByRole("table");
    expect(within(table).getByRole("link", { name: "Runbook" })).toHaveAttribute("href", `/s/DOCS/p/${pageId}/runbook`);
    expect(within(table).getByText("This page and the pages below")).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: "Operations" })).toHaveAttribute("href", "/s/OPS");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Stop watching Space Operations" }));
    expect(await screen.findByRole("status")).toHaveTextContent("You no longer watch Space Operations.");
    expect(sent.some((each) => each.method === "DELETE" && each.path === "/spaces/OPS/watch")).toBe(true);
    await waitFor(() => expect(screen.queryByRole("link", { name: "Operations" })).toBeNull());
  });

  it("names a watch on a blog, links to the blog, and stops it on its own route", async () => {
    const blogWatch: Watch = { kind: "blog", spaceKey: "OPS", spaceName: "Operations", page: null, createdAt: "2026-09-29T08:00:00Z" };
    const sent = stubApi({
      "GET /spaces": { status: 200, body: { spaces: [aSpace()] } },
      "GET /watches": { status: 200, body: { watches: [blogWatch], total: 1, limit: 20, offset: 0 } },
      "DELETE /spaces/OPS/blog/watch": { status: 204 },
    });
    await renderAt("/settings/watching");
    const table = await screen.findByRole("table");
    expect(within(table).getByRole("link", { name: "Blog of Operations" })).toHaveAttribute("href", "/s/OPS/blog");
    expect(within(table).getByText("New posts in the blog")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Stop watching Blog of Operations" }));
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === "/spaces/OPS/blog/watch")).toBe(true));
    expect(sent.some((each) => each.path === "/spaces/OPS/watch")).toBe(false);
  });

  it("says what to do when the reader watches nothing", async () => {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [aSpace()] } },
      "GET /watches": { status: 200, body: { watches: [], total: 0, limit: 20, offset: 0 } },
    });
    await renderAt("/settings/watching");
    expect(await screen.findByText("You watch nothing yet")).toBeInTheDocument();
  });
});
