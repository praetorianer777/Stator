import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TreeNode } from "@/api/tree";
import type { Comparison, Draft, VersionEntry } from "@/api/versions";
import { DRAFT_AUTOSAVE_MS } from "@/config";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// The editor's chunk loads and the editor mounts in jsdom, which is slow on a busy machine.
const EDITOR_TEST_MS = 20_000;
// Long enough for the autosave's pause and the save after it.
const AUTOSAVE_WAIT = { timeout: DRAFT_AUTOSAVE_MS * 5 };

const space = aSpace();
const home = aPage();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000a1";
const plans = aPage({
  id: PAGE_ID,
  title: "Plans",
  home: false,
  parentId: home.id,
  ancestors: [{ id: home.id, title: "Handbook", home: true }],
  version: 3,
});
const paragraph = (text: string) => ({ type: "paragraph", content: [{ type: "text", text }] });
const aDraft = (over: Partial<Draft> = {}): Draft => ({
  pageId: PAGE_ID,
  title: "Plans for next year",
  body: { type: "doc", content: [paragraph("Draft words.")] },
  baseVersion: 3,
  updatedAt: "2026-09-29T10:00:00Z",
  ...over,
});
const conflict: Answer = {
  status: 409,
  body: { error: { code: "publish_conflict", message: "Somebody published this page since your draft began." } },
};

/** The API around one page of a small space; more answers go on top. */
function stubPage(more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}, { page = plans, draft = null as Draft | null } = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    [`GET /pages/${PAGE_ID}`]: { status: 200, body: { page, space } },
    [`GET /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft } },
    ...more,
  });
}

const savedDraft: Answer = { status: 200, body: { draft: aDraft() } };
const saveFailed: Answer = { status: 503, body: { error: { code: "unavailable", message: "The server is busy." } } };

/** Answers that wait until the test lets each one go, in the order they were asked. */
function heldAnswers() {
  const waiting: Array<(answer: Answer) => void> = [];
  return { waiting, answer: () => new Promise<Answer>((resolve) => waiting.push(resolve)) };
}

const draftStatus = () => document.querySelector("[data-draft-status]");

const EDIT_PATH = `/s/DOCS/p/${PAGE_ID}/plans/edit`;
// Every page view notes a visit, which is not the change a test makes.
const isVisit = (r: { path: string }) => r.path.endsWith("/visit");
const isChange = (r: { method: string; path: string }) => r.method === "POST" && !isVisit(r);
const puts = (sent: ReturnType<typeof stubApi>) => sent.filter((r) => r.method === "PUT" && r.path === `/pages/${PAGE_ID}/draft`);

describe("the editor", () => {
  it(
    "saves what is typed to a private draft once typing pauses",
    async () => {
      const sent = stubPage({ [`PUT /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft: aDraft({ title: "Plans 2027" }) } } });
      await renderAt(EDIT_PATH);
      const title = await screen.findByLabelText("Title");
      await userEvent.clear(title);
      await userEvent.type(title, "Plans 2027");
      expect(puts(sent)).toHaveLength(0);
      expect(await screen.findByText("Draft saved. Only you can see it until you publish.", {}, AUTOSAVE_WAIT)).toBeInTheDocument();
      expect(puts(sent)).toHaveLength(1);
      expect(puts(sent)[0]!.body).toMatchObject({ title: "Plans 2027", baseVersion: 3, body: { type: "doc" } });
      expect(sent.some((r) => isChange(r) || r.method === "PATCH")).toBe(false);
    },
    EDITOR_TEST_MS,
  );

  it(
    "opens on the caller's draft rather than the published page",
    async () => {
      stubPage({}, { draft: aDraft() });
      await renderAt(EDIT_PATH);
      expect(await screen.findByLabelText("Title")).toHaveValue("Plans for next year");
      await waitFor(() => expect(document.getElementById("page-body")).toHaveTextContent("Draft words."));
      expect(screen.getByRole("button", { name: "Discard draft" })).toBeInTheDocument();
    },
    EDITOR_TEST_MS,
  );

  it(
    "offers nothing to publish until something changed",
    async () => {
      stubPage();
      await renderAt(EDIT_PATH);
      await screen.findByLabelText("Title");
      expect(screen.getByRole("button", { name: "Publish" })).toBeDisabled();
      expect(screen.getByText("Nothing to publish yet: change something first.")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Discard draft" })).toBeNull();
    },
    EDITOR_TEST_MS,
  );

  it(
    "publishes the draft with a comment and whether to tell the watchers",
    async () => {
      const published = aPage({ ...plans, title: "Plans for next year", version: 4, draft: null });
      const sent = stubPage({ [`POST /pages/${PAGE_ID}/publish`]: { status: 200, body: { page: published, version: {} } } }, { draft: aDraft() });
      const router = await renderAt(EDIT_PATH);
      await userEvent.click(await screen.findByRole("button", { name: "Publish" }));
      const dialog = await screen.findByRole("dialog", { name: "Publish Plans for next year" });
      const notify = within(dialog).getByLabelText("Notify the people watching this page");
      expect(notify).toBeChecked();
      expect(await axeViolations()).toEqual([]);
      await userEvent.type(within(dialog).getByLabelText("What changed"), "  Added the budget.  ");
      await userEvent.click(notify);
      await userEvent.click(within(dialog).getByRole("button", { name: "Publish" }));
      await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${PAGE_ID}/plans-for-next-year`));
      expect(sent.find(isChange)?.body).toEqual({ comment: "Added the budget.", notifyWatchers: false });
      expect(puts(sent)).toHaveLength(0);
    },
    EDITOR_TEST_MS,
  );

  it(
    "saves what is still waiting before it publishes",
    async () => {
      const sent = stubPage({
        [`PUT /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft: aDraft({ title: "Plans now" }) } },
        [`POST /pages/${PAGE_ID}/publish`]: { status: 200, body: { page: aPage({ ...plans, title: "Plans now", version: 4 }), version: {} } },
      });
      await renderAt(EDIT_PATH);
      const title = await screen.findByLabelText("Title");
      await userEvent.clear(title);
      await userEvent.type(title, "Plans now");
      await userEvent.click(screen.getByRole("button", { name: "Publish" }));
      await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Publish" }));
      await waitFor(() => expect(sent.some(isChange)).toBe(true));
      const order = sent.filter((r) => r.method !== "GET" && !isVisit(r)).map((r) => r.method);
      expect(order).toEqual(["PUT", "POST"]);
    },
    EDITOR_TEST_MS,
  );

  it(
    "refuses an empty title before asking the API",
    async () => {
      const sent = stubPage();
      await renderAt(EDIT_PATH);
      await userEvent.clear(await screen.findByLabelText("Title"));
      await userEvent.click(screen.getByRole("button", { name: "Publish" }));
      expect(await screen.findByText("A page needs a title.")).toBeInTheDocument();
      expect(await screen.findByText("A page needs a title before the draft can be saved.", {}, AUTOSAVE_WAIT)).toBeInTheDocument();
      expect(sent.some((r) => r.method !== "GET" && !isVisit(r))).toBe(false);
    },
    EDITOR_TEST_MS,
  );

  it(
    "says saving until the last of overlapping saves lands, however slow the first",
    async () => {
      const saves = heldAnswers();
      const sent = stubPage({ [`PUT /pages/${PAGE_ID}/draft`]: saves.answer });
      await renderAt(EDIT_PATH);
      const title = await screen.findByLabelText("Title");
      await userEvent.clear(title);
      await userEvent.type(title, "Plans A");
      await waitFor(() => expect(saves.waiting).toHaveLength(1), AUTOSAVE_WAIT);
      await userEvent.type(title, " B");
      expect(draftStatus()).toHaveAttribute("data-draft-status", "pending");
      await waitFor(() => expect(draftStatus()).toHaveAttribute("data-draft-status", "saving"), AUTOSAVE_WAIT);

      saves.waiting[0]!(savedDraft);
      await waitFor(() => expect(saves.waiting).toHaveLength(2));
      expect(draftStatus()).toHaveAttribute("data-draft-status", "saving");
      saves.waiting[1]!(savedDraft);
      await waitFor(() => expect(draftStatus()).toHaveAttribute("data-draft-status", "saved"));
      expect(puts(sent).map((r) => (r.body as { title: string }).title)).toEqual(["Plans A", "Plans A B"]);
    },
    EDITOR_TEST_MS,
  );

  it(
    "says a failed save failed, and saved once a later one lands",
    async () => {
      let tries = 0;
      stubPage({ [`PUT /pages/${PAGE_ID}/draft`]: () => (++tries === 1 ? saveFailed : savedDraft) });
      await renderAt(EDIT_PATH);
      const title = await screen.findByLabelText("Title");
      await userEvent.clear(title);
      await userEvent.type(title, "Plans A");
      expect(await screen.findByText(/Your draft could not be saved: The server is busy\./, {}, AUTOSAVE_WAIT)).toBeInTheDocument();
      expect(draftStatus()).toHaveAttribute("data-draft-status", "error");
      await userEvent.type(title, " B");
      await waitFor(() => expect(draftStatus()).toHaveAttribute("data-draft-status", "saved"), AUTOSAVE_WAIT);
      expect(screen.queryByText(/Your draft could not be saved/)).toBeNull();
    },
    EDITOR_TEST_MS,
  );

  it(
    "does not call the draft unsaved when an older save fails while a newer one waits",
    async () => {
      const saves = heldAnswers();
      stubPage({ [`PUT /pages/${PAGE_ID}/draft`]: saves.answer });
      await renderAt(EDIT_PATH);
      const title = await screen.findByLabelText("Title");
      await userEvent.clear(title);
      await userEvent.type(title, "Plans A");
      await waitFor(() => expect(saves.waiting).toHaveLength(1), AUTOSAVE_WAIT);
      await userEvent.type(title, " B");
      await waitFor(() => expect(draftStatus()).toHaveAttribute("data-draft-status", "saving"), AUTOSAVE_WAIT);

      saves.waiting[0]!(saveFailed);
      await waitFor(() => expect(saves.waiting).toHaveLength(2));
      expect(draftStatus()).toHaveAttribute("data-draft-status", "saving");
      saves.waiting[1]!(savedDraft);
      await waitFor(() => expect(draftStatus()).toHaveAttribute("data-draft-status", "saved"));
      expect(screen.queryByText(/Your draft could not be saved/)).toBeNull();
    },
    EDITOR_TEST_MS,
  );

  it(
    "learns the newer version from a read made after the conflict, not from its own copy",
    async () => {
      let current = plans;
      const sent = stubPage(
        {
          [`GET /pages/${PAGE_ID}`]: () => ({ status: 200, body: { page: current, space } }),
          [`POST /pages/${PAGE_ID}/publish`]: () => {
            current = aPage({ ...plans, version: 5 });
            return conflict;
          },
        },
        { draft: aDraft() },
      );
      await renderAt(EDIT_PATH);
      await userEvent.click(await screen.findByRole("button", { name: "Publish" }));
      await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Publish" }));
      expect(await screen.findByRole("button", { name: "Compare with version 5" })).toBeInTheDocument();
      const refused = sent.findIndex((r) => r.method === "POST");
      expect(sent.slice(refused + 1).some((r) => r.method === "GET" && r.path === `/pages/${PAGE_ID}`)).toBe(true);
    },
    EDITOR_TEST_MS,
  );

  it(
    "explains a publish conflict, and publishes over it once the draft is kept",
    async () => {
      let current = plans;
      let tries = 0;
      const sent = stubPage(
        {
          [`GET /pages/${PAGE_ID}`]: () => ({ status: 200, body: { page: current, space } }),
          [`PUT /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft: aDraft({ baseVersion: 4 }) } },
          [`POST /pages/${PAGE_ID}/publish`]: () => {
            tries += 1;
            if (tries === 1) {
              current = aPage({ ...plans, version: 4, updatedByName: "Bob" });
              return conflict;
            }
            return { status: 200, body: { page: aPage({ ...plans, title: "Plans for next year", version: 5 }), version: {} } };
          },
        },
        { draft: aDraft() },
      );
      const router = await renderAt(EDIT_PATH);
      await userEvent.click(await screen.findByRole("button", { name: "Publish" }));
      await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Publish" }));

      const alert = await screen.findByText("Somebody published this page while you were editing.");
      const panel = alert.closest("[data-publish-conflict]") as HTMLElement;
      expect(panel).toHaveTextContent("Compare your draft with version 4 first.");
      expect(within(panel).getByRole("button", { name: "Compare with version 4" })).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]);

      await userEvent.click(within(panel).getByRole("button", { name: "Keep my draft and publish" }));
      await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${PAGE_ID}/plans-for-next-year`));
      expect(puts(sent).map((r) => (r.body as { baseVersion: number }).baseVersion)).toEqual([4]);
      expect(sent.filter(isChange)).toHaveLength(2);
    },
    EDITOR_TEST_MS,
  );

  it(
    "sends a conflicted author to compare the other version with their draft",
    async () => {
      let current = plans;
      stubPage(
        {
          [`GET /pages/${PAGE_ID}`]: () => ({ status: 200, body: { page: current, space } }),
          [`POST /pages/${PAGE_ID}/publish`]: () => {
            current = aPage({ ...plans, version: 4 });
            return conflict;
          },
        },
        { draft: aDraft() },
      );
      const router = await renderAt(EDIT_PATH);
      await userEvent.click(await screen.findByRole("button", { name: "Publish" }));
      await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Publish" }));
      await userEvent.click(await screen.findByRole("button", { name: "Compare with version 4" }));
      await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${PAGE_ID}/plans/history`));
      expect(router.state.location.search).toEqual({ from: 4, to: "draft" });
    },
    EDITOR_TEST_MS,
  );

  it(
    "discards the draft after asking, and shows the page as published",
    async () => {
      const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValue(true);
      const sent = stubPage({ [`DELETE /pages/${PAGE_ID}/draft`]: { status: 204 } }, { draft: aDraft() });
      const router = await renderAt(EDIT_PATH);
      const discard = await screen.findByRole("button", { name: "Discard draft" });
      await userEvent.click(discard);
      expect(sent.some((r) => r.method === "DELETE")).toBe(false);
      await userEvent.click(discard);
      await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${PAGE_ID}/plans`));
      expect(sent.filter((r) => r.method === "DELETE").map((r) => r.path)).toEqual([`/pages/${PAGE_ID}/draft`]);
      expect(confirm).toHaveBeenCalledWith("Throw your draft away? The page stays as it was last published.");
    },
    EDITOR_TEST_MS,
  );
});

describe("an unpublished page", () => {
  it("is marked where it is read and in the tree", async () => {
    const fresh = aPage({ ...plans, unpublished: true, version: 0 });
    const node: TreeNode = { id: PAGE_ID, parentId: home.id, title: "Plans", hasChildren: false, unpublished: true, restricted: false };
    stubPage({ "GET /spaces/DOCS/pages": { status: 200, body: { pages: [node] } } }, { page: fresh });
    await renderAt(`/s/DOCS/p/${PAGE_ID}/plans`);
    expect(await screen.findByText("Only you can see this page until you publish it.")).toBeInTheDocument();
    expect(screen.getByText("Unpublished")).toBeInTheDocument();
    const item = await screen.findByRole("treeitem", { name: "Plans, unpublished" });
    expect(item.querySelector("[data-tree-unpublished]")).not.toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("tells its author about their draft and takes them back to it", async () => {
    stubPage({}, { page: aPage({ ...plans, draft: { baseVersion: 3, updatedAt: "2026-09-29T10:00:00Z" } }), draft: aDraft() });
    const router = await renderAt(`/s/DOCS/p/${PAGE_ID}/plans`);
    expect(await screen.findByText("You have a draft of this page that nobody else sees yet.")).toBeInTheDocument();
    expect(screen.queryByText("Unpublished")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Continue editing" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(EDIT_PATH));
  });
});

const entry = (number: number, over: Partial<VersionEntry> = {}): VersionEntry => ({
  number,
  title: "Plans",
  comment: "",
  authorId: null,
  authorName: "Ada Lovelace",
  createdAt: `2026-09-2${number}T09:00:00Z`,
  restoredFrom: null,
  ...over,
});
const versions = [
  entry(3, { comment: "Back to the first plan.", restoredFrom: 1, authorName: "Bob" }),
  entry(2, { comment: "Added the budget." }),
  entry(1, { title: "Plan" }),
];
const HISTORY_PATH = `/s/DOCS/p/${PAGE_ID}/plans/history`;

function stubHistory(more: Record<string, Answer | ((request: Request) => Answer)> = {}, options: { page?: typeof plans; draft?: Draft | null } = {}) {
  return stubPage({ [`GET /pages/${PAGE_ID}/versions`]: { status: 200, body: { versions, total: 3, limit: 20, offset: 0 } }, ...more }, options);
}

describe("a page's history", () => {
  it("lists every version with who published it, when and why", async () => {
    stubHistory();
    await renderAt(HISTORY_PATH);
    const row = (n: number) => document.querySelector(`[data-version-row="${n}"]`) as HTMLElement;
    await screen.findByRole("heading", { name: "History of Plans", level: 1 });
    await waitFor(() => expect(row(3)).not.toBeNull());
    expect(row(3)).toHaveTextContent("Latest");
    expect(row(3)).toHaveTextContent("Bob");
    expect(row(3)).toHaveTextContent("Back to the first plan.");
    expect(row(3)).toHaveTextContent("Restored from version 1");
    expect(row(2)).toHaveTextContent("Added the budget.");
    expect(row(1)).toHaveTextContent("No comment");
    expect(row(1)).toHaveTextContent("Plan");
    expect(within(row(3)).queryByRole("button", { name: "Restore version 3" })).toBeNull();
    expect(within(row(2)).getByRole("button", { name: "Restore version 2" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("is reached from the page, by every reader", async () => {
    const reader = aSpace({ can: { ...space.can, editPages: false } });
    stubHistory({
      "GET /spaces/DOCS": { status: 200, body: { space: reader } },
      [`GET /pages/${PAGE_ID}`]: { status: 200, body: { page: { ...plans, can: { ...plans.can, edit: false } }, space: reader } },
    });
    const router = await renderAt(`/s/DOCS/p/${PAGE_ID}/plans`);
    await userEvent.click(await screen.findByRole("button", { name: "History" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(HISTORY_PATH));
    await waitFor(() => expect(document.querySelector('[data-version-row="2"]')).not.toBeNull());
    expect(screen.queryByRole("button", { name: /^Restore/ })).toBeNull();
  });

  it("shows an old version read-only", async () => {
    stubHistory({
      [`GET /pages/${PAGE_ID}/versions/1`]: {
        status: 200,
        body: { version: { ...entry(1, { title: "Plan", comment: "First go." }), body: { type: "doc", content: [paragraph("The first plan.")] } } },
      },
    });
    await renderAt(`${HISTORY_PATH}?version=1`);
    expect(await screen.findByRole("heading", { name: "Plan, version 1", level: 1 })).toBeInTheDocument();
    expect(await screen.findByText("The first plan.")).toBeInTheDocument();
    expect(screen.getByText("First go.")).toBeInTheDocument();
    expect(screen.getByText("You are reading an old version. The latest is version 3.")).toBeInTheDocument();
    expect(document.querySelector("[contenteditable]")).toBeNull();
    expect(screen.getByRole("button", { name: "Restore version 1" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("compares the two versions picked", async () => {
    stubHistory();
    const router = await renderAt(HISTORY_PATH);
    const compare = await screen.findByRole("button", { name: "Compare selected" });
    expect(compare).toBeDisabled();
    await userEvent.click(await screen.findByRole("checkbox", { name: "Select Version 3 to compare" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Select Version 1 to compare" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Select Version 2 to compare" }));
    expect(screen.getByRole("checkbox", { name: "Select Version 3 to compare" })).not.toBeChecked();
    await userEvent.click(compare);
    await waitFor(() => expect(router.state.location.search).toEqual({ from: 1, to: 2 }));
  });

  it("offers the caller's draft among the versions to compare", async () => {
    stubHistory({}, { page: aPage({ ...plans, draft: { baseVersion: 3, updatedAt: "2026-09-29T10:00:00Z" } }) });
    const router = await renderAt(HISTORY_PATH);
    await userEvent.click(await screen.findByRole("checkbox", { name: "Select Your draft to compare" }));
    await userEvent.click(await screen.findByRole("checkbox", { name: "Select Version 3 to compare" }));
    await userEvent.click(screen.getByRole("button", { name: "Compare selected" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ from: 3, to: "draft" }));
  });

  it("restores a version after asking, as a new version", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValue(true);
    const sent = stubHistory({
      [`POST /pages/${PAGE_ID}/versions/2/restore`]: { status: 200, body: { page: aPage({ ...plans, version: 4 }), version: entry(4, { restoredFrom: 2 }) } },
    });
    await renderAt(HISTORY_PATH);
    const restore = await screen.findByRole("button", { name: "Restore version 2" });
    await userEvent.click(restore);
    expect(sent.some(isChange)).toBe(false);
    await userEvent.click(restore);
    expect(await screen.findByText("Restored version 2 as version 4.")).toBeInTheDocument();
    expect(sent.find(isChange)?.body).toEqual({ baseVersion: 3 });
    expect(confirm).toHaveBeenLastCalledWith(
      "Restore version 2? It is published again as a new version on top of the history, and nothing in the history is lost.",
    );
  });

  it("says what to do when the history changed under a restore", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    stubHistory({
      [`POST /pages/${PAGE_ID}/versions/2/restore`]: { status: 409, body: { error: { code: "conflict", message: "Stale." } } },
    });
    await renderAt(HISTORY_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Restore version 2" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Somebody published this page while you were looking, so the history has changed. Look at the latest version, then restore again if you still want to.",
    );
  });
});

const comparison: Comparison = {
  from: { number: 1, draft: false, title: "Plan", authorName: "Ada Lovelace", createdAt: "2026-09-21T09:00:00Z" },
  to: { number: 0, draft: true, title: "Plans", authorName: "Ada Lovelace", createdAt: "2026-09-29T10:00:00Z" },
  blocks: [
    { change: "equal", node: paragraph("Unchanged words.") },
    {
      change: "modified",
      node: {
        type: "paragraph",
        content: [
          { type: "text", text: "The budget is " },
          { type: "text", text: "small", marks: [{ type: "diffDelete" }] },
          { type: "text", text: "large", marks: [{ type: "diffInsert" }] },
          { type: "text", text: "." },
        ],
      },
    },
    { change: "deleted", node: { type: "heading", attrs: { level: 1, id: "goals" }, content: [{ type: "text", text: "Goals" }] } },
    { change: "inserted", node: { type: "heading", attrs: { level: 1, id: "goals" }, content: [{ type: "text", text: "Goals" }] } },
  ],
};

describe("comparing", () => {
  it("draws what was added and removed in words as well as colour", async () => {
    let asked = "";
    stubHistory({
      [`GET /pages/${PAGE_ID}/compare`]: (request) => {
        asked = new URL(request.url).search;
        return { status: 200, body: { comparison } };
      },
    });
    await renderAt(`${HISTORY_PATH}?from=1&to=draft`);
    expect(await screen.findByRole("heading", { name: "Compare versions", level: 1 })).toBeInTheDocument();
    await waitFor(() => expect(asked).toBe("?from=1&to=draft"));
    const view = await waitFor(() => document.querySelector("[data-diff-view]") as HTMLElement);
    expect(view.querySelector("del[data-diff]")).toHaveTextContent("Removed: small (end of removal)");
    expect(view.querySelector("ins[data-diff]")).toHaveTextContent("Added: large (end of addition)");
    expect(view.querySelector('[data-diff-block="deleted"]')).toHaveTextContent("RemovedGoals");
    expect(view.querySelector('[data-diff-block="inserted"]')).toHaveTextContent("AddedGoals");
    expect(view.querySelector('[data-diff-block="modified"]')).toHaveTextContent("Changed");
    expect(view.querySelectorAll("[id]")).toHaveLength(0);
    expect(screen.getByText("Title changed from Plan to Plans.")).toBeInTheDocument();
    expect(document.querySelector("[data-compare-sides]")).toHaveTextContent("Version 1");
    expect(document.querySelector("[data-compare-sides]")).toHaveTextContent("Your draft");
    expect(screen.getByRole("button", { name: "Continue editing" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("compares a version with the one before it from the list", async () => {
    stubHistory({ [`GET /pages/${PAGE_ID}/compare`]: { status: 200, body: { comparison } } });
    const router = await renderAt(HISTORY_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Compare with previous, for version 2" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ from: 1, to: 2 }));
    expect(await screen.findByRole("heading", { name: "Compare versions", level: 1 })).toBeInTheDocument();
  });

  it("says so when there is nothing between the two", async () => {
    const same: Comparison = { ...comparison, to: { ...comparison.from, number: 2 }, blocks: [{ change: "equal", node: paragraph("Same.") }] };
    stubHistory({ [`GET /pages/${PAGE_ID}/compare`]: { status: 200, body: { comparison: same } } });
    await renderAt(`${HISTORY_PATH}?from=1&to=2`);
    expect(await screen.findByText("These two are the same.")).toBeInTheDocument();
  });
});
