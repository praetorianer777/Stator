import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor } from "@tiptap/core";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { Comment, Thread } from "@/api/comments";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { editorExtensions, fitSchema } from "@/features/editor/extensions";
import type { Doc } from "@/features/editor/schema";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const home = aPage();
const pageId = "0195f000-0000-7000-8000-0000000000c1";
const threadId = "0195f000-0000-7000-8000-00000000c0a1";
const replyId = "0195f000-0000-7000-8000-00000000c0a2";
const otherThreadId = "0195f000-0000-7000-8000-00000000c0b1";

const say = (text: string): Doc => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

function aComment(over: Partial<Comment> = {}): Comment {
  return {
    id: threadId,
    threadId,
    authorId: "u-grace",
    authorName: "Grace Hopper",
    body: say("Should we ship Friday?"),
    deleted: false,
    createdAt: "2026-09-29T08:00:00Z",
    editedAt: null,
    can: { edit: false, delete: false },
    ...over,
  };
}

function aThread(over: Partial<Thread> = {}): Thread {
  return {
    id: threadId,
    pageId,
    kind: "page",
    anchor: null,
    resolved: false,
    resolvedByName: "",
    resolvedAt: null,
    comments: [aComment()],
    can: { reply: true, resolve: false },
    ...over,
  };
}

const discussion = (): Thread[] => [
  aThread({
    comments: [
      aComment({ deleted: true, body: null }),
      aComment({
        id: replyId,
        authorId: signedIn.user.id,
        authorName: "Ada Lovelace",
        body: say("Monday is safer."),
        editedAt: "2026-09-29T09:00:00Z",
        can: { edit: true, delete: true },
      }),
    ],
  }),
  aThread({ id: otherThreadId, comments: [aComment({ id: otherThreadId, threadId: otherThreadId, body: say("Who writes the notes?") })] }),
];

function stubPage({
  comment = true,
  unpublished = false,
  threads = discussion(),
  count = 2,
  more = {},
}: {
  comment?: boolean;
  unpublished?: boolean;
  threads?: Thread[];
  count?: number;
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const page = aPage({
    id: pageId,
    title: "Plan",
    home: false,
    parentId: home.id,
    version: unpublished ? 0 : 1,
    unpublished,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    can: { edit: comment, delete: comment, restrict: comment, comment },
    comments: { page: count, inline: 0, detached: 0 },
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

/** Types into the comment editor once it has arrived. */
async function write(id: string, text: string) {
  const box = await waitFor(() => {
    const found = document.getElementById(id);
    if (!found) throw new Error(`no editor ${id} yet`);
    return found;
  });
  await userEvent.click(box);
  await userEvent.type(box, text);
  return box;
}

describe("the comments below a page", () => {
  it("lists threads with their replies, a placeholder for what was deleted, and offers what each comment allows", async () => {
    stubPage();
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const section = await screen.findByRole("region", { name: /Comments/ });
    const threads = await within(section).findAllByRole("article");
    expect(threads).toHaveLength(2);
    const first = threads[0]!;
    expect(first).toHaveAccessibleName("Thread started by Grace Hopper");
    expect(within(first).getByText("This comment was deleted.")).toBeInTheDocument();
    expect(within(first).getByText("Monday is safer.")).toBeInTheDocument();
    expect(within(first).getByText(/edited/)).toBeInTheDocument();
    const deleted = first.querySelector(`[data-comment="${threadId}"]`)!;
    expect(within(deleted as HTMLElement).queryByRole("button", { name: "Edit" })).toBeNull();
    const mine = first.querySelector(`[data-comment="${replyId}"]`) as HTMLElement;
    expect(within(mine).getByRole("button", { name: "Edit" })).toBeInTheDocument();
    expect(within(mine).getByRole("button", { name: "Delete" })).toBeInTheDocument();
    expect(within(threads[1]!).queryByRole("button", { name: "Edit" })).toBeNull();
    expect(within(threads[1]!).getByRole("button", { name: "Reply" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "2 comments, go to the comments" })).toHaveAttribute("href", "#comments");
    expect(await axeViolations()).toEqual([]);
  });

  it("starts a thread with the comment editor, which holds a comment to its allowlist", async () => {
    const sent = stubPage({ threads: [], count: 0, more: { [`POST /pages/${pageId}/comments`]: { status: 201, body: { thread: aThread() } } } });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    expect(await screen.findByText("Nobody has commented yet.")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /comments, go to/ })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Add a comment" }));
    const box = await write("new-comment", "Looks good");
    expect(box).toHaveAccessibleName("Your comment");
    const toolbar = screen.getByRole("toolbar", { name: "Formatting" });
    for (const gone of ["Table", "Panel", "Checklist", "Divider", "Attach files"]) {
      expect(within(toolbar).queryByRole("button", { name: gone })).toBeNull();
    }
    expect(within(toolbar).getByRole("button", { name: "Bulleted list" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Comment" }));
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === `/pages/${pageId}/comments`)).toBe(true));
    const posted = sent.find((each) => each.method === "POST" && each.path === `/pages/${pageId}/comments`)!.body as { body: Doc };
    expect(JSON.stringify(posted.body)).toContain("Looks good");
    await waitFor(() => expect(document.getElementById("new-comment")).toBeNull());
  });

  it("replies at the end of a thread, and edits and deletes one's own", async () => {
    const sent = stubPage({
      more: {
        [`POST /comments/${threadId}/replies`]: { status: 201, body: { comment: aComment(), thread: aThread() } },
        [`PATCH /comments/${replyId}`]: { status: 200, body: { comment: aComment({ id: replyId }) } },
        [`DELETE /comments/${replyId}`]: { status: 204 },
      },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const first = (await within(await screen.findByRole("region", { name: /Comments/ })).findAllByRole("article"))[0]!;
    await userEvent.click(within(first).getByRole("button", { name: "Reply" }));
    const box = await write(`reply-${threadId}`, "Agreed");
    expect(box).toHaveAccessibleName("Your reply to Grace Hopper");
    await userEvent.click(within(first).getByRole("button", { name: "Post reply" }));
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === `/comments/${threadId}/replies`)).toBe(true));

    const mine = first.querySelector(`[data-comment="${replyId}"]`) as HTMLElement;
    await userEvent.click(within(mine).getByRole("button", { name: "Edit" }));
    const editing = await write(`edit-${replyId}`, " instead");
    expect(editing.textContent).toContain("Monday is safer.");
    await userEvent.click(within(mine).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((each) => each.method === "PATCH" && each.path === `/comments/${replyId}`)).toBe(true));
    const patched = sent.find((each) => each.method === "PATCH")!.body as { body: Doc };
    expect(JSON.stringify(patched.body)).toContain("instead");

    await waitFor(() => expect(within(mine).getByRole("button", { name: "Delete" })).toBeInTheDocument());
    await userEvent.click(within(mine).getByRole("button", { name: "Delete" }));
    expect(window.confirm).toHaveBeenCalledWith("Delete this comment? Its words go for good; replies to it stay.");
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/comments/${replyId}`)).toBe(true));
  });

  it("offers Delete on somebody else's comment only where the comment allows it, and names whose it is", async () => {
    const sent = stubPage({
      threads: [
        aThread({ comments: [aComment({ can: { edit: false, delete: false } })] }),
        aThread({
          id: otherThreadId,
          comments: [aComment({ id: otherThreadId, threadId: otherThreadId, body: say("Who writes the notes?"), can: { edit: false, delete: true } })],
        }),
      ],
      more: { [`DELETE /comments/${otherThreadId}`]: { status: 204 } },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const [member, moderated] = await within(await screen.findByRole("region", { name: /Comments/ })).findAllByRole("article");
    expect(within(member!).queryByRole("button", { name: "Delete" })).toBeNull();
    expect(within(moderated!).queryByRole("button", { name: "Edit" })).toBeNull();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(moderated!).getByRole("button", { name: "Delete" }));
    expect(window.confirm).toHaveBeenCalledWith("Delete Grace Hopper's comment? Its words go for good, and the deletion is recorded.");
    await waitFor(() => expect(sent.some((each) => each.method === "DELETE" && each.path === `/comments/${otherThreadId}`)).toBe(true));
  });

  it("leaves a thread as it was when Escape cancels, and shows a refusal in its sentence", async () => {
    stubPage({
      more: {
        [`POST /comments/${threadId}/replies`]: {
          status: 403,
          body: { error: { code: "forbidden", message: "You may not comment in this space. Ask an administrator of the space for access." } },
        },
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const first = (await within(await screen.findByRole("region", { name: /Comments/ })).findAllByRole("article"))[0]!;
    await userEvent.click(within(first).getByRole("button", { name: "Reply" }));
    await write(`reply-${threadId}`, "Me too");
    await userEvent.click(within(first).getByRole("button", { name: "Post reply" }));
    expect(await within(first).findByRole("alert")).toHaveTextContent("You may not comment in this space");
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(document.getElementById(`reply-${threadId}`)).toBeNull());
    await waitFor(() => expect(within(first).getByRole("button", { name: "Reply" })).toHaveFocus());
  });

  it("offers nothing to write to somebody without the right, and says why", async () => {
    stubPage({ comment: false, threads: [aThread({ can: { reply: false, resolve: false } })] });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    expect(await screen.findByText(/You may read the comments but not add to them/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add a comment" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Reply" })).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("asks for a publish on a page nobody else can read yet", async () => {
    const sent = stubPage({ unpublished: true, threads: [], count: 0 });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    expect(await screen.findByText("Publish the page to let people comment on it.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add a comment" })).toBeNull();
    expect(sent.some((each) => each.path.endsWith("/comments"))).toBe(false);
  });

  it("brings the thread a link names into view and focuses it", async () => {
    const scrolled = vi.fn();
    Element.prototype.scrollIntoView = scrolled;
    stubPage();
    await renderAt(`/s/DOCS/p/${pageId}/plan?thread=${otherThreadId}`);
    const target = await waitFor(() => {
      const found = document.querySelector(`[data-thread="${otherThreadId}"]`);
      expect(found).toHaveFocus();
      return found!;
    });
    expect(target).toHaveAttribute("data-highlighted", "true");
    expect(scrolled).toHaveBeenCalled();
  });
});

describe("the comment editor against the server's comment allowlist", () => {
  const allowed = JSON.parse(readFileSync(resolve(process.cwd(), "../api/comment-allowlist.json"), "utf8")) as {
    nodes: Record<string, { attrs?: Record<string, unknown> }>;
    marks: Record<string, { attrs?: Record<string, unknown> }>;
  };

  it("has exactly the node and mark types a comment may hold, and no attribute the server does not know", () => {
    const e = new Editor({ element: document.createElement("div"), extensions: editorExtensions({ variant: "comment" }) });
    expect(Object.keys(e.schema.nodes).sort()).toEqual(Object.keys(allowed.nodes).sort());
    expect(Object.keys(e.schema.marks).sort()).toEqual(Object.keys(allowed.marks).sort());
    for (const [name, type] of Object.entries(e.schema.nodes)) {
      for (const attr of Object.keys(type.spec.attrs ?? {})) expect(allowed.nodes[name]?.attrs, `${name}.${attr}`).toHaveProperty([attr]);
    }
    e.destroy();
  });

  it("keeps the words of pasted blocks a comment cannot hold, and nothing else of them", () => {
    const e = new Editor({ element: document.createElement("div"), extensions: editorExtensions({ variant: "comment" }) });
    const fitted = fitSchema(
      {
        type: "doc",
        content: [
          {
            type: "table",
            content: [{ type: "tableRow", content: [{ type: "tableCell", content: [{ type: "paragraph", content: [{ type: "text", text: "cell" }] }] }] }],
          },
          { type: "horizontalRule" },
          { type: "paragraph", content: [{ type: "text", text: "kept", marks: [{ type: "bold" }, { type: "hint" }] }] },
        ],
      },
      e.schema,
    );
    expect(fitted.content).toEqual([
      { type: "paragraph", content: [{ type: "text", text: "cell" }] },
      { type: "paragraph", content: [{ type: "text", text: "kept", marks: [{ type: "bold" }] }] },
    ]);
    e.destroy();
  });
});
