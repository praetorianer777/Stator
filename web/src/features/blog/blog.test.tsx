import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { arrival, renderAt, stubApi } from "@/test/app";
import { allowlist } from "@/test/allowlist";
import { aPage, aSpace } from "@/test/spaces";
import { BLOG_POSTS_DEFAULT_LIMIT, PAGE_LIST_MAX_LIMIT } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { PublicReadingContext } from "@/features/editor/publicReading";
import { BlogPostsDialog } from "./BlogPostsDialog";
import { groupByYear } from "./BlogScreen";
import { LatestPosts, shortExcerpt } from "./LatestPosts";
import { blogPostsSettings } from "./blogPosts";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const space = aSpace();
const post = (id: string, title: string, postedAt: string, over: Record<string, unknown> = {}) => ({
  id: `0195f000-0000-7000-8000-0000000000${id}`,
  title,
  spaceKey: "DOCS",
  spaceName: "Handbook",
  postedAt,
  authorName: "Ada Lovelace",
  excerpt: `${title} says hello.`,
  icon: null,
  ...over,
});
const launch = post("a1", "Launch day", "2026-10-02T09:00:00Z");
const retro = post("a2", "Retrospective", "2026-09-20T09:00:00Z");
const blog = {
  spaceKey: "DOCS",
  spaceName: "Handbook",
  months: [
    { year: 2026, month: 10, count: 1 },
    { year: 2026, month: 9, count: 1 },
    { year: 2025, month: 12, count: 3 },
  ],
  unpublished: [{ id: "0195f000-0000-7000-8000-0000000000b1", title: "Plans for 2027", updatedAt: "2026-10-05T08:00:00Z", publishAt: null }],
  watching: false,
  canPost: true,
};

function shell(more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    "GET /spaces/DOCS/blog": { status: 200, body: { blog } },
    "GET /posts": (request) => {
      const month = new URL(request.url).searchParams.get("month");
      return { status: 200, body: { posts: month === "9" ? [retro] : [launch, retro], next: null } };
    },
    ...more,
  });
}

describe("a space's blog", () => {
  it("lists the posts newest first, offers the months with their counts, and keeps one month when chosen", async () => {
    const sent = shell();
    const router = await renderAt("/s/DOCS/blog");
    await waitFor(() => expect(document.querySelectorAll("[data-post]")).toHaveLength(2));
    const titles = [...document.querySelectorAll("[data-post-title]")].map((el) => el.getAttribute("data-post-title"));
    expect(titles).toEqual(["Launch day", "Retrospective"]);
    expect(screen.getByRole("link", { name: "Launch day" })).toHaveAttribute("href", `/s/DOCS/p/${launch.id}/launch-day`);

    const dates = screen.getByRole("navigation", { name: "Posts by date" });
    expect(within(dates).getByRole("link", { name: "2026 (2)" })).toBeInTheDocument();
    expect(within(dates).getByRole("link", { name: "2025 (3)" })).toBeInTheDocument();
    expect(document.querySelector('[data-blog-month="2026-10"]')).toHaveAttribute("data-count", "1");

    await userEvent.click(within(dates).getByRole("link", { name: "September (1)" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ year: 2026, month: 9 }));
    await arrival(router, "/s/DOCS/blog");
    await waitFor(() => expect(document.querySelectorAll("[data-post]")).toHaveLength(1));
    expect(sent.some((r) => r.path === "/posts" && r.method === "GET")).toBe(true);
    expect(within(dates).getByRole("link", { name: "September (1)" })).toHaveAttribute("aria-current", "page");
  });

  it("shows the reader's own unpublished posts, and watches the blog", async () => {
    const sent = shell({ "PUT /spaces/DOCS/blog/watch": { status: 204 } });
    await renderAt("/s/DOCS/blog");
    const mine = await screen.findByRole("region", { name: "Your unpublished posts" });
    expect(within(mine).getByRole("link", { name: "Plans for 2027" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Watch blog" }));
    await waitFor(() => expect(sent.some((r) => r.method === "PUT" && r.path === "/spaces/DOCS/blog/watch")).toBe(true));
    expect(await screen.findByRole("button", { name: "Watching blog" })).toHaveAttribute("aria-pressed", "true");
  });

  it("starts a post from a template, asking for its variables, which the server fills in", async () => {
    const weekly = {
      key: "0195f000-0000-7000-8000-0000000000c1",
      name: "Weekly update",
      description: "",
      title: "Week of {week}",
      body: { type: "doc", content: [{ type: "paragraph" }] },
      builtIn: false,
      scope: "space",
      spaceKey: "DOCS",
      canEdit: false,
      variables: [{ name: "week", label: "Week", kind: "date", options: [], default: "", required: true }],
    };
    const made = { id: "0195f000-0000-7000-8000-0000000000c2", title: "Week of 2026-10-05", spaceKey: "DOCS" };
    const sent = shell({
      "GET /templates": { status: 200, body: { templates: [weekly] } },
      "POST /spaces/DOCS/posts": { status: 201, body: { page: made } },
    });
    const router = await renderAt("/s/DOCS/blog");
    await userEvent.click(await screen.findByRole("button", { name: "New post" }));
    const dialog = await screen.findByRole("dialog", { name: "Write a blog post" });
    await userEvent.click(await within(dialog).findByRole("radio", { name: /Weekly update/ }));
    expect(within(dialog).getByLabelText("Title")).toHaveValue("Week of {week}");
    await userEvent.type(within(dialog).getByLabelText("Week (required)"), "2026-10-05");
    await userEvent.click(within(dialog).getByRole("button", { name: "Start writing" }));
    await arrival(router, `/s/DOCS/p/${made.id}/week-of-2026-10-05/edit`);
    expect(sent.find((r) => r.method === "GET" && r.path === "/templates")).toBeDefined();
    expect(sent.find((r) => r.method === "POST" && r.path === "/spaces/DOCS/posts")?.body).toEqual({
      title: "Week of {week}",
      template: weekly.key,
      values: { week: "2026-10-05" },
    });
  });

  it("says when a month holds nothing the reader may read", async () => {
    shell({ "GET /posts": { status: 200, body: { posts: [], next: null } } });
    await renderAt("/s/DOCS/blog?year=2024&month=2");
    expect(await screen.findByText("No post you can read went out in this time.")).toBeInTheDocument();
  });

  it("groups months under their years, newest first", () => {
    expect(groupByYear(blog.months).map((y) => [y.year, y.count, y.months.map((m) => m.month)])).toEqual([
      [2026, 2, [10, 9]],
      [2025, 3, [12]],
    ]);
  });
});

describe("a post's own page", () => {
  it("sits in its blog and offers nothing that places it in the tree", async () => {
    const id = "0195f000-0000-7000-8000-0000000000c1";
    const page = aPage({ id, title: "Launch day", kind: "post", home: false, postedAt: "2026-10-02T09:00:00Z" });
    shell({
      [`GET /pages/${id}`]: { status: 200, body: { page, space } },
      [`GET /pages/${id}/attachments`]: { status: 200, body: { attachments: [] } },
      [`POST /pages/${id}/visit`]: { status: 204 },
    });
    await renderAt(`/s/DOCS/p/${id}/launch-day`);
    await waitFor(() => expect(document.querySelector("[data-blog-crumb]")).toHaveAttribute("href", "/s/DOCS/blog"));
    expect(document.querySelector("[data-post-date]")).toHaveTextContent("Posted on Oct 2, 2026");
    expect(document.querySelector('[data-action="new-page"]')).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Page actions" }));
    for (const action of ["move-page", "copy-page", "new-folder", "import-markdown", "page-mode"]) {
      expect(document.querySelector(`[data-action="${action}"]`), action).toBeNull();
    }
    expect(document.querySelector('[data-action="trash-page"]')).not.toBeNull();
  });
});

function shown(children: ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Overview</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

describe("the latest blog posts block", () => {
  it("puts right what the server would refuse, as the allowlist says", () => {
    expect(blogPostsSettings({ space: "docs", limit: 0 })).toEqual({ space: null, limit: BLOG_POSTS_DEFAULT_LIMIT });
    expect(blogPostsSettings({ space: "NEWS", limit: PAGE_LIST_MAX_LIMIT })).toEqual({ space: "NEWS", limit: PAGE_LIST_MAX_LIMIT });
    expect(allowlist.nodes.blogPosts?.attrs?.limit?.max).toBe(PAGE_LIST_MAX_LIMIT);
    expect(Object.keys(allowlist.nodes.blogPosts?.attrs ?? {}).sort()).toEqual(["limit", "space"]);
    expect(shortExcerpt("one two three four", 9)).toBe("one two...");
  });

  it("lists the newest posts the reader may read, naming each space when it lists every one", async () => {
    const asked: URLSearchParams[] = [];
    stubApi({
      "GET /posts": (request) => {
        asked.push(new URL(request.url).searchParams);
        return {
          status: 200,
          body: { posts: [launch, post("a3", "Elsewhere", "2026-10-01T09:00:00Z", { spaceKey: "OPS", spaceName: "Operations" })], next: null },
        };
      },
    });
    shown(<LatestPosts settings={{ space: null, limit: 5 }} inEditor />);
    const block = await screen.findByRole("region", { name: "Latest blog posts" });
    await waitFor(() => expect(block).toHaveAttribute("data-state", "list"));
    expect(block.querySelector('[data-listed-post="Elsewhere"]')).toHaveTextContent("Operations");
    expect(asked[0]?.get("limit")).toBe("5");
    expect(asked[0]?.has("space")).toBe(false);
  });

  it("says in words what it lists where nobody is signed in, and asks for nothing", () => {
    const sent = stubApi({});
    render(
      <PublicReadingContext value="demo">
        <DocView doc={{ type: "doc", content: [{ type: "blogPosts", attrs: { space: "NEWS", limit: 5 } }] }} />
      </PublicReadingContext>,
    );
    expect(screen.getByText("The latest blog posts in NEWS")).toBeInTheDocument();
    expect(sent).toHaveLength(0);
  });

  it("stores this space by its key, another space as chosen, or every space", async () => {
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [space, aSpace({ id: "s2", key: "OPS", name: "Operations" })] } } });
    const saved = vi.fn();
    shown(<BlogPostsDialog initial={{ space: "DOCS", limit: 5 }} currentSpace="DOCS" isNew onSave={saved} onClose={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "Insert" }));
    expect(saved).toHaveBeenLastCalledWith({ space: "DOCS", limit: 5 });

    await userEvent.click(screen.getByRole("radio", { name: "Another space" }));
    await userEvent.click(screen.getByRole("button", { name: "Insert" }));
    expect(await screen.findByText("Choose the space whose posts to list.", { selector: "[id$=-error]" })).toBeInTheDocument();
    await screen.findByRole("option", { name: "Operations (OPS)" });
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Space" }), "OPS");
    await userEvent.click(screen.getByRole("button", { name: "Insert" }));
    expect(saved).toHaveBeenLastCalledWith({ space: "OPS", limit: 5 });

    await userEvent.click(screen.getByRole("radio", { name: "Every space" }));
    await userEvent.click(screen.getByRole("button", { name: "Insert" }));
    expect(saved).toHaveBeenLastCalledWith({ space: null, limit: 5 });
  });
});
