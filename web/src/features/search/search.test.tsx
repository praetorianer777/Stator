import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Hit, PageHit, RecentPage } from "@/api/search";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const space = aSpace();
const pageHit = (id: string, title: string, path: string[] = ["Handbook"]): PageHit => ({ id, title, spaceKey: "DOCS", spaceName: "Handbook", path });
const onboarding = pageHit("0195f000-0000-7000-8000-0000000000a1", "Onboarding");
const oncall = pageHit("0195f000-0000-7000-8000-0000000000a2", "On call", ["Handbook", "Operations"]);
const recent: RecentPage = { ...pageHit("0195f000-0000-7000-8000-0000000000a3", "Release checklist"), visitedAt: "2026-09-29T10:00:00Z" };

const hit = (over: Partial<Hit> = {}): Hit => ({
  type: "page",
  page: { id: onboarding.id, title: "Onboarding", spaceKey: "DOCS", spaceName: "Handbook" },
  attachmentId: null,
  commentId: null,
  title: [{ text: "Onboarding", match: true }],
  snippet: [
    { text: "Your first week: ", match: false },
    { text: "onboarding", match: true },
    { text: " <b>starts</b> here.", match: false },
  ],
  labels: [],
  updatedAt: "2026-09-29T08:00:00Z",
  updatedByName: "Ada Lovelace",
  ...over,
});

const results = (hits: Hit[], total = hits.length, offset = 0): Answer => ({ status: 200, body: { hits, total, limit: 20, offset } });
const query = (request: Request) => new URL(request.url).searchParams;

async function openPalette() {
  await userEvent.keyboard("{Control>}k{/Control}");
  return screen.getByRole("combobox");
}

describe("quick search", () => {
  it("lists recent pages until something is typed, then page titles as they are typed", async () => {
    const asked: string[] = [];
    stubApi({
      "GET /recent-pages": { status: 200, body: { pages: [recent] } },
      "GET /search/quick": (request) => {
        asked.push(query(request).get("q") ?? "");
        return { status: 200, body: { pages: [onboarding, oncall] } };
      },
    });
    await renderAt("/");
    const input = await openPalette();
    const recentList = await screen.findByRole("listbox", { name: "Recent pages" });
    expect(within(recentList).getByRole("option", { name: /Release checklist/ })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.type(input, "on");
    const found = await screen.findByRole("listbox", { name: "Pages" });
    expect(
      within(found)
        .getAllByRole("option")
        .map((option) => option.getAttribute("data-quick-search-option")),
    ).toEqual(["Onboarding", "On call"]);
    expect(within(found).getByRole("option", { name: /On call/ })).toHaveTextContent("Handbook / Operations");
    // The typing delay means the server hears the words once they settle, not every key.
    expect(asked).toEqual(["on"]);
    expect(input).toHaveAttribute("aria-expanded", "true");
    expect(input).toHaveAttribute("aria-controls", found.id);
    expect(await axeViolations()).toEqual([]);
  });

  it("moves through the options with the arrow keys and opens the active one with Enter", async () => {
    stubApi({
      "GET /recent-pages": { status: 200, body: { pages: [] } },
      "GET /search/quick": { status: 200, body: { pages: [onboarding, oncall] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      [`GET /pages/${oncall.id}`]: { status: 200, body: { page: aPage({ id: oncall.id, title: "On call", home: false }), space } },
      [`POST /pages/${oncall.id}/visit`]: { status: 204 },
    });
    const router = await renderAt("/");
    const input = await openPalette();
    await userEvent.type(input, "on");
    const options = await within(await screen.findByRole("listbox")).findAllByRole("option");
    expect(input).not.toHaveAttribute("aria-activedescendant");

    await userEvent.keyboard("{ArrowDown}");
    expect(input).toHaveAttribute("aria-activedescendant", options[0]!.id);
    expect(options[0]).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    expect(input).toHaveAttribute("aria-activedescendant", options[1]!.id);
    await userEvent.keyboard("{ArrowUp}{ArrowUp}");
    expect(input).not.toHaveAttribute("aria-activedescendant");
    await userEvent.keyboard("{ArrowDown}{ArrowDown}{Enter}");

    await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${oncall.id}/on-call`));
    expect(screen.queryByRole("dialog", { name: "Quick search" })).toBeNull();
  });

  it("opens the full search for the words when Enter is pressed on the query itself", async () => {
    stubApi({
      "GET /recent-pages": { status: 200, body: { pages: [] } },
      "GET /search/quick": { status: 200, body: { pages: [onboarding] } },
      "GET /search": results([hit()]),
      "GET /spaces": { status: 200, body: { spaces: [space] } },
    });
    const router = await renderAt("/");
    const input = await openPalette();
    await userEvent.type(input, "first week{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/search"));
    expect(router.state.location.search).toEqual({ q: "first week" });
    expect(await screen.findByRole("searchbox", { name: "Search for" })).toHaveValue("first week");
  });

  it("says when nothing starts with the words, and closes on Escape", async () => {
    stubApi({
      "GET /recent-pages": { status: 200, body: { pages: [] } },
      "GET /search/quick": { status: 200, body: { pages: [] } },
    });
    await renderAt("/");
    const input = await openPalette();
    expect(await screen.findByText("Pages you open are listed here, the latest first.")).toBeInTheDocument();
    await userEvent.type(input, "zebra");
    expect(await screen.findByText(/No page title starts with that/)).toBeInTheDocument();
    expect(input).toHaveAttribute("aria-expanded", "false");
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("the full search", () => {
  it("shows hits with their matches marked, and never reads a snippet as markup", async () => {
    stubApi({
      "GET /search": results([
        hit(),
        hit({ type: "attachment", attachmentId: "0195f000-0000-7000-8000-0000000000b1", title: [{ text: "onboarding.pdf", match: true }] }),
      ]),
      "GET /spaces": { status: 200, body: { spaces: [space] } },
    });
    await renderAt("/search?q=onboarding");
    const rows = await screen.findAllByRole("listitem");
    const first = rows.find((row) => row.getAttribute("data-hit-type") === "page")!;
    expect(within(first).getByRole("link", { name: "Onboarding" })).toHaveAttribute("href", `/s/DOCS/p/${onboarding.id}/onboarding`);
    const snippet = first.querySelector("[data-search-snippet]")!;
    expect([...snippet.querySelectorAll("mark")].map((mark) => mark.textContent)).toEqual(["onboarding"]);
    expect(snippet.querySelector("b")).toBeNull();
    expect(snippet).toHaveTextContent("Your first week: onboarding <b>starts</b> here.");
    const file = rows.find((row) => row.getAttribute("data-hit-type") === "attachment")!;
    expect(file).toHaveTextContent("File");
    expect(file).toHaveTextContent("On Onboarding");
    expect(screen.getByText("2 results")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("keeps every filter in the address and sends each to the API", async () => {
    const sent: URLSearchParams[] = [];
    stubApi({
      "GET /search": (request) => {
        sent.push(query(request));
        return results([hit()]);
      },
      "GET /spaces": { status: 200, body: { spaces: [space, aSpace({ id: "s2", key: "ENG", name: "Engineering" })] } },
      "GET /people": { status: 200, body: { people: [{ id: "0195f000-0000-7000-8000-0000000000c1", name: "Grace Hopper", email: "grace@stator.test" }] } },
    });
    const router = await renderAt("/search?q=onboarding");
    await screen.findByText("1 result");

    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Space" }), "ENG");
    await userEvent.click(screen.getByRole("checkbox", { name: "Pages" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Comments" }));
    await userEvent.type(screen.getByRole("textbox", { name: "Labels" }), "howto, team{Enter}");
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Changed by" }), `You (${signedIn.user.name})`);
    await userEvent.type(screen.getByLabelText("Changed on or after"), "2026-09-01");
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Order" }), "updated");

    await waitFor(() =>
      expect(router.state.location.search).toEqual({
        q: "onboarding",
        space: "ENG",
        type: "page,comment",
        label: "howto,team",
        author: signedIn.user.id,
        updatedAfter: "2026-09-01",
        sort: "updated",
      }),
    );
    await waitFor(() => expect(sent.at(-1)?.get("sort")).toBe("updated"));
    const last = sent.at(-1)!;
    expect(last.get("q")).toBe("onboarding");
    expect(last.getAll("space")).toEqual(["ENG"]);
    expect(last.getAll("type")).toEqual(["page", "comment"]);
    expect(last.getAll("label")).toEqual(["howto", "team"]);
    expect(last.getAll("author")).toEqual([signedIn.user.id]);
    expect(last.get("updatedAfter")).toBe("2026-09-01");
    expect(screen.getByRole("option", { name: "Grace Hopper" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "onboarding", sort: "updated" }));
  });

  it("pages through the hits with the offset in the request and the page in the address", async () => {
    const offsets: string[] = [];
    stubApi({
      "GET /search": (request) => {
        const offset = query(request).get("offset") ?? "0";
        offsets.push(offset);
        return results([hit()], 45, Number(offset));
      },
      "GET /spaces": { status: 200, body: { spaces: [space] } },
    });
    const router = await renderAt("/search?q=onboarding");
    expect(await screen.findByText("1 to 1 of 45 results")).toBeInTheDocument();
    expect(screen.getByText("Page 1 of 3")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "onboarding", page: 2 }));
    expect(await screen.findByText("Page 2 of 3")).toBeInTheDocument();
    expect(offsets).toEqual(["0", "20"]);
  });

  it("says when nothing matches", async () => {
    stubApi({ "GET /search": results([]), "GET /spaces": { status: 200, body: { spaces: [space] } } });
    await renderAt("/search?q=zebra");
    expect(await screen.findByText("Nothing matches")).toBeInTheDocument();
  });

  it("says in a sentence when search fails, and passes on what the API says of a refused query", async () => {
    stubApi({ "GET /search": { status: 500, body: { error: { code: "internal", message: "boom" } } }, "GET /spaces": { status: 200, body: { spaces: [] } } });
    await renderAt("/search?q=onboarding");
    expect(await screen.findByRole("alert")).toHaveTextContent("Search is not answering right now. Try again in a moment.");

    vi.unstubAllGlobals();
    stubApi({
      "GET /search": { status: 422, body: { error: { code: "invalid", message: "Keep the words to 200 characters or fewer.", fields: { q: "too long" } } } },
      "GET /spaces": { status: 200, body: { spaces: [] } },
    });
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Keep the words to 200 characters or fewer."));
  });
});

describe("a page view", () => {
  it("notes the visit once, without holding up the page", async () => {
    const page = aPage({ id: onboarding.id, title: "Onboarding", home: false });
    let release: () => void = () => {};
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      [`GET /pages/${page.id}`]: { status: 200, body: { page, space } },
      [`POST /pages/${page.id}/visit`]: { status: 204 },
    });
    const fetch = globalThis.fetch;
    // The visit never answers, and the page still shows.
    vi.stubGlobal("fetch", (request: Request) =>
      request.method === "POST" ? fetch(request).then((response) => new Promise<Response>((resolve) => (release = () => resolve(response)))) : fetch(request),
    );
    await renderAt(`/s/DOCS/p/${page.id}/onboarding`);
    expect(await screen.findByText("Welcome to the handbook.")).toBeInTheDocument();
    await waitFor(() => expect(sent.filter((r) => r.method === "POST" && r.path === `/pages/${page.id}/visit`)).toHaveLength(1));
    release();
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(sent.filter((r) => r.method === "POST")).toHaveLength(1);
  });
});
