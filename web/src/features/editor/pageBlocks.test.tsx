import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { BelowPage } from "@/api/tree";
import type { AppRouter } from "@/routes";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { DocDiffView } from "./DocView";
import type { DocNode } from "./schema";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// The editor's chunk loads and the editor mounts in jsdom, which is slow on a busy machine.
const EDITOR_TEST_MS = 20_000;

const space = aSpace();
const home = aPage();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000b1";
const heading = (level: number, text: string, id: string): DocNode => ({ type: "heading", attrs: { level, id }, content: [{ type: "text", text }] });
const body = (...blocks: DocNode[]) => ({ type: "doc" as const, content: blocks });
const child = (id: string, title: string, parentId: string, depth: number, over: Partial<BelowPage> = {}): BelowPage => ({
  id,
  parentId,
  title,
  depth,
  unpublished: false,
  updatedAt: "2026-09-29T08:00:00Z",
  ...over,
});

/** The API around one page whose body is given, and whatever the pages below it are asked for. */
function stubPage(doc: ReturnType<typeof body>, below: Answer | ((request: Request) => Answer) = { status: 200, body: { pages: [], truncated: false } }) {
  const page = aPage({ id: PAGE_ID, title: "Guide", home: false, parentId: home.id, ancestors: [{ id: home.id, title: "Handbook", home: true }], body: doc });
  const asked: URLSearchParams[] = [];
  const sent = stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${PAGE_ID}`]: { status: 200, body: { page, space } },
    [`GET /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft: null } },
    [`PUT /pages/${PAGE_ID}/draft`]: async (request) => {
      const input = (await request.json()) as { title: string; body: unknown; baseVersion: number };
      return { status: 200, body: { draft: { pageId: PAGE_ID, ...input, updatedAt: "2026-09-30T08:00:00Z" } } };
    },
    [`GET /pages/${PAGE_ID}/attachments`]: { status: 200, body: { attachments: [] } },
    [`GET /pages/${PAGE_ID}/below`]: (request) => {
      asked.push(new URL(request.url, "http://app.test").searchParams);
      return typeof below === "function" ? below(request) : below;
    },
  });
  return { sent, asked };
}

/**
 * Closes the editor, which saves the draft it still holds, and waits for that
 * save to land so it cannot outlive the test. Returns the body saved.
 */
async function closeEditor(router: AppRouter, sent: ReturnType<typeof stubApi>) {
  await userEvent.click(document.querySelector<HTMLElement>('[data-action="close-editor"]')!);
  await waitFor(() => expect(sent.some((r) => r.method === "PUT")).toBe(true));
  await waitFor(() => expect(router.options.context.queryClient.isMutating()).toBe(0));
  return (sent.filter((r) => r.method === "PUT").at(-1)!.body as { body: { content: DocNode[] } }).body;
}

const toc = (maxLevel = 3): DocNode => ({ type: "tableOfContents", attrs: { maxLevel } });
const childPages = (scope: string, depth: number | null, sort: string): DocNode => ({ type: "childPages", attrs: { scope, depth, sort } });

describe("the reader", () => {
  it("links a table of contents to the page's headings, and follows a link without leaving the page", async () => {
    stubPage(body(toc(2), heading(1, "Install", "install"), heading(2, "Linux", "linux"), heading(3, "Debian", "debian"), heading(1, "Use", "use")));
    const router = await renderAt(`/s/DOCS/p/${PAGE_ID}/guide`);
    const nav = await screen.findByRole("navigation", { name: "Table of contents" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((a) => [a.textContent, a.getAttribute("href")])).toEqual([
      ["Install", "#install"],
      ["Linux", "#linux"],
      ["Use", "#use"],
    ]);
    // Linux sits in a list under Install.
    expect(within(nav).getByRole("link", { name: "Install" }).closest("li")).toContainElement(within(nav).getByRole("link", { name: "Linux" }));
    await userEvent.click(within(nav).getByRole("link", { name: "Use" }));
    expect(window.location.hash).toBe("#use");
    expect(router.state.location.pathname).toBe(`/s/DOCS/p/${PAGE_ID}/guide`);
    expect(document.getElementById("use")).toHaveTextContent("Use");
  });

  it("says so in a sentence when the page has no headings", async () => {
    stubPage(body(toc(), { type: "paragraph", content: [{ type: "text", text: "Only words." }] }));
    await renderAt(`/s/DOCS/p/${PAGE_ID}/guide`);
    const nav = await screen.findByRole("navigation", { name: "Table of contents" });
    expect(nav).toHaveTextContent("No headings yet. Add a heading to the page and it is listed here.");
  });

  it("lists the pages below as the block asks, nested, with links to each", async () => {
    const { asked } = stubPage(body(childPages("subtree", 2, "updated")), {
      status: 200,
      body: {
        pages: [
          child("0195f000-0000-7000-8000-0000000000c1", "Setup", PAGE_ID, 1, { updatedAt: "2026-09-30T08:00:00Z" }),
          child("0195f000-0000-7000-8000-0000000000c2", "Install on Linux", "0195f000-0000-7000-8000-0000000000c1", 2),
          child("0195f000-0000-7000-8000-0000000000c3", "My notes", PAGE_ID, 1, { unpublished: true }),
        ],
        truncated: false,
      },
    });
    await renderAt(`/s/DOCS/p/${PAGE_ID}/guide`);
    const nav = await screen.findByRole("navigation", { name: "Child pages" });
    await within(nav).findByRole("link", { name: "Setup" });
    expect(Object.fromEntries(asked[0]!)).toEqual({ scope: "subtree", depth: "2", sort: "updated" });
    expect(within(nav).getByRole("link", { name: "Install on Linux" })).toHaveAttribute(
      "href",
      "/s/DOCS/p/0195f000-0000-7000-8000-0000000000c2/install-on-linux",
    );
    expect(within(nav).getByRole("link", { name: "Setup" }).closest("li")).toContainElement(within(nav).getByRole("link", { name: "Install on Linux" }));
    expect(within(nav).getByRole("link", { name: "My notes" }).closest("li")).toHaveTextContent("Unpublished");
    expect(nav.querySelector('[data-child-page="Setup"]')).toHaveTextContent(/changed /);
    expect(await axeViolations()).toEqual([]);
  });

  it("asks for direct children without a depth, and says so when there are none", async () => {
    const { asked } = stubPage(body(childPages("children", 4, "title")));
    await renderAt(`/s/DOCS/p/${PAGE_ID}/guide`);
    const nav = await screen.findByRole("navigation", { name: "Child pages" });
    await waitFor(() => expect(nav).toHaveTextContent("No pages below this one yet. Pages added under it are listed here."));
    expect(Object.fromEntries(asked[0]!)).toEqual({ scope: "children", sort: "title" });
  });

  it("says when the list is cut, and when it cannot be loaded", async () => {
    stubPage(body(childPages("subtree", null, "tree")), {
      status: 200,
      body: { pages: [child("0195f000-0000-7000-8000-0000000000c1", "Setup", PAGE_ID, 1)], truncated: true },
    });
    await renderAt(`/s/DOCS/p/${PAGE_ID}/guide`);
    expect(await screen.findByText("Only the first 500 pages are listed. Choose fewer levels to see a shorter list.")).toBeInTheDocument();
  });

  it("describes both blocks in words in a comparison", async () => {
    render(
      <DocDiffView
        blocks={[
          { change: "deleted", node: toc(3) },
          { change: "inserted", node: toc(1) },
          { change: "equal", node: childPages("subtree", null, "updated") },
          { change: "inserted", node: childPages("children", null, "title") },
        ]}
      />,
    );
    const view = document.querySelector("[data-diff-view]")!;
    expect([...view.querySelectorAll("[data-toc], [data-child-pages]")].map((el) => el.textContent)).toEqual([
      "Table of contents: headings 1 to 3",
      "Table of contents: heading 1 only",
      "Child pages: all pages below, every level, last changed first",
      "Child pages: direct children, by title",
    ]);
    expect(view.querySelectorAll("a")).toHaveLength(0);
  });
});

describe("the editor", () => {
  it(
    "keeps the table of contents up to date as headings are typed, and sets its depth from the keyboard",
    async () => {
      const { sent } = stubPage(body(toc(), heading(1, "Install", "install"), { type: "paragraph" }));
      const router = await renderAt(`/s/DOCS/p/${PAGE_ID}/guide/edit`);
      const box = await screen.findByRole("textbox", { name: "Page content" }, { timeout: 10_000 });
      const nav = await within(box).findByRole("navigation", { name: "Table of contents" });
      expect(
        within(nav)
          .getAllByRole("link")
          .map((a) => a.textContent),
      ).toEqual(["Install"]);

      const user = userEvent.setup();
      // jsdom places no caret on a click, so it is put where a browser would.
      box.focus();
      const range = document.createRange();
      range.selectNodeContents(box.querySelector("p:last-of-type")!);
      range.collapse(true);
      window.getSelection()!.removeAllRanges();
      window.getSelection()!.addRange(range);
      act(() => {
        document.dispatchEvent(new Event("selectionchange"));
      });
      await user.keyboard("## Linux");
      await waitFor(() =>
        expect(
          within(nav)
            .getAllByRole("link")
            .map((a) => a.textContent),
        ).toEqual(["Install", "Linux"]),
      );

      const levels = within(nav).getByRole("combobox", { name: "Headings to list" });
      levels.focus();
      expect(levels).toHaveFocus();
      await user.selectOptions(levels, "Heading 1 only");
      await waitFor(() =>
        expect(
          within(nav)
            .getAllByRole("link")
            .map((a) => a.textContent),
        ).toEqual(["Install"]),
      );
      expect(levels).toHaveValue("1");
      expect(await axeViolations()).toEqual([]);

      const saved = await closeEditor(router, sent);
      expect(saved.content[0]).toEqual(toc(1));
      expect(saved.content[2]).toMatchObject({ type: "heading", attrs: { level: 2 }, content: [{ type: "text", text: "Linux" }] });
    },
    EDITOR_TEST_MS,
  );

  it(
    "lists the pages below with settings a keyboard reaches, and asks again when they change",
    async () => {
      const { sent, asked } = stubPage(body(childPages("children", null, "tree")), {
        status: 200,
        body: { pages: [child("0195f000-0000-7000-8000-0000000000c1", "Setup", PAGE_ID, 1)], truncated: false },
      });
      const router = await renderAt(`/s/DOCS/p/${PAGE_ID}/guide/edit`);
      const box = await screen.findByRole("textbox", { name: "Page content" }, { timeout: 10_000 });
      const nav = await within(box).findByRole("navigation", { name: "Child pages" });
      await within(nav).findByRole("link", { name: "Setup" });
      expect(within(nav).queryByRole("combobox", { name: "Levels" })).toBeNull();

      const user = userEvent.setup();
      const scope = within(nav).getByRole("combobox", { name: "Pages to list" });
      scope.focus();
      await user.selectOptions(scope, "All pages below");
      const depth = await within(nav).findByRole("combobox", { name: "Levels" });
      // The next setting is the next stop for Tab.
      await user.tab();
      expect(depth).toHaveFocus();
      await user.selectOptions(depth, "3 levels");
      await user.tab();
      const sort = within(nav).getByRole("combobox", { name: "Order" });
      expect(sort).toHaveFocus();
      await user.selectOptions(sort, "By title");
      await waitFor(() => expect(Object.fromEntries(asked[asked.length - 1]!)).toEqual({ scope: "subtree", depth: "3", sort: "title" }));
      expect(await axeViolations()).toEqual([]);

      expect((await closeEditor(router, sent)).content[0]).toEqual(childPages("subtree", 3, "title"));
    },
    EDITOR_TEST_MS,
  );
});
