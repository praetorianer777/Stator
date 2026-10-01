import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { normalizeLabel, type LabeledPage } from "@/api/labels";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const space = aSpace();
const home = aPage();
const pageId = "0195f000-0000-7000-8000-0000000000a1";

function stubPage({
  edit = true,
  labels = ["howto"],
  more = {},
}: {
  edit?: boolean;
  labels?: string[];
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const page = aPage({
    id: pageId,
    title: "Notes",
    home: false,
    parentId: home.id,
    labels,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    can: { edit, delete: edit, restrict: edit, comment: true, archive: false },
  });
  const where = aSpace({ can: { ...space.can, editPages: edit } });
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [where] } },
    "GET /spaces/DOCS": { status: 200, body: { space: where } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page, space: where } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
    "GET /labels": (request) => {
      const q = new URL(request.url).searchParams.get("q") ?? "";
      const known = [
        { name: "release", pages: 4 },
        { name: "release-notes", pages: 2 },
        { name: "howto", pages: 9 },
      ];
      return { status: 200, body: { labels: known.filter((each) => each.name.startsWith(q)) } };
    },
    ...more,
  });
}

const labeled = (title: string, over: Partial<LabeledPage> = {}): LabeledPage => ({
  id: `0195f000-0000-7000-8000-0000000000${title.length.toString(16).padStart(2, "0")}`,
  title,
  spaceKey: "DOCS",
  spaceName: "Handbook",
  path: ["Handbook"],
  labels: ["release"],
  unpublished: false,
  updatedByName: "Ada Lovelace",
  updatedAt: "2026-09-29T08:00:00Z",
  ...over,
});

describe("normalizeLabel", () => {
  it("reads a name the way the API stores it", () => {
    expect(normalizeLabel("  Release Notes ")).toEqual({ name: "release-notes" });
    expect(normalizeLabel("Übersicht")).toEqual({ name: "übersicht" });
    expect(normalizeLabel("v1.2_b-c")).toEqual({ name: "v1.2_b-c" });
    expect(normalizeLabel("a/b").problem).toBe("invalid");
    expect(normalizeLabel("-lead").problem).toBe("invalid");
    expect(normalizeLabel("").problem).toBe("invalid");
    expect(normalizeLabel("a".repeat(41)).problem).toBe("tooLong");
  });
});

describe("labels on a page", () => {
  it("shows each label as a way to its pages in the space", async () => {
    stubPage({ edit: false, labels: ["howto", "release"] });
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    const list = await screen.findByRole("list", { name: "Labels" });
    const link = within(list).getByRole("link", { name: "release" });
    expect(link).toHaveAttribute("href", "/s/DOCS/labels/release");
    expect(screen.queryByRole("combobox", { name: "Add a label" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove label release" })).not.toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("draws nothing for a reader when the page has no labels", async () => {
    stubPage({ edit: false, labels: [] });
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    await screen.findByRole("heading", { name: "Notes", level: 1 });
    expect(document.querySelector("[data-page-labels]")).toBeNull();
  });

  it("adds a label picked with the arrow keys, and one typed new with Enter", async () => {
    const added: string[] = [];
    const sent = stubPage({
      more: {
        [`POST /pages/${pageId}/labels`]: async (request) => {
          const { name } = (await request.clone().json()) as { name: string };
          added.push(name);
          return { status: 200, body: { labels: ["howto", ...added].sort() } };
        },
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    const input = await screen.findByRole("combobox", { name: "Add a label" });
    await userEvent.type(input, "rel");
    const options = await screen.findByRole("listbox", { name: "Labels to pick" });
    await waitFor(() =>
      expect(
        within(options)
          .getAllByRole("option")
          .map((o) => o.getAttribute("data-label-option")),
      ).toEqual(["rel", "release", "release-notes"]),
    );
    expect(within(options).getByRole("option", { name: /Add rel/ })).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    const active = within(options).getByRole("option", { name: /release-notes/ });
    expect(input).toHaveAttribute("aria-activedescendant", active.id);
    expect(await axeViolations()).toEqual([]);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(added).toEqual(["release-notes"]));
    expect(input).toHaveValue("");
    expect(await screen.findByRole("link", { name: "release-notes" })).toBeInTheDocument();

    await userEvent.type(input, "Team Wiki{Enter}");
    await waitFor(() => expect(added).toEqual(["release-notes", "team-wiki"]));
    expect(sent.filter((each) => each.method === "POST" && each.path.endsWith("/labels")).map((each) => each.body)).toEqual([
      { name: "release-notes" },
      { name: "team-wiki" },
    ]);
    expect(await screen.findByText("Added the label team-wiki.")).toBeInTheDocument();
  });

  it("says why a typed name cannot be a label, and does not send it", async () => {
    const sent = stubPage();
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    const input = await screen.findByRole("combobox", { name: "Add a label" });
    await userEvent.type(input, "a/b{Enter}");
    expect(await screen.findByText(/Use letters, digits, hyphens/)).toBeInTheDocument();
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(sent.some((each) => each.method === "POST" && each.path.endsWith("/labels"))).toBe(false);
  });

  it("takes a label off with its button and with Backspace in the empty box, and shows a refusal", async () => {
    const removed: string[] = [];
    stubPage({
      labels: ["howto", "release"],
      more: {
        [`DELETE /pages/${pageId}/labels/release`]: () => {
          removed.push("release");
          return { status: 204 };
        },
        [`POST /pages/${pageId}/labels`]: {
          status: 403,
          body: { error: { code: "forbidden", message: "You may read this page but not change it. Ask an administrator of the space for access." } },
        },
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/notes`);
    await userEvent.click(await screen.findByRole("button", { name: "Remove label release" }));
    await waitFor(() => expect(removed).toEqual(["release"]));
    await waitFor(() => expect(screen.queryByRole("link", { name: "release" })).not.toBeInTheDocument());

    await userEvent.type(screen.getByRole("combobox", { name: "Add a label" }), "new{Enter}");
    expect(await screen.findByText(/You may read this page but not change it/)).toBeInTheDocument();
  });
});

describe("the label page", () => {
  it("lists the pages with a label in every space, a page of the list at a time", async () => {
    const offsets: string[] = [];
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /labels/release/pages": (request) => {
        const params = new URL(request.url).searchParams;
        offsets.push(params.get("offset") ?? "0");
        expect(params.get("space")).toBeNull();
        return {
          status: 200,
          body: {
            pages: [labeled("Launch plan", { path: ["Handbook", "Projects"], labels: ["plan", "release"] })],
            total: 45,
            limit: 20,
            offset: Number(params.get("offset") ?? 0),
          },
        };
      },
    });
    const router = await renderAt("/labels/release");
    expect(await screen.findByText("1 to 1 of 45 pages")).toBeInTheDocument();
    const row = document.querySelector('[data-labeled-page="Launch plan"]') as HTMLElement;
    expect(within(row).getByRole("link", { name: "Launch plan" })).toBeInTheDocument();
    expect(row).toHaveTextContent("Handbook / Projects");
    expect(within(row).getByRole("link", { name: "plan" })).toHaveAttribute("href", "/labels/plan");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ page: 2 }));
    await waitFor(() => expect(offsets.at(-1)).toBe("20"));
  });

  it("narrows to one space, offers every space, and says when no page carries the label", async () => {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /labels/lonely/pages": (request) => {
        expect(new URL(request.url).searchParams.get("space")).toBe("DOCS");
        return { status: 200, body: { pages: [], total: 0, limit: 20, offset: 0 } };
      },
    });
    await renderAt("/s/DOCS/labels/lonely");
    expect(await screen.findByText("No page carries this label")).toBeInTheDocument();
    expect(screen.getByText("Pages in Handbook with this label")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Show every space" })).toHaveAttribute("href", "/labels/lonely");
    expect(await axeViolations()).toEqual([]);
  });
});

describe("the search label filter", () => {
  it("adds a picked label to the address and takes it off again", async () => {
    stubApi({
      "GET /search": { status: 200, body: { hits: [], total: 0, limit: 20, offset: 0 } },
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /people": { status: 200, body: { people: [] } },
      "GET /labels": { status: 200, body: { labels: [{ name: "release", pages: 3 }] } },
    });
    const router = await renderAt("/search?q=plan");
    const input = await screen.findByRole("combobox", { name: "Labels" });
    await userEvent.click(input);
    const option = await screen.findByRole("option", { name: /release/ });
    await userEvent.click(option);
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "plan", label: "release" }));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Remove label release" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "plan" }));
  });
});
