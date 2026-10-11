import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Page } from "@/api/pages";
import type { PageReview, ReviewRule } from "@/api/reviews";
import type { Comparison } from "@/api/versions";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const home = aPage();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000a1";
const REVIEW_ID = "0195f000-0000-7000-8000-0000000000e1";
const PATH = `/s/DOCS/p/${PAGE_ID}/policy`;
const ids = { ann: "0195f000-0000-7000-8000-0000000000f1", bob: "0195f000-0000-7000-8000-0000000000f2", leads: "0195f000-0000-7000-8000-0000000000f3" };
const member = { edit: false, delete: false, restrict: false, comment: true, archive: false, add: false, grantEdit: false };
const notAdmin = aSpace({ can: { editPages: true, administer: false, delete: false, purgeTrash: false, addComments: true, deletePages: false } });

const spaceRule: ReviewRule = {
  id: "0195f000-0000-7000-8000-0000000000d1",
  spaceKey: "DOCS",
  page: null,
  reviewers: [
    { type: "user", id: ids.ann, name: "Ann Reviewer" },
    { type: "group", id: ids.leads, name: "Leads" },
  ],
  required: 2,
  updatedByName: "Ada Lovelace",
  updatedAt: "2026-10-01T09:00:00Z",
};

const pending = (over: Partial<PageReview> = {}): PageReview => ({
  id: REVIEW_ID,
  pageId: PAGE_ID,
  state: "pending",
  authorId: ids.bob,
  authorName: "Bob Author",
  baseVersion: 3,
  title: "Policy",
  comment: "Tightened the rules.",
  reviewers: spaceRule.reviewers,
  required: 2,
  approvals: 1,
  decisions: [{ reviewerId: ids.ann, reviewerName: "Ann Reviewer", decision: "approve", comment: "Looks right.", decidedAt: "2026-10-02T09:00:00Z" }],
  can: { decide: false, withdraw: false, skip: false },
  submittedAt: "2026-10-01T10:00:00Z",
  finishedAt: null,
  version: null,
  skipReason: "",
  skippedByName: "",
  ...over,
});

const comparison: Comparison = {
  from: { number: 3, draft: false, title: "Policy", authorName: "Ada Lovelace", createdAt: "2026-09-30T09:00:00Z" },
  to: { number: 0, draft: true, title: "Policy", authorName: "Bob Author", createdAt: "2026-10-01T10:00:00Z" },
  blocks: [
    { change: "equal", node: { type: "paragraph", content: [{ type: "text", text: "Be kind." }] } },
    { change: "inserted", node: { type: "paragraph", content: [{ type: "text", text: "Lock the door." }] } },
  ],
};

function policy(over: Partial<Page> = {}) {
  return aPage({
    id: PAGE_ID,
    title: "Policy",
    home: false,
    parentId: home.id,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    version: 3,
    review: { rule: spaceRule, pending: null },
    ...over,
  });
}

type Answers = Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;

function stubPage(page: Page | (() => Page), more: Answers = {}, space = aSpace()) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${PAGE_ID}`]: () => ({ status: 200, body: { page: typeof page === "function" ? page() : page, space } }),
    [`GET /pages/${PAGE_ID}/attachments`]: { status: 200, body: { attachments: [] } },
    [`GET /pages/${PAGE_ID}/views`]: { status: 404, body: { error: { code: "not_found", message: "Not stubbed." } } },
    [`POST /pages/${PAGE_ID}/visit`]: { status: 204 },
    ...more,
  });
}

const posts = (sent: ReturnType<typeof stubApi>, path: string) => sent.filter((r) => r.method === "POST" && r.path === path);
const banner = async () => (await screen.findByText("A draft of this page is waiting for review.")).closest("[data-review-banner]") as HTMLElement;

describe("a pending review on the page", () => {
  it("says who submitted it, how far it is, who reviews and who said what", async () => {
    stubPage(policy({ review: { rule: spaceRule, pending: pending() } }));
    await renderAt(PATH);
    const shown = await banner();
    expect(shown).toHaveAttribute("data-review-banner", REVIEW_ID);
    expect(shown).toHaveTextContent(/Submitted by Bob Author on/);
    expect(within(shown).getByText("1 of 2 approvals")).toBeInTheDocument();
    expect(within(shown).getByText("Reviewers: Ann Reviewer and Leads")).toBeInTheDocument();
    expect(within(shown).getByText("The author says: Tightened the rules.")).toBeInTheDocument();
    const decisions = within(shown).getByRole("list", { name: "Decisions so far" });
    expect(within(decisions).getByText("Ann Reviewer approved")).toBeInTheDocument();
    expect(within(decisions).getByText("Looks right.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("offers a member who may do nothing with it no action, and no rule to set", async () => {
    stubPage(policy({ review: { rule: spaceRule, pending: pending() }, can: member }), {}, notAdmin);
    await renderAt(PATH);
    const shown = await banner();
    expect(within(shown).queryByRole("button")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Page actions" }));
    expect(screen.queryByRole("menuitem", { name: "Review rule" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Ask readers to confirm" })).toBeNull();
  });

  it("lets a reviewer read the draft against the published page and approve it, after which the page shows the new version", async () => {
    let published = false;
    const sent = stubPage(
      () =>
        published
          ? policy({ version: 4, body: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "Lock the door." }] }] } })
          : policy({ review: { rule: spaceRule, pending: pending({ can: { decide: true, withdraw: false, skip: false } }) } }),
      {
        [`GET /pages/${PAGE_ID}/compare`]: { status: 200, body: { comparison } },
        [`POST /pages/${PAGE_ID}/reviews/${REVIEW_ID}/decisions`]: () => {
          published = true;
          return { status: 200, body: { review: pending({ state: "published", approvals: 2, version: 4 }), version: null } };
        },
      },
    );
    await renderAt(PATH);
    await userEvent.click(within(await banner()).getByRole("button", { name: "Review" }));
    const dialog = await screen.findByRole("dialog", { name: "Review the draft of Policy" });
    expect(await within(dialog).findByText("Lock the door.")).toBeInTheDocument();
    expect(within(dialog).getByText(/The draft under review/)).toBeInTheDocument();
    const compare = sent.find((r) => r.path === `/pages/${PAGE_ID}/compare`);
    expect(compare).toBeDefined();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(dialog).getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(posts(sent, `/pages/${PAGE_ID}/reviews/${REVIEW_ID}/decisions`)[0]!.body).toEqual({ decision: "approve" });
    await waitFor(() => expect(screen.queryByText("A draft of this page is waiting for review.")).toBeNull());
    expect(await screen.findByText("Lock the door.")).toBeInTheDocument();
  });

  it("asks the review's comparison with the draft as review:{id}", async () => {
    let asked = "";
    stubPage(policy({ review: { rule: spaceRule, pending: pending({ can: { decide: true, withdraw: false, skip: false } }) } }), {
      [`GET /pages/${PAGE_ID}/compare`]: (request) => {
        asked = new URL(request.url).searchParams.get("to") ?? "";
        return { status: 200, body: { comparison } };
      },
    });
    await renderAt(PATH);
    await userEvent.click(within(await banner()).getByRole("button", { name: "Review" }));
    await screen.findByText("Lock the door.");
    expect(asked).toBe(`review:${REVIEW_ID}`);
  });

  it("asks for changes only with a comment, and shows the server's refusal", async () => {
    const sent = stubPage(policy({ review: { rule: spaceRule, pending: pending({ can: { decide: true, withdraw: false, skip: false } }) } }), {
      [`GET /pages/${PAGE_ID}/compare`]: { status: 200, body: { comparison } },
      [`POST /pages/${PAGE_ID}/reviews/${REVIEW_ID}/decisions`]: {
        status: 409,
        body: { error: { code: "not_pending", message: "This review was decided meanwhile. Open the page again to see how." } },
      },
    });
    await renderAt(PATH);
    await userEvent.click(within(await banner()).getByRole("button", { name: "Review" }));
    const dialog = await screen.findByRole("dialog", { name: "Review the draft of Policy" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Request changes" }));
    expect(within(dialog).getByText("Say what should change, so the author knows what to do.")).toBeInTheDocument();
    expect(posts(sent, `/pages/${PAGE_ID}/reviews/${REVIEW_ID}/decisions`)).toHaveLength(0);

    await userEvent.type(within(dialog).getByLabelText("Comment"), "Name the door.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Request changes" }));
    expect(await within(dialog).findByText("This review was decided meanwhile. Open the page again to see how.")).toBeInTheDocument();
    expect(posts(sent, `/pages/${PAGE_ID}/reviews/${REVIEW_ID}/decisions`)[0]!.body).toEqual({ decision: "changes", comment: "Name the door." });
  });

  it("lets its author withdraw it after asking", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    let withdrawn = false;
    const sent = stubPage(
      () => policy({ review: { rule: spaceRule, pending: withdrawn ? null : pending({ can: { decide: false, withdraw: true, skip: false } }) } }),
      {
        [`POST /pages/${PAGE_ID}/reviews/${REVIEW_ID}/withdraw`]: () => {
          withdrawn = true;
          return { status: 200, body: { review: pending({ state: "withdrawn" }) } };
        },
      },
    );
    await renderAt(PATH);
    const shown = await banner();
    expect(within(shown).queryByRole("button", { name: "Review" })).toBeNull();
    await userEvent.click(within(shown).getByRole("button", { name: "Withdraw" }));
    await waitFor(() => expect(screen.queryByText("A draft of this page is waiting for review.")).toBeNull());
    expect(posts(sent, `/pages/${PAGE_ID}/reviews/${REVIEW_ID}/withdraw`)).toHaveLength(1);
  });

  it("lets an administrator publish it without review only with a reason", async () => {
    const sent = stubPage(policy({ review: { rule: spaceRule, pending: pending({ can: { decide: false, withdraw: false, skip: true } }) } }), {
      [`POST /pages/${PAGE_ID}/reviews/${REVIEW_ID}/skip`]: { status: 200, body: { review: pending({ state: "skipped" }), version: null } },
    });
    await renderAt(PATH);
    await userEvent.click(within(await banner()).getByRole("button", { name: "Publish without review" }));
    const dialog = await screen.findByRole("dialog", { name: "Publish Policy without review" });
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Publish without review" }));
    expect(within(dialog).getByText("Give a reason, so others can see why the review was skipped.")).toBeInTheDocument();
    expect(posts(sent, `/pages/${PAGE_ID}/reviews/${REVIEW_ID}/skip`)).toHaveLength(0);
    await userEvent.type(within(dialog).getByLabelText("Reason"), "The office is on fire.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Publish without review" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(posts(sent, `/pages/${PAGE_ID}/reviews/${REVIEW_ID}/skip`)[0]!.body).toEqual({ reason: "The office is on fire." });
  });
});

describe("a page's review rule", () => {
  const people = [{ id: ids.bob, name: "Bob Builder", email: "bob@stator.test" }];

  async function openRule() {
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Review rule" }));
    return screen.getByRole("dialog", { name: "Review rule for Policy" });
  }

  it("says the page is held to the space's rule, and sets the page's own", async () => {
    const sent = stubPage(policy(), {
      "GET /people": { status: 200, body: { people } },
      "GET /groups": { status: 200, body: { groups: [] } },
      [`PUT /pages/${PAGE_ID}/review-rule`]: {
        status: 200,
        body: {
          rule: {
            ...spaceRule,
            id: "r2",
            page: { id: PAGE_ID, title: "Policy" },
            reviewers: [{ type: "user", id: ids.bob, name: "Bob Builder" }],
            required: 1,
          },
        },
      },
    });
    await renderAt(PATH);
    const dialog = await openRule();
    expect(within(dialog).getByText("This page is held to the space's rule.")).toBeInTheDocument();
    expect(within(dialog).getByText("Reviewed by Ann Reviewer and Leads; 2 approvals needed.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(dialog).getByRole("button", { name: "Save rule" }));
    expect(within(dialog).getByText("Add at least one person or group to review.")).toBeInTheDocument();

    await userEvent.type(within(dialog).getByRole("combobox"), "bo");
    await userEvent.click(await within(dialog).findByRole("option", { name: /Bob Builder/ }));
    const required = within(dialog).getByLabelText("Approvals needed");
    await userEvent.clear(required);
    await userEvent.type(required, "11");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save rule" }));
    expect(within(dialog).getByText("Choose a number from 1 to 10.")).toBeInTheDocument();
    await userEvent.clear(required);
    await userEvent.type(required, "1");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save rule" }));
    expect(await within(dialog).findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT" && r.path === `/pages/${PAGE_ID}/review-rule`)!.body).toEqual({
      reviewers: [{ type: "user", id: ids.bob }],
      required: 1,
    });
  });

  it("shows the server's refusal while a page of the branch is live", async () => {
    const live = "A rule cannot cover a live page. Switch Runbook back to drafts first.";
    stubPage(policy({ review: { rule: null, pending: null } }), {
      "GET /people": { status: 200, body: { people } },
      "GET /groups": { status: 200, body: { groups: [] } },
      [`PUT /pages/${PAGE_ID}/review-rule`]: { status: 409, body: { error: { code: "live_pages", message: live } } },
    });
    await renderAt(PATH);
    const dialog = await openRule();
    expect(within(dialog).getByText("Publishing this page needs no review.")).toBeInTheDocument();
    await userEvent.type(within(dialog).getByRole("combobox"), "bo");
    await userEvent.click(await within(dialog).findByRole("option", { name: /Bob Builder/ }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save rule" }));
    expect(await within(dialog).findByText(live)).toBeInTheDocument();
  });

  it("names the page above whose rule holds, and removes the page's own after asking", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const above = { ...spaceRule, page: { id: home.id, title: "Handbook" } };
    const own = { ...spaceRule, id: "r3", page: { id: PAGE_ID, title: "Policy" } };
    let rule: ReviewRule | null = own;
    const sent = stubPage(() => policy({ review: { rule, pending: null } }), {
      [`DELETE /pages/${PAGE_ID}/review-rule`]: () => {
        rule = above;
        return { status: 204 };
      },
    });
    await renderAt(PATH);
    const dialog = await openRule();
    expect(within(dialog).getByText("This page and the pages below it are held to their own rule.")).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Remove rule" }));
    expect(await within(dialog).findByText("This page is held to the rule of Handbook above it.")).toBeInTheDocument();
    expect(within(dialog).getByText("The rule is removed.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE" && r.path === `/pages/${PAGE_ID}/review-rule`)).toBe(true);
  });
});

describe("the space's review settings", () => {
  const branch: ReviewRule = {
    ...spaceRule,
    id: "r4",
    page: { id: PAGE_ID, title: "Policy" },
    reviewers: [{ type: "user", id: ids.ann, name: "Ann Reviewer" }],
    required: 1,
  };

  function stubSettings(space = aSpace(), more: Answers = {}) {
    return stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/review-rules": { status: 200, body: { rules: [spaceRule, branch] } },
      ...more,
    });
  }

  it("shows the space's rule and the branches with their own, and removes a branch's", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const sent = stubSettings(aSpace(), { [`DELETE /pages/${PAGE_ID}/review-rule`]: { status: 204 } });
    await renderAt("/s/DOCS/settings?tab=reviews");
    const whole = (await screen.findByText("The whole space")).closest("[data-space-review-rule]") as HTMLElement;
    expect(whole).toHaveAttribute("data-space-review-rule", "set");
    expect(within(whole).getByText("Ann Reviewer")).toBeInTheDocument();
    expect(within(whole).getByLabelText("Approvals needed")).toHaveValue(2);
    const row = document.querySelector(`[data-branch-rule="${PAGE_ID}"]`) as HTMLElement;
    expect(within(row).getByRole("link", { name: "Policy" })).toHaveAttribute("href", PATH);
    expect(row).toHaveTextContent("Reviewed by Ann Reviewer; 1 approval needed.");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(row).getByRole("button", { name: "Remove the rule of Policy" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === `/pages/${PAGE_ID}/review-rule`)).toBe(true));
  });

  it("removes the space's rule after asking", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const sent = stubSettings(aSpace(), { "DELETE /spaces/DOCS/review-rule": { status: 204 } });
    await renderAt("/s/DOCS/settings?tab=reviews");
    const whole = (await screen.findByText("The whole space")).closest("[data-space-review-rule]") as HTMLElement;
    await userEvent.click(within(whole).getByRole("button", { name: "Remove rule" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === "/spaces/DOCS/review-rule")).toBe(true));
  });

  it("is no tab for a member who does not administer the space", async () => {
    stubSettings(notAdmin);
    await renderAt("/s/DOCS/settings?tab=reviews");
    expect(await screen.findByRole("tab", { name: "Details" })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("tab", { name: "Reviews" })).toBeNull();
  });
});
