import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WaitingReview } from "@/api/reviews";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const space = aSpace();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000a4";

const waiting = (id: string, title: string, over: Partial<WaitingReview["review"]> = {}): WaitingReview => ({
  spaceKey: "DOCS",
  page: { id: PAGE_ID, title },
  review: {
    id,
    pageId: PAGE_ID,
    state: "pending",
    authorId: null,
    authorName: "Bob Author",
    baseVersion: 3,
    title,
    comment: "",
    reviewers: [{ type: "user", id: "0195f000-0000-7000-8000-0000000000f1", name: "Ada Lovelace" }],
    required: 2,
    approvals: 1,
    decisions: [],
    can: { decide: true, withdraw: false, skip: false },
    submittedAt: "2026-10-01T10:00:00Z",
    finishedAt: null,
    version: null,
    skipReason: "",
    skippedByName: "",
    ...over,
  },
});

const two = [waiting("0195f000-0000-7000-8000-0000000000e5", "Policy"), waiting("0195f000-0000-7000-8000-0000000000e6", "Runbook", { approvals: 0 })];

function stubShell(reviews: WaitingReview[], more: Record<string, Answer> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    "GET /notifications/unread-count": { status: 200, body: { unread: 0 } },
    "GET /reviews/waiting": { status: 200, body: { reviews: reviews.map(({ review, page, spaceKey }) => ({ ...review, page, spaceKey })) } },
    ...more,
  });
}

describe("the reviews waiting for the caller", () => {
  it("are counted beside Reviews in the sidebar, which leads to their list", async () => {
    stubShell(two);
    const router = await renderAt("/spaces");
    const sidebar = document.querySelector<HTMLElement>("[data-sidebar]")!;
    const entry = await within(sidebar).findByRole("link", { name: "Reviews 2 reviews waiting for you" });
    expect(entry).toHaveAttribute("data-nav", "reviews");
    expect(document.querySelector("[data-waiting-badge]")).toHaveTextContent("2");
    await userEvent.click(entry);
    await waitFor(() => expect(router.state.location.pathname).toBe("/reviews"));
    await screen.findByRole("heading", { name: "Reviews waiting for you" });
    const row = (await screen.findByRole("link", { name: "Policy" })).closest("[data-waiting-review]") as HTMLElement;
    expect(row.closest("[data-waiting-reviews]")).not.toBeNull();
    expect(row).toHaveAttribute("data-waiting-review", "0195f000-0000-7000-8000-0000000000e5");
    expect(within(row).getByRole("link", { name: "Policy" })).toHaveAttribute("href", `/s/DOCS/p/${PAGE_ID}/policy`);
    expect(row).toHaveTextContent(/Bob Author, submitted/);
    expect(within(row).getByText("1 of 2 approvals")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("show no count while none wait, and the list says so", async () => {
    stubShell([]);
    await renderAt("/reviews");
    expect(await screen.findByText("No reviews are waiting for you. When somebody submits a draft you review, it shows here.")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /waiting for you/ })).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("are listed on the home page when there are any", async () => {
    stubShell(two);
    await renderAt("/");
    const list = (await screen.findByRole("region", { name: "Reviews waiting for you" })) as HTMLElement;
    expect(list).toHaveAttribute("data-home-list", "reviews");
    expect(within(list).getByRole("link", { name: "Runbook" })).toBeInTheDocument();
    expect(within(list).getByText("0 of 2 approvals")).toBeInTheDocument();
    expect(within(list).getByRole("link", { name: "All reviews waiting for you" })).toHaveAttribute("href", "/reviews");
  });

  it("are no list on the home page when none wait", async () => {
    stubShell([]);
    await renderAt("/");
    await screen.findByRole("heading", { name: "Home" });
    expect(document.querySelector('[data-home-list="reviews"]')).toBeNull();
  });
});
