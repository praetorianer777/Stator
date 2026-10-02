import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { render } from "@testing-library/react";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aSpace } from "@/test/spaces";
import { DocView } from "@/features/editor/DocView";

afterEach(() => vi.unstubAllGlobals());

const space = aSpace();
const decided = {
  pageId: "0195f000-0000-7000-8000-0000000000d1",
  pageTitle: "Release",
  text: "Ship on Tuesdays",
  state: "decided",
  updatedAt: "2026-10-01T08:00:00Z",
  updatedByName: "Ada",
};
const open = { ...decided, text: "Which region first", state: "undecided" };

describe("the decision log", () => {
  it("lists a space's decisions with their state and page, and keeps one state when asked", async () => {
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/decisions": (request) => {
        const state = new URL(request.url).searchParams.get("state");
        const all = [decided, open];
        return { status: 200, body: { decisions: state ? all.filter((d) => d.state === state) : all, truncated: false } };
      },
    });
    const router = await renderAt("/s/DOCS/decisions");
    const rows = await screen.findAllByRole("listitem");
    expect(rows.map((row) => row.textContent)).toEqual([expect.stringContaining("Ship on Tuesdays"), expect.stringContaining("Which region first")]);
    expect(within(rows[0]!).getByRole("link", { name: /on Release/ })).toHaveAttribute("href", `/s/DOCS/p/${decided.pageId}/release`);
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Undecided" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ state: "undecided" }));
    await waitFor(() => expect(screen.queryByText("Ship on Tuesdays")).toBeNull());
    expect(screen.getByText("Which region first")).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/spaces/DOCS/decisions" && r.method === "GET")).toBe(true);
  });
});

describe("a decision item in the reader's view", () => {
  it("says its state in words before its line", () => {
    render(<DocView doc={{ type: "doc", content: [{ type: "decision", attrs: { state: "decided" }, content: [{ type: "text", text: "Use Postgres" }] }] }} />);
    const item = screen.getByText("Use Postgres").closest("[data-decision]");
    expect(item).toHaveAttribute("data-decision", "decided");
    expect(item).toHaveTextContent("DecidedUse Postgres");
  });
});
