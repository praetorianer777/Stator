import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import type { Page } from "@/api/pages";
import type { PageReader, PageViews } from "@/api/pageviews";
import { PAGE_READERS_PAGE_SIZE } from "@/config";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const home = aPage();
const pageId = "0195f000-0000-7000-8000-0000000000e1";
const PATH = `/s/DOCS/p/${pageId}/guide`;
const counts = (over: Partial<PageViews> = {}): PageViews => ({
  views: 1234,
  readers: 56,
  recentViews: 78,
  recentReaders: 9,
  days: 30,
  canListReaders: true,
  ...over,
});
const reader = (n: number, over: Partial<PageReader> = {}): PageReader => ({
  id: `0195f000-0000-7000-8000-0000000001${String(n).padStart(2, "0")}`,
  name: `Reader ${n}`,
  viewedAt: "2026-09-30T08:00:00Z",
  days: n,
  ...over,
});

type Answers = Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;

function stubPage({ page = {}, more = {} }: { page?: Partial<Page>; more?: Answers } = {}) {
  const space = aSpace();
  const shown = aPage({ id: pageId, title: "Guide", home: false, parentId: home.id, ancestors: [{ id: home.id, title: "Handbook", home: true }], ...page });
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page: shown, space } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
    [`POST /pages/${pageId}/visit`]: { status: 204 },
    [`GET /pages/${pageId}/views`]: { status: 200, body: counts() },
    ...more,
  });
}

async function openViews() {
  await userEvent.click(await screen.findByRole("button", { name: "1,234 views by 56 people. Show the page views." }));
  return screen.findByRole("dialog", { name: "Page views" });
}

describe("a page's views", () => {
  it("shows how often the page was read under its title, and opens the counts in all and lately", async () => {
    stubPage({ more: { [`GET /pages/${pageId}/readers`]: { status: 200, body: { readers: [], unnamed: 0, retentionDays: 90, next: null } } } });
    await renderAt(PATH);
    const shown = await screen.findByRole("button", { name: /Show the page views/ });
    expect(shown).toHaveTextContent("1,234 views");
    const dialog = await openViews();
    const inAll = dialog.querySelector("[data-views-in-all]") as HTMLElement;
    expect(inAll).toHaveTextContent("In all");
    expect(inAll).toHaveTextContent("1,234 views");
    expect(inAll).toHaveTextContent("56 people");
    const lately = dialog.querySelector("[data-views-lately]") as HTMLElement;
    expect(lately).toHaveTextContent("Last 30 days");
    expect(lately).toHaveTextContent("78 views");
    expect(lately).toHaveTextContent("9 people");
    expect(within(dialog).getByText("A view counts each person once a day, however often they open the page.")).toBeInTheDocument();
    expect(await within(dialog).findByText("Nobody opened the page in this period.")).toBeInTheDocument();
  });

  it("lists the readers for an editor a window at a time, and says how many chose not to be named", async () => {
    const first = Array.from({ length: PAGE_READERS_PAGE_SIZE }, (_, i) => reader(i + 1));
    const sent = stubPage({
      more: {
        [`GET /pages/${pageId}/readers`]: (request) =>
          new URL(request.url).searchParams.get("cursor") === "after-first"
            ? { status: 200, body: { readers: [reader(99, { name: "Last Reader", days: 1 })], unnamed: 2, retentionDays: 90, next: null } }
            : { status: 200, body: { readers: first, unnamed: 2, retentionDays: 90, next: "after-first" } },
      },
    });
    await renderAt(PATH);
    const dialog = await openViews();
    const list = await within(dialog).findByRole("region", { name: "Who read it" });
    expect(within(list).getByText("People who opened the page in the last 90 days, the latest first.")).toBeInTheDocument();
    expect(list.querySelectorAll("[data-reader]")).toHaveLength(PAGE_READERS_PAGE_SIZE);
    const one = list.querySelector('[data-reader="Reader 1"]') as HTMLElement;
    expect(one).toHaveTextContent("Last opened Sep 30, 2026 · on 1 day");
    expect(list.querySelector('[data-reader="Reader 3"]')).toHaveTextContent("on 3 days");
    expect(within(list).getByText("2 more people chose not to be named.")).toBeInTheDocument();

    await userEvent.click(within(list).getByRole("button", { name: "Show more" }));
    await waitFor(() => expect(list.querySelector('[data-reader="Last Reader"]')).not.toBeNull());
    expect(within(list).queryByRole("button", { name: "Show more" })).toBeNull();
    expect(sent.filter((each) => each.method === "GET" && each.path === `/pages/${pageId}/readers`)).toHaveLength(2);
    expect(await axeViolations()).toEqual([]);
  });

  it("tells somebody who may not edit the page that only editors see who read it, and never asks", async () => {
    const sent = stubPage({ more: { [`GET /pages/${pageId}/views`]: { status: 200, body: counts({ canListReaders: false }) } } });
    await renderAt(PATH);
    const dialog = await openViews();
    expect(within(dialog).getByText("Only people who may edit this page see who read it.")).toBeInTheDocument();
    expect(within(dialog).getByRole("link", { name: "Whether your own name is shown is up to you, in your profile." })).toHaveAttribute(
      "href",
      "/settings/profile",
    );
    expect(sent.some((each) => each.path.endsWith("/readers"))).toBe(false);
    expect(await axeViolations()).toEqual([]);
  });

  it("counts again once the visit is noted, so the reader sees their own view", async () => {
    let asked = 0;
    stubPage({
      more: {
        [`GET /pages/${pageId}/views`]: () => {
          asked += 1;
          return { status: 200, body: counts({ views: asked === 1 ? 1 : 2, readers: 1 }) };
        },
      },
    });
    await renderAt(PATH);
    await waitFor(() => expect(document.querySelector("[data-page-views]")).toHaveAttribute("data-page-views", "2"));
    expect(screen.getByRole("button", { name: "2 views by 1 person. Show the page views." })).toBeInTheDocument();
  });

  it("asks nothing for a page that is not published", async () => {
    const sent = stubPage({ page: { unpublished: true, version: 0 } });
    await renderAt(PATH);
    expect(await screen.findByRole("heading", { level: 1, name: /Guide/ })).toBeInTheDocument();
    expect(sent.some((each) => each.path.endsWith("/views"))).toBe(false);
    expect(document.querySelector("[data-page-views]")).toBeNull();
  });

  it("leaves the line under the title alone when the counts are refused", async () => {
    stubPage({ more: { [`GET /pages/${pageId}/views`]: { status: 404, body: { error: { code: "not_found", message: "That page does not exist." } } } } });
    await renderAt(PATH);
    expect(await screen.findByRole("heading", { level: 1, name: /Guide/ })).toBeInTheDocument();
    await waitFor(() => expect(document.querySelector("[data-page-title]")).not.toBeNull());
    expect(document.querySelector("[data-page-views]")).toBeNull();
  });
});

describe("the profile's privacy switch", () => {
  it("hides the person's name from readers lists, and says they are still counted", async () => {
    const hidden: Me = { ...signedIn, user: { ...signedIn.user, showInReaders: false } };
    const sent = stubApi({ "PATCH /auth/me": { status: 200, body: hidden }, "GET /auth/me": { status: 200, body: signedIn } });
    await renderAt("/settings/profile");
    const toggle = await screen.findByRole("switch", { name: "Show my name to the editors of pages I read" });
    expect(toggle).toHaveAttribute("aria-checked", "true");
    expect(screen.queryByText("You are still counted when you read a page, only without your name.")).toBeNull();
    await userEvent.click(toggle);
    await waitFor(() => expect(toggle).toHaveAttribute("aria-checked", "false"));
    expect(sent.find((each) => each.method === "PATCH")).toEqual({ method: "PATCH", path: "/auth/me", body: { showInReaders: false } });
    expect(screen.getByText("You are still counted when you read a page, only without your name.")).toBeInTheDocument();
    expect(document.querySelector("[data-privacy-saved]")).toHaveTextContent("Your choice is saved.");
    expect(await axeViolations()).toEqual([]);
  });
});
