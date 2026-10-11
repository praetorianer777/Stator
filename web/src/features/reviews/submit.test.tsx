import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReviewRule } from "@/api/reviews";
import type { Draft, VersionEntry } from "@/api/versions";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
// The edit route's chunk carries the editor; loaded here it is at hand for the router.
import "@/routes/page-edit.lazy";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// The editor's chunk loads and the editor mounts in jsdom, which is slow on a busy machine.
const EDITOR_TEST_MS = 20_000;

const space = aSpace();
const home = aPage();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000a3";
const EDIT_PATH = `/s/DOCS/p/${PAGE_ID}/policy/edit`;
const rule: ReviewRule = {
  id: "0195f000-0000-7000-8000-0000000000d2",
  spaceKey: "DOCS",
  page: null,
  reviewers: [{ type: "user", id: "0195f000-0000-7000-8000-0000000000f1", name: "Ann Reviewer" }],
  required: 1,
  updatedByName: "Ada Lovelace",
  updatedAt: "2026-10-01T09:00:00Z",
};
const policy = aPage({
  id: PAGE_ID,
  title: "Policy",
  home: false,
  parentId: home.id,
  ancestors: [{ id: home.id, title: "Handbook", home: true }],
  version: 3,
  review: { rule, pending: null },
});
const draft: Draft = {
  pageId: PAGE_ID,
  title: "Policy",
  body: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "Lock the door." }] }] },
  baseVersion: 3,
  updatedAt: "2026-10-01T10:00:00Z",
};

function stubEditor(more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${PAGE_ID}`]: { status: 200, body: { page: policy, space } },
    [`GET /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft } },
    [`GET /pages/${PAGE_ID}/attachments`]: { status: 200, body: { attachments: [] } },
    [`POST /pages/${PAGE_ID}/visit`]: { status: 204 },
    ...more,
  });
}

const submits = (sent: ReturnType<typeof stubApi>) => sent.filter((r) => r.method === "POST" && r.path === `/pages/${PAGE_ID}/reviews`);

describe("a draft of a page held to a review", () => {
  it(
    "is submitted for review with its comment and whether to tell the watchers, never published",
    async () => {
      const review = {
        id: "0195f000-0000-7000-8000-0000000000e2",
        pageId: PAGE_ID,
        state: "pending",
        authorId: null,
        authorName: "Ada Lovelace",
        baseVersion: 3,
        title: "Policy",
        comment: "Locks.",
        reviewers: rule.reviewers,
        required: 1,
        approvals: 0,
        decisions: [],
        can: { decide: false, withdraw: true, skip: true },
        submittedAt: "2026-10-01T10:00:00Z",
        finishedAt: null,
        version: null,
        skipReason: "",
        skippedByName: "",
      };
      let submitted = false;
      const sent = stubEditor({
        [`GET /pages/${PAGE_ID}`]: () => ({ status: 200, body: { page: submitted ? { ...policy, review: { rule, pending: review } } : policy, space } }),
        [`POST /pages/${PAGE_ID}/reviews`]: () => {
          submitted = true;
          return { status: 201, body: { review } };
        },
      });
      const router = await renderAt(EDIT_PATH);
      expect(await screen.findByRole("button", { name: "Submit for review" })).toHaveAttribute("data-action", "submit-review");
      expect(screen.queryByRole("button", { name: "Publish" })).toBeNull();
      await userEvent.click(screen.getByRole("button", { name: "Submit for review" }));
      const dialog = await screen.findByRole("dialog", { name: "Submit Policy for review" });
      expect(within(dialog).getByText(/once one of them approves it/)).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]);
      await userEvent.type(within(dialog).getByLabelText("What changed"), "Locks.");
      await userEvent.click(within(dialog).getByRole("button", { name: "Submit for review" }));
      await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${PAGE_ID}/policy`));
      expect(submits(sent)[0]!.body).toEqual({ comment: "Locks.", notifyWatchers: true });
      expect(sent.some((r) => r.path === `/pages/${PAGE_ID}/publish`)).toBe(false);
      expect(await screen.findByText("A draft of this page is waiting for review.")).toBeInTheDocument();
    },
    EDITOR_TEST_MS,
  );

  it(
    "says why it was refused while another review waits",
    async () => {
      const refusal = "This page already waits on a review. Wait until it is decided or withdrawn, then submit again.";
      stubEditor({ [`POST /pages/${PAGE_ID}/reviews`]: { status: 409, body: { error: { code: "review_pending", message: refusal } } } });
      await renderAt(EDIT_PATH);
      await userEvent.click(await screen.findByRole("button", { name: "Submit for review" }));
      const dialog = await screen.findByRole("dialog", { name: "Submit Policy for review" });
      await userEvent.click(within(dialog).getByRole("button", { name: "Submit for review" }));
      expect(await screen.findByText(refusal)).toBeInTheDocument();
      expect(screen.queryByRole("dialog")).toBeNull();
    },
    EDITOR_TEST_MS,
  );

  it(
    "offers to keep the draft when the page was published since it began, and submits it then",
    async () => {
      let first = true;
      const sent = stubEditor({
        [`PUT /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft: { ...draft, baseVersion: 3 } } },
        [`POST /pages/${PAGE_ID}/reviews`]: () => {
          if (first) {
            first = false;
            return { status: 409, body: { error: { code: "publish_conflict", message: "Somebody published this page since your draft began." } } };
          }
          return { status: 409, body: { error: { code: "no_draft", message: "You have no draft of this page to submit. Change something first." } } };
        },
      });
      await renderAt(EDIT_PATH);
      await userEvent.click(await screen.findByRole("button", { name: "Submit for review" }));
      await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Submit for review" }));
      const conflict = (await screen.findByText(/Somebody published this page/, { selector: "p" })).closest("[data-publish-conflict]") as HTMLElement;
      await userEvent.click(within(conflict).getByRole("button", { name: /Keep my draft/ }));
      expect(await screen.findByText("You have no draft of this page to submit. Change something first.")).toBeInTheDocument();
      expect(submits(sent)).toHaveLength(2);
    },
    EDITOR_TEST_MS,
  );
});

describe("the history of a reviewed page", () => {
  const entry = (number: number, over: Partial<VersionEntry> = {}): VersionEntry => ({
    number,
    title: "Policy",
    comment: "",
    authorId: null,
    authorName: "Bob Author",
    createdAt: `2026-10-0${number}T09:00:00Z`,
    restoredFrom: null,
    live: false,
    updatedAt: `2026-10-0${number}T09:00:00Z`,
    coEditors: [],
    originalAuthor: null,
    review: null,
    ...over,
  });

  it("says who approved each version, or who published it without review and why", async () => {
    stubEditor({
      [`GET /pages/${PAGE_ID}/versions`]: {
        status: 200,
        body: {
          versions: [
            entry(3, { review: { reviewId: "r1", approvedBy: ["Ann Reviewer", "Carl Lead"], skippedByName: "", skipReason: "" } }),
            entry(2, { review: { reviewId: "r0", approvedBy: [], skippedByName: "Ada Lovelace", skipReason: "The office is on fire." } }),
            entry(1),
          ],
          total: 3,
        },
      },
    });
    await renderAt(`/s/DOCS/p/${PAGE_ID}/policy/history`);
    expect(await screen.findByText("Approved by Ann Reviewer and Carl Lead")).toHaveAttribute("data-version-review", "approved");
    expect(screen.getByText("Published without review by Ada Lovelace: The office is on fire.")).toHaveAttribute("data-version-review", "skipped");
    expect(document.querySelector('[data-version-row="1"] [data-version-review]')).toBeNull();
  });
});
