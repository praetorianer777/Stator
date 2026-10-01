import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { EditedPage, PageUpdate, Star } from "@/api/stars";
import { HOME_UPDATES_PAGE_SIZE } from "@/config";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const runbook = "0195f000-0000-7000-8000-0000000000c1";
const guide = "0195f000-0000-7000-8000-0000000000c2";

const starOnPage: Star = { kind: "page", spaceKey: "DOCS", spaceName: "Handbook", page: { id: runbook, title: "Runbook" }, starredAt: "2026-10-01T09:00:00Z" };
const starOnSpace: Star = { kind: "space", spaceKey: "OPS", spaceName: "Operations", page: null, starredAt: "2026-10-01T08:00:00Z" };

function update(over: Partial<PageUpdate> = {}): PageUpdate {
  return {
    id: guide,
    title: "Guide",
    spaceKey: "DOCS",
    spaceName: "Handbook",
    version: 3,
    publishedAt: "2026-10-01T10:00:00Z",
    authorName: "Grace Hopper",
    comment: "Tidied the steps",
    verified: false,
    ...over,
  };
}

const edited: EditedPage = {
  id: runbook,
  title: "Runbook",
  spaceKey: "DOCS",
  spaceName: "Handbook",
  editedAt: "2026-10-01T07:00:00Z",
  draft: true,
  unpublished: false,
};

type Stubs = Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;

function stubHome(more: Stubs = {}) {
  return stubApi({
    "GET /stars": { status: 200, body: { stars: [starOnPage, starOnSpace], next: null } },
    "GET /recent-pages": {
      status: 200,
      body: { pages: [{ id: guide, title: "Guide", spaceKey: "DOCS", spaceName: "Handbook", path: [], visitedAt: "2026-10-01T06:00:00Z" }] },
    },
    "GET /home/edited": { status: 200, body: { pages: [edited], next: null } },
    "GET /home/updates": (request) => {
      const scope = new URL(request.url).searchParams.get("scope");
      return { status: 200, body: { updates: scope === "watched" ? [] : [update()], next: null } };
    },
    ...more,
  });
}

function list(name: string) {
  return document.querySelector<HTMLElement>(`[data-home-list="${name}"]`)!;
}

describe("the home page", () => {
  it("lists the updates, the stars, the recent pages and the edits", async () => {
    stubHome();
    await renderAt("/");
    const row = await screen.findByText("Grace Hopper published version 3");
    expect(row.closest("li")).toHaveTextContent("Tidied the steps");
    expect(within(list("updates")).getByRole("link", { name: "Guide" })).toHaveAttribute("href", `/s/DOCS/p/${guide}/guide`);
    expect(within(list("starred")).getByRole("link", { name: "Runbook" })).toBeInTheDocument();
    expect(within(list("starred")).getByRole("link", { name: "Operations" })).toHaveAttribute("href", "/s/OPS");
    expect(within(list("recent")).getByRole("link", { name: "Guide" })).toBeInTheDocument();
    expect(within(list("edited")).getByText("Draft")).toBeInTheDocument();
    expect(screen.queryByText("Welcome to Stator")).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("says a first version created the page", async () => {
    stubHome({ "GET /home/updates": { status: 200, body: { updates: [update({ version: 1, comment: "", authorName: "" })], next: null } } });
    await renderAt("/");
    expect(await screen.findByText("Somebody created this page")).toBeInTheDocument();
  });

  it("marks a verified page among the updates", async () => {
    stubHome({
      "GET /home/updates": {
        status: 200,
        body: { updates: [update({ verified: true }), update({ id: runbook, title: "Runbook", verified: false })], next: null },
      },
    });
    await renderAt("/");
    const guideRow = (await within(list("updates")).findByRole("link", { name: "Guide" })).closest("li")!;
    expect(within(guideRow).getByText("Verified")).toHaveAttribute("data-verification-badge", "verified");
    const runbookRow = within(list("updates")).getByRole("link", { name: "Runbook" }).closest("li")!;
    expect(within(runbookRow).queryByText("Verified")).toBeNull();
  });

  it("switches to what the reader watches, from the keyboard too", async () => {
    const sent = stubHome();
    await renderAt("/");
    await screen.findByText("Grace Hopper published version 3");
    const all = screen.getByRole("tab", { name: "All updates" });
    all.focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Watched" })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByText(/Nothing you watch has changed yet/)).toBeInTheDocument();
    expect(sent.some((each) => each.path === "/home/updates" && each.method === "GET")).toBe(true);
  });

  it("reads the next window after the cursor it was given", async () => {
    const urls: string[] = [];
    const first = Array.from({ length: HOME_UPDATES_PAGE_SIZE }, (_, i) =>
      update({ id: `0195f000-0000-7000-8000-${String(i).padStart(12, "0")}`, title: `Page ${i}` }),
    );
    stubHome({
      "GET /home/updates": (request) => {
        const url = new URL(request.url);
        urls.push(url.search);
        return url.searchParams.get("cursor")
          ? { status: 200, body: { updates: [update({ title: "Older" })], next: null } }
          : { status: 200, body: { updates: first, next: "after-20" } };
      },
    });
    await renderAt("/");
    await userEvent.click(await within(list("updates")).findByRole("button", { name: "Show more" }));
    expect(await screen.findByRole("link", { name: "Older" })).toBeInTheDocument();
    expect(urls.some((each) => each.includes("cursor=after-20"))).toBe(true);
    expect(within(list("updates")).queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("takes a star off from the list and says so", async () => {
    let stars = [starOnPage, starOnSpace];
    const sent = stubHome({
      "GET /stars": () => ({ status: 200, body: { stars, next: null } }),
      [`DELETE /pages/${runbook}/star`]: () => {
        stars = [starOnSpace];
        return { status: 204 };
      },
    });
    await renderAt("/");
    await userEvent.click(await within(list("starred")).findByRole("button", { name: "Unstar Runbook" }));
    expect(await screen.findByText("Runbook is no longer starred.")).toBeInTheDocument();
    await waitFor(() => expect(within(list("starred")).queryByRole("link", { name: "Runbook" })).toBeNull());
    expect(sent.some((each) => each.method === "DELETE" && each.path === `/pages/${runbook}/star`)).toBe(true);
  });

  it("welcomes somebody with nothing in any list", async () => {
    stubApi({
      "GET /stars": { status: 200, body: { stars: [], next: null } },
      "GET /recent-pages": { status: 200, body: { pages: [] } },
      "GET /home/edited": { status: 200, body: { pages: [], next: null } },
      "GET /home/updates": { status: 200, body: { updates: [], next: null } },
    });
    await renderAt("/");
    expect(await screen.findByText("Welcome to Stator")).toBeInTheDocument();
    expect(screen.getByText(/Star a page or a space/)).toBeInTheDocument();
  });

  it("says when a list could not be read, and reads it again", async () => {
    let fail = true;
    stubHome({
      "GET /home/edited": () =>
        fail
          ? { status: 500, body: { error: { code: "internal", message: "Something went wrong." } } }
          : { status: 200, body: { pages: [edited], next: null } },
    });
    await renderAt("/");
    expect(await within(list("edited")).findByText("This list could not be loaded. Try again in a moment.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(within(list("edited")).getByRole("button", { name: "Retry" }));
    expect(await within(list("edited")).findByRole("link", { name: "Runbook" })).toBeInTheDocument();
  });
});

describe("the star buttons", () => {
  const space = aSpace();
  const shown = aPage({ id: runbook, title: "Runbook", home: false, parentId: aPage().id, ancestors: [{ id: aPage().id, title: "Handbook", home: true }] });

  function stubPage(more: Stubs = {}) {
    return stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      [`GET /pages/${runbook}`]: { status: 200, body: { page: shown, space } },
      [`GET /pages/${runbook}/attachments`]: { status: 200, body: { attachments: [] } },
      [`POST /pages/${runbook}/visit`]: { status: 204 },
      ...more,
    });
  }

  it("stars a page and takes the star off, saying its state rather than changing its name", async () => {
    let starred = false;
    const sent = stubPage({
      [`GET /pages/${runbook}`]: () => ({ status: 200, body: { page: { ...shown, starred }, space } }),
      [`PUT /pages/${runbook}/star`]: () => {
        starred = true;
        return { status: 204 };
      },
      [`DELETE /pages/${runbook}/star`]: () => {
        starred = false;
        return { status: 204 };
      },
    });
    await renderAt(`/s/DOCS/p/${runbook}/runbook`);
    const button = await screen.findByRole("button", { name: "Star Runbook" });
    expect(button).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(button);
    await waitFor(() => expect(screen.getByRole("button", { name: "Star Runbook" })).toHaveAttribute("aria-pressed", "true"));
    await userEvent.click(screen.getByRole("button", { name: "Star Runbook" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Star Runbook" })).toHaveAttribute("aria-pressed", "false"));
    expect(sent.filter((each) => each.path === `/pages/${runbook}/star`).map((each) => each.method)).toEqual(["PUT", "DELETE"]);
  });

  it("says when a star could not be changed", async () => {
    stubPage({ [`PUT /pages/${runbook}/star`]: { status: 500, body: { error: { code: "internal", message: "Something went wrong." } } } });
    await renderAt(`/s/DOCS/p/${runbook}/runbook`);
    await userEvent.click(await screen.findByRole("button", { name: "Star Runbook" }));
    expect(await screen.findByText("The star could not be changed. Try again in a moment.")).toBeInTheDocument();
  });

  it("stars a space from the directory", async () => {
    let starred = false;
    const sent = stubApi({
      "GET /spaces": () => ({ status: 200, body: { spaces: [{ ...space, starred }] } }),
      "PUT /spaces/DOCS/star": () => {
        starred = true;
        return { status: 204 };
      },
    });
    await renderAt("/spaces");
    const button = await screen.findByRole("button", { name: "Star the space Handbook" });
    await userEvent.click(button);
    await waitFor(() => expect(screen.getByRole("button", { name: "Star the space Handbook" })).toHaveAttribute("aria-pressed", "true"));
    expect(sent.some((each) => each.method === "PUT" && each.path === "/spaces/DOCS/star")).toBe(true);
    expect(await axeViolations()).toEqual([]);
  });
});
