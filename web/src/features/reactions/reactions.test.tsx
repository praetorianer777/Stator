import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Comment, Thread } from "@/api/comments";
import type { Reaction } from "@/api/reactions";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import type { Doc } from "@/features/editor/schema";
import { REACTION_CHOICES } from "@/config";
import { whoReacted } from "./Reactions";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const home = aPage();
const pageId = "0195f000-0000-7000-8000-0000000000d1";
const commentId = "0195f000-0000-7000-8000-00000000d0a1";
const me = signedIn.user;
const grace = { id: "u-grace", name: "Grace Hopper" };
const alan = { id: "u-alan", name: "Alan Turing" };

function aReaction(over: Partial<Reaction> = {}): Reaction {
  return { emoji: "👍", count: 1, mine: false, people: [grace], ...over };
}

const say = (text: string): Doc => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

function aThread(reactions: Reaction[]): Thread {
  const comment: Comment = {
    id: commentId,
    threadId: commentId,
    authorId: grace.id,
    authorName: grace.name,
    body: say("Ship it Friday?"),
    deleted: false,
    createdAt: "2026-10-01T08:00:00Z",
    editedAt: null,
    reactions,
    can: { edit: false, delete: false },
  };
  return {
    id: commentId,
    pageId,
    kind: "page",
    anchor: null,
    resolved: false,
    resolvedByName: "",
    resolvedAt: null,
    comments: [comment],
    can: { reply: true, resolve: false },
  };
}

function stubPage({
  reactions = [],
  comment = true,
  threads = [],
  more = {},
}: {
  reactions?: Reaction[];
  comment?: boolean;
  threads?: Thread[];
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const page = aPage({
    id: pageId,
    title: "Plan",
    home: false,
    parentId: home.id,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    can: { edit: comment, delete: comment, restrict: comment, comment, archive: false, add: comment, grantEdit: false },
    comments: { page: threads.length, inline: 0, detached: 0 },
    reactions,
  });
  const space = aSpace();
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page, space } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
    [`GET /pages/${pageId}/comments`]: { status: 200, body: { threads } },
    ...more,
  });
}

async function pageBar() {
  return screen.findByRole("list", { name: "Reactions to this page" });
}

describe("who reacted", () => {
  it("names the caller first, as you, then the others, and counts the rest", () => {
    expect(whoReacted(aReaction(), me.id)).toBe("Grace Hopper reacted with 👍");
    expect(whoReacted(aReaction({ count: 1, mine: true, people: [{ id: me.id, name: me.name }] }), me.id)).toBe("You reacted with 👍");
    expect(whoReacted(aReaction({ count: 3, mine: true, people: [grace, { id: me.id, name: me.name }, alan] }), me.id)).toBe(
      "You, Grace Hopper and Alan Turing reacted with 👍",
    );
    expect(whoReacted(aReaction({ count: 12, people: [grace, alan] }), me.id)).toBe("Grace Hopper, Alan Turing and 10 others reacted with 👍");
    expect(whoReacted(aReaction({ count: 3, people: [grace, alan] }), me.id)).toBe("Grace Hopper, Alan Turing and 1 other reacted with 👍");
    // Past the names the API sends, the caller is still counted as you, once.
    expect(whoReacted(aReaction({ count: 12, mine: true, people: [grace, alan] }), me.id)).toBe("You, Grace Hopper, Alan Turing and 9 others reacted with 👍");
    expect(whoReacted(aReaction({ people: [{ id: "u-gone", name: "" }] }), me.id)).toBe("a former member reacted with 👍");
  });
});

describe("reactions on a page", () => {
  it("shows each emoji with its count and who reacted, and takes the caller's own back", async () => {
    let asked = "";
    const sent = stubPage({
      reactions: [aReaction({ count: 2, mine: true, people: [grace, { id: me.id, name: me.name }] }), aReaction({ emoji: "🎉", people: [alan] })],
      more: {
        [`DELETE /pages/${pageId}/reactions`]: (request) => {
          asked = new URL(request.url).searchParams.get("emoji") ?? "";
          return { status: 200, body: { reactions: [aReaction(), aReaction({ emoji: "🎉", people: [alan] })] } };
        },
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const bar = await pageBar();
    const thumbs = within(bar).getByRole("button", { name: "👍 2 reactions" });
    expect(thumbs).toHaveAttribute("aria-pressed", "true");
    expect(thumbs).toHaveAccessibleDescription("You and Grace Hopper reacted with 👍");
    const party = within(bar).getByRole("button", { name: "🎉 1 reaction" });
    expect(party).toHaveAttribute("aria-pressed", "false");
    expect(party).toHaveAccessibleDescription("Alan Turing reacted with 🎉");
    expect(await axeViolations()).toEqual([]);

    await userEvent.hover(party);
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Alan Turing reacted with 🎉");

    await userEvent.click(thumbs);
    await waitFor(() => expect(asked).toBe("👍"));
    expect(sent.some((each) => each.method === "DELETE" && each.path === `/pages/${pageId}/reactions`)).toBe(true);
    await waitFor(() => expect(within(bar).getByRole("button", { name: "👍 1 reaction" })).toHaveAttribute("aria-pressed", "false"));
  });

  it("adds an emoji from the picker by keyboard, and offers the ones already given as taken", async () => {
    const sent = stubPage({
      reactions: [aReaction({ emoji: "🎉", mine: true, people: [{ id: me.id, name: me.name }] })],
      more: {
        [`POST /pages/${pageId}/reactions`]: (request) =>
          request
            .clone()
            .json()
            .then((body: { emoji: string }) => ({
              status: 200,
              body: {
                reactions: [aReaction({ emoji: "🎉", mine: true }), aReaction({ emoji: body.emoji, mine: true, people: [{ id: me.id, name: me.name }] })],
              },
            })),
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const bar = await pageBar();
    const add = within(bar).getByRole("button", { name: "Add a reaction" });
    add.focus();
    await userEvent.keyboard("{Enter}");
    const menu = await screen.findByRole("menu", { name: "Pick a reaction" });
    const items = within(menu).getAllByRole("menuitem");
    expect(items).toHaveLength(REACTION_CHOICES.length + 1);
    expect(within(menu).getByRole("menuitem", { name: "🎉 Celebrate" })).toHaveAttribute("aria-disabled", "true");
    expect(await axeViolations()).toEqual([]);
    await waitFor(() => expect(items[0]).toHaveFocus());
    await userEvent.keyboard("{ArrowDown}{Enter}");
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === `/pages/${pageId}/reactions`)).toBe(true));
    const posted = sent.find((each) => each.method === "POST" && each.path === `/pages/${pageId}/reactions`)!.body as { emoji: string };
    expect(posted.emoji).toBe(REACTION_CHOICES[1]);
    await waitFor(() => expect(within(bar).getByRole("button", { name: `${REACTION_CHOICES[1]} 1 reaction` })).toHaveAttribute("aria-pressed", "true"));
    expect(add).toHaveFocus();
  });

  it("finds any other emoji by name, and Enter takes the first one found", async () => {
    const sent = stubPage({
      more: {
        [`POST /pages/${pageId}/reactions`]: {
          status: 200,
          body: { reactions: [aReaction({ emoji: "🌮", mine: true, people: [{ id: me.id, name: me.name }] })] },
        },
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    await userEvent.click(within(await pageBar()).getByRole("button", { name: "Add a reaction" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "More emoji" }));
    const dialog = await screen.findByRole("dialog", { name: "React with any emoji" });
    const box = within(dialog).getByRole("searchbox", { name: "Find an emoji by name" });
    await waitFor(() => expect(box).toHaveFocus());
    await userEvent.type(box, "taco");
    expect(await within(dialog).findByRole("button", { name: "taco" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === `/pages/${pageId}/reactions`)).toBe(true));
    expect((sent.find((each) => each.method === "POST" && each.path === `/pages/${pageId}/reactions`)!.body as { emoji: string }).emoji).toBe("🌮");
    await userEvent.click(within(await pageBar()).getByRole("button", { name: "Add a reaction" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "More emoji" }));
    await userEvent.type(await screen.findByRole("searchbox"), "zzzz");
    expect(await screen.findByText("No emoji by that name. Try another word.")).toHaveAttribute("role", "status");
  });

  it("shows a refusal as the sentence the server wrote", async () => {
    stubPage({
      reactions: [aReaction()],
      more: {
        [`POST /pages/${pageId}/reactions`]: {
          status: 403,
          body: { error: { code: "forbidden", message: "You may not react in this space. Ask an administrator of the space for access." } },
        },
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    await userEvent.click(within(await pageBar()).getByRole("button", { name: "👍 1 reaction" }));
    expect(await screen.findByText("You may not react in this space. Ask an administrator of the space for access.")).toBeInTheDocument();
  });

  it("lets a reader who may not comment see who reacted, and nothing more", async () => {
    const sent = stubPage({ comment: false, reactions: [aReaction()] });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const bar = await pageBar();
    expect(within(bar).queryByRole("button", { name: "Add a reaction" })).toBeNull();
    const thumbs = within(bar).getByRole("button", { name: "👍 1 reaction" });
    expect(thumbs).toHaveAttribute("aria-disabled", "true");
    expect(thumbs).toHaveAccessibleDescription("Grace Hopper reacted with 👍");
    await userEvent.click(thumbs);
    expect(sent.some((each) => each.path.endsWith("/reactions"))).toBe(false);
    expect(await axeViolations()).toEqual([]);
  });

  it("shows nothing to a reader who may not comment on a page without reactions", async () => {
    stubPage({ comment: false });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    await screen.findByRole("heading", { name: "Plan", level: 1 });
    expect(screen.queryByRole("list", { name: "Reactions to this page" })).toBeNull();
  });
});

describe("reactions on a comment", () => {
  it("are put on and named like a page's, below the comment", async () => {
    const sent = stubPage({
      threads: [aThread([aReaction({ emoji: "✅", people: [alan] })])],
      more: {
        [`POST /comments/${commentId}/reactions`]: {
          status: 200,
          body: { reactions: [aReaction({ emoji: "✅", count: 2, mine: true, people: [alan, { id: me.id, name: me.name }] })] },
        },
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const bar = await screen.findByRole("list", { name: "Reactions to Grace Hopper's comment" });
    const done = within(bar).getByRole("button", { name: "✅ 1 reaction" });
    expect(done).toHaveAccessibleDescription("Alan Turing reacted with ✅");
    await userEvent.click(done);
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === `/comments/${commentId}/reactions`)).toBe(true));
    expect((sent.find((each) => each.method === "POST" && each.path === `/comments/${commentId}/reactions`)!.body as { emoji: string }).emoji).toBe("✅");
    await waitFor(() => expect(within(bar).getByRole("button", { name: "✅ 2 reactions" })).toHaveAccessibleDescription("You and Alan Turing reacted with ✅"));
    expect(await axeViolations()).toEqual([]);
  });

  it("are not offered on a deleted comment", async () => {
    const live = aThread([]);
    const opening = live.comments[0]!;
    const reply = { ...opening, id: "0195f000-0000-7000-8000-00000000d0a2" };
    stubPage({ threads: [{ ...live, comments: [{ ...opening, deleted: true, body: null }, reply] }] });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    await screen.findByText("This comment was deleted.");
    expect(screen.getAllByRole("list", { name: "Reactions to Grace Hopper's comment" })).toHaveLength(1);
  });
});
