import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { CONTRIBUTORS_DEFAULT_LIMIT, CONTRIBUTORS_MAX_LIMIT, CONTRIBUTOR_SCOPES } from "@/config";
import { DocPageContext } from "@/features/editor/BlockViews";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { allowlist } from "@/test/allowlist";
import { Contributors } from "./Contributors";
import { ContributorsDialog } from "./ContributorsDialog";
import { contributorsSettings } from "./contributors";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const guide = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a20";

function shown(children: ReactNode, page: { id: string; spaceKey: string } | null = { id: guide, spaceKey: "TEAM" }) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Guide</h1>
        <DocPageContext value={page}>{children}</DocPageContext>
      </main>
    </QueryClientProvider>,
  );
}

describe("contributors settings", () => {
  it("put right what the server would refuse, as the allowlist says", () => {
    expect(contributorsSettings({ scope: "space", limit: CONTRIBUTORS_MAX_LIMIT + 1 })).toEqual({ scope: "page", limit: CONTRIBUTORS_DEFAULT_LIMIT });
    expect(contributorsSettings({ scope: "tree", limit: 5 })).toEqual({ scope: "tree", limit: 5 });
    const attrs = allowlist.nodes.contributors?.attrs;
    expect(attrs?.scope?.enum).toEqual([...CONTRIBUTOR_SCOPES]);
    expect(attrs?.limit?.max).toBe(CONTRIBUTORS_MAX_LIMIT);
  });
});

describe("the contributors block", () => {
  it("names who published the page and its tree, with their versions and last day", async () => {
    const asked: string[] = [];
    stubApi({
      [`GET /pages/${guide}/contributors`]: (request) => {
        asked.push(new URL(request.url).search);
        return {
          status: 200,
          body: {
            contributors: [
              { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", name: "Ada Lovelace", edits: 4, lastEditedAt: "2026-10-01T09:00:00Z" },
              {
                id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a02",
                name: "Grace Hopper",
                avatarUrl: "https://example.test/grace.png",
                edits: 1,
                lastEditedAt: "2026-09-20T09:00:00Z",
              },
            ],
            truncated: true,
          },
        };
      },
    });
    shown(<Contributors settings={{ scope: "tree", limit: 2 }} />);
    const block = await screen.findByRole("region", { name: "Contributors to this page and the pages below it" });
    const rows = await within(block).findAllByRole("listitem");
    expect(asked[0]).toBe("?scope=tree&limit=2");
    expect(rows.map((li) => li.getAttribute("data-contributor"))).toEqual(["Ada Lovelace", "Grace Hopper"]);
    expect(rows[0]).toHaveTextContent(/Ada Lovelace.*4 versions, the last on/);
    expect(rows[1]).toHaveTextContent(/1 version, the last on/);
    expect(within(rows[1]!).getByRole("img", { name: "Grace Hopper" })).toBeInTheDocument();
    expect(within(block).getByText("Showing the first 2 people. Raise the number in the list's settings to see more.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says when nobody published yet, and asks nothing before the page is saved", async () => {
    stubApi({ [`GET /pages/${guide}/contributors`]: { status: 200, body: { contributors: [], truncated: false } } });
    shown(<Contributors settings={contributorsSettings({})} />);
    expect(await screen.findByText("Nobody has published a version here yet.")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Contributors to this page" })).toHaveAttribute("data-state", "empty");
    cleanup();
    const sent = stubApi({});
    shown(<Contributors settings={contributorsSettings({})} />, null);
    expect(screen.getByText("Save the page once to list who published it.")).toBeInTheDocument();
    expect(sent).toEqual([]);
  });
});

describe("the contributors dialog", () => {
  it("asks for this page or its tree, and how many people", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    shown(<ContributorsDialog initial={contributorsSettings({})} onSave={onSave} onClose={() => {}} />);
    const dialog = await screen.findByRole("dialog", { name: "Contributors" });
    await user.selectOptions(within(dialog).getByLabelText("Count"), "tree");
    await user.selectOptions(within(dialog).getByLabelText("Show"), "20");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(onSave).toHaveBeenCalledWith({ scope: "tree", limit: 20 });
  });
});
