import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor as Tiptap } from "@tiptap/core";
import { collabTransport } from "@/features/collab/transport";
import { editorExtensions } from "@/features/editor/extensions";
import { arrival, renderAt, stubApi, type Answer } from "@/test/app";
import { aPage, aSpace } from "@/test/spaces";

// The editor's chunk loads and the editor mounts in jsdom, which is slow on a busy machine.
const EDITOR_TEST_MS = 20_000;

const space = aSpace();
const home = aPage();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000c3";
const PATH = `/s/DOCS/p/${PAGE_ID}/notes`;
const notes = aPage({
  id: PAGE_ID,
  title: "Notes",
  home: false,
  parentId: home.id,
  ancestors: [{ id: home.id, title: "Handbook", home: true }],
  version: 2,
});
const was = { ...collabTransport };

afterEach(() => {
  Object.assign(collabTransport, was);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function stubPage(page = notes, more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    [`GET /pages/${PAGE_ID}`]: { status: 200, body: { page, space } },
    [`GET /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft: null } },
    ...more,
  });
}

const savedVersion = {
  number: 3,
  title: "Notes!",
  comment: "",
  authorId: null,
  authorName: "Ada",
  createdAt: "2026-10-06T09:00:00Z",
  updatedAt: "2026-10-06T09:00:00Z",
  restoredFrom: null,
  live: true,
  coEditors: [],
  originalAuthor: null,
};

describe("a live page", () => {
  it("tells its readers it is live", async () => {
    stubPage(aPage({ ...notes, mode: "live" }));
    const router = await renderAt(PATH);
    await arrival(router, PATH);
    expect(await screen.findByText("Live")).toHaveAttribute("data-live-badge");
  });

  it(
    "is saved to the page as it is typed, with nothing to publish",
    async () => {
      collabTransport.enabled = false;
      const sent = stubPage(aPage({ ...notes, mode: "live" }), {
        [`PUT /pages/${PAGE_ID}/live`]: { status: 200, body: { version: savedVersion, amended: false } },
      });
      await renderAt(`${PATH}/edit`);
      const title = await screen.findByLabelText("Title");
      expect(document.querySelector("[data-page-mode]")).toHaveAttribute("data-page-mode", "live");
      expect(screen.queryByRole("button", { name: "Publish" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Discard draft" })).toBeNull();
      expect(screen.getByRole("button", { name: "Done" })).toBeInTheDocument();
      await userEvent.type(title, "!");
      await waitFor(() => expect(document.querySelector("[data-draft-status]")).toHaveTextContent("Saved. Everybody reading the page sees it."));
      const saves = sent.filter((r) => r.method === "PUT");
      expect(saves.map((r) => r.path)).toEqual([`/pages/${PAGE_ID}/live`]);
      expect(saves[0]!.body).toMatchObject({ title: "Notes!" });
      expect(saves[0]!.body).not.toHaveProperty("room");
    },
    EDITOR_TEST_MS,
  );
});

describe("choosing how a page is edited", () => {
  it("makes a page live once its unpublished drafts are confirmed thrown away", async () => {
    const pending: Answer = {
      status: 409,
      body: { error: { code: "drafts_pending", message: "Bob has a draft of this page that nobody published. Making the page live throws them away." } },
    };
    const sent = stubPage(notes, {
      [`PUT /pages/${PAGE_ID}/mode`]: (request: Request) =>
        request
          .clone()
          .json()
          .then((input: { discardDrafts?: boolean }) => (input.discardDrafts ? { status: 200, body: { mode: "live", discardedDrafts: ["Bob"] } } : pending)),
    });
    const router = await renderAt(PATH);
    await arrival(router, PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Editing mode" }));
    const dialog = await screen.findByRole("dialog", { name: "How this page is edited" });
    expect(within(dialog).getByLabelText(/Drafts and publishing/)).toBeChecked();
    await userEvent.click(within(dialog).getByLabelText(/^Live/));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByText(/Bob has a draft of this page/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Make it live anyway" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "How this page is edited" })).toBeNull());
    const asked = sent.filter((r) => r.method === "PUT" && r.path === `/pages/${PAGE_ID}/mode`).map((r) => r.body);
    expect(asked).toEqual([
      { mode: "live", discardDrafts: false },
      { mode: "live", discardDrafts: true },
    ]);
  });
});

describe("checklist items in a live editor", () => {
  const item = (taskId: string | null, text: string) => ({
    type: "taskItem",
    attrs: { checked: false, taskId },
    content: [{ type: "paragraph", content: [{ type: "text", text }] }],
  });
  const ids = (editor: Tiptap) => {
    const found: unknown[] = [];
    editor.state.doc.descendants((node) => {
      if (node.type.name === "taskItem") found.push(node.attrs.taskId);
    });
    return found;
  };

  it("get an id of their own at the first change, and a copy does not share one", () => {
    const kept = "0195f000-0000-7000-8000-0000000000aa";
    const editor = new Tiptap({
      extensions: editorExtensions({ taskIds: true }),
      content: { type: "doc", content: [{ type: "taskList", content: [item(kept, "Write"), item(kept, "Copy"), item(null, "New")] }] },
    });
    editor.commands.insertContentAt(3, "x");
    const [first, second, third] = ids(editor);
    expect(first).toBe(kept);
    expect(second).toMatch(/^[0-9a-f-]{36}$/);
    expect(second).not.toBe(kept);
    expect(third).toMatch(/^[0-9a-f-]{36}$/);
    editor.destroy();
  });

  it("are left alone in an editor of drafts, where publishing numbers them", () => {
    const editor = new Tiptap({
      extensions: editorExtensions(),
      content: { type: "doc", content: [{ type: "taskList", content: [item(null, "New")] }] },
    });
    editor.commands.insertContentAt(3, "x");
    expect(ids(editor)).toEqual([null]);
    editor.destroy();
  });
});
