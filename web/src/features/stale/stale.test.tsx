import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { StalePage } from "@/api/stale";
import { STALE_DEFAULT_DAYS, STALE_PATH } from "@/config";
import { arrival, renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => vi.unstubAllGlobals());

const carl = "0199a000-0000-7000-8000-0000000000c1";
const runbookId = "0199a000-0000-7000-8000-0000000000a1";

const row = (over: Partial<StalePage>): StalePage => ({
  id: crypto.randomUUID(),
  title: "A page",
  spaceKey: "DOCS",
  spaceName: "Handbook",
  version: 3,
  publishedAt: "2025-01-10T09:00:00Z",
  viewedAt: null,
  activeAt: "2025-01-10T09:00:00Z",
  owner: null,
  verification: "none",
  verificationExpiresAt: null,
  ...over,
});

const first = [
  row({ id: runbookId, title: "Runbook", owner: { id: carl, name: "Carl Owner", canView: true } }),
  row({
    title: "Onboarding",
    viewedAt: "2025-02-01T09:00:00Z",
    activeAt: "2025-02-01T09:00:00Z",
    verification: "expired",
    verificationExpiresAt: "2025-03-01T09:00:00Z",
    owner: { id: "0199a000-0000-7000-8000-0000000000c2", name: "Dana Gone", canView: false },
  }),
];
const second = [row({ title: "Glossary", verification: "verified", verificationExpiresAt: "2027-01-01T09:00:00Z" })];

/** The API's report, a page of two then a page of one, recording each query it was asked. */
function report(extra: Record<string, Answer | ((request: Request) => Answer)> = {}) {
  const queries: URLSearchParams[] = [];
  const sent = stubApi({
    "GET /spaces": { status: 200, body: { spaces: [aSpace(), aSpace({ key: "OPS", name: "Operations", can: { ...aSpace().can, administer: false } })] } },
    "GET /stale-pages": (request) => {
      const query = new URL(request.url).searchParams;
      queries.push(query);
      return query.get("cursor") === "page-2" ? { status: 200, body: { pages: second, next: null } } : { status: 200, body: { pages: first, next: "page-2" } };
    },
    ...extra,
  });
  return { queries, sent };
}

describe("the stale content report", () => {
  it("lists the longest untouched pages with when, who owns them and whether they are verified", async () => {
    const { queries } = report();
    await renderAt(STALE_PATH);
    const table = await screen.findByRole("table");
    const rows = within(table).getAllByRole("row");
    expect(rows).toHaveLength(first.length + 1);
    expect(rows[1]).toHaveTextContent("Runbook");
    expect(rows[1]).toHaveTextContent("Handbook");
    expect(rows[1]).toHaveTextContent("Never");
    expect(rows[1]).toHaveTextContent("Carl Owner");
    expect(rows[1]).toHaveTextContent("Not verified");
    expect(rows[2]).toHaveTextContent("Expired");
    expect(rows[2]).toHaveTextContent("no longer has access");
    expect(
      screen.getByText(`Published pages nobody has published again or opened for at least ${STALE_DEFAULT_DAYS} days`, { exact: false }),
    ).toBeInTheDocument();
    expect(queries[0]?.get("olderThan")).toBe(String(STALE_DEFAULT_DAYS));
    expect(await axeViolations()).toEqual([]);
  });

  it("offers only the spaces the reader administers, and narrows by space, period, owner and verification", async () => {
    const { queries } = report();
    await renderAt(STALE_PATH);
    await screen.findByRole("table");
    const spaces = within(screen.getByLabelText("Space"))
      .getAllByRole("option")
      .map((option) => option.textContent);
    expect(spaces).toEqual(["Every space you administer", "Handbook"]);
    await userEvent.selectOptions(screen.getByLabelText("Space"), "DOCS");
    await waitFor(() => expect(queries.at(-1)?.get("space")).toBe("DOCS"));
    await userEvent.selectOptions(screen.getByLabelText("Untouched for"), "365");
    await waitFor(() => expect(queries.at(-1)?.get("olderThan")).toBe("365"));
    await userEvent.selectOptions(screen.getByLabelText("Verification"), "expired");
    await waitFor(() => expect(queries.at(-1)?.get("verification")).toBe("expired"));
    await userEvent.selectOptions(screen.getByLabelText("Owner"), "none");
    await waitFor(() => expect(queries.at(-1)?.get("owner")).toBe("none"));

    await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(screen.getByLabelText("Space")).toHaveValue("");
    expect(screen.getByLabelText("Untouched for")).toHaveValue(String(STALE_DEFAULT_DAYS));
    expect(screen.getByLabelText("Owner")).toHaveValue("");
    expect(screen.getByLabelText("Verification")).toHaveValue("");
    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();
  });

  it("narrows to an owner when their name is followed", async () => {
    const { queries } = report();
    await renderAt(STALE_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Show only pages Carl Owner owns" }));
    await waitFor(() => expect(queries.at(-1)?.get("owner")).toBe(carl));
    expect(screen.getByLabelText("Owner")).toHaveValue(carl);
    expect(within(screen.getByLabelText("Owner")).getByRole("option", { name: "Carl Owner" })).toBeInTheDocument();
  });

  it("starts in the space a space's settings link to", async () => {
    const { queries } = report();
    await renderAt(`${STALE_PATH}?space=docs`);
    await screen.findByRole("table");
    expect(queries[0]?.get("space")).toBe("DOCS");
    expect(screen.getByLabelText("Space")).toHaveValue("DOCS");
  });

  it("pages on and back by the cursor the API handed out", async () => {
    const { queries } = report();
    await renderAt(STALE_PATH);
    await screen.findByRole("table");
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(queries.at(-1)?.get("cursor")).toBe("page-2"));
    await waitFor(() => expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(second.length + 1));
    expect(screen.getByText("Page 2")).toBeInTheDocument();
    expect(within(screen.getByRole("table")).getByText("Verified")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Previous" }));
    expect(await screen.findByText("Page 1")).toBeInTheDocument();
  });

  it("says when nothing went stale", async () => {
    report({ "GET /stale-pages": { status: 200, body: { pages: [], next: null } } });
    await renderAt(STALE_PATH);
    expect(await screen.findByText("No stale pages")).toBeInTheDocument();
    expect(screen.getByText("Every page here was published or opened within the period.")).toBeInTheDocument();
  });

  it("tells a member who administers nothing whom to ask", async () => {
    report({
      "GET /stale-pages": {
        status: 403,
        body: { error: { code: "forbidden", message: "Only administrators of a space, or of the organization, can read which of its pages went stale." } },
      },
    });
    await renderAt(STALE_PATH, { me: { ...signedIn, organization: { ...signedIn.organization!, role: "member" } } });
    expect(await screen.findByText(/Only administrators of a space, or of the organization, read which pages went stale/)).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("opens a page to review it without counting the review as a view", async () => {
    const space = aSpace();
    const home = aPage();
    const page = aPage({ id: runbookId, title: "Runbook", home: false, parentId: home.id, ancestors: [{ id: home.id, title: "Handbook", home: true }] });
    const { sent } = report({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/outline": { status: 200, body: { pages: [] } },
      [`GET /pages/${runbookId}`]: { status: 200, body: { page, space } },
    });
    const router = await renderAt(STALE_PATH);
    const link = await screen.findByRole("link", { name: "Runbook" });
    expect(link.getAttribute("href")).toBe(`/s/DOCS/p/${runbookId}/runbook?from=stale`);
    await userEvent.click(link);
    await arrival(router, `/s/DOCS/p/${runbookId}/runbook`);
    expect(await screen.findByRole("heading", { name: "Runbook", level: 1 })).toBeInTheDocument();
    expect(sent.some((each) => each.method === "POST" && each.path.endsWith("/visit"))).toBe(false);
  });
});
