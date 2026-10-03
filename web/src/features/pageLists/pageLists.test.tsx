import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { allowlist } from "@/test/allowlist";
import { PAGE_LIST_MATCHES, PAGE_LIST_MAX_LIMIT, PAGE_LIST_SORTS } from "@/config";
import { LabelledPages, UpdatedPages } from "./PageLists";
import { PageListDialog, checkLabels } from "./PageListDialog";
import { labelledSettings, updatedSettings } from "./lists";

afterEach(cleanup);

function shown(children: ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Overview</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

const labelled = (title: string, at: string) => ({
  id: `0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a${String(title.length).padStart(2, "0")}`,
  title,
  spaceKey: "DOCS",
  spaceName: "Docs",
  path: [],
  labels: ["howto"],
  unpublished: false,
  updatedByName: "",
  updatedAt: at,
});

describe("list settings", () => {
  it("put right what the server would refuse, as the allowlist says", () => {
    expect(labelledSettings({ labels: ["HowTo", "a/b"], match: "some", space: "docs", sort: "views", limit: 0 })).toEqual({
      labels: ["howto"],
      match: "all",
      space: null,
      sort: "updated",
      limit: 10,
    });
    expect(updatedSettings({ space: "DOCS", limit: PAGE_LIST_MAX_LIMIT })).toEqual({ space: "DOCS", limit: PAGE_LIST_MAX_LIMIT });
    expect(allowlist.nodes.labelledPages?.attrs?.match?.enum).toEqual([...PAGE_LIST_MATCHES]);
    expect(allowlist.nodes.labelledPages?.attrs?.sort?.enum).toEqual([...PAGE_LIST_SORTS]);
    expect(allowlist.nodes.recentlyUpdated?.attrs?.limit?.max).toBe(PAGE_LIST_MAX_LIMIT);
    expect(checkLabels(" HowTo, release notes, howto ")).toEqual({ labels: ["howto", "release-notes"] });
    expect(checkLabels(" , ")).toEqual({ problem: "Name at least one label to list pages by." });
    expect("problem" in checkLabels("a/b")).toBe(true);
  });
});

describe("the lists", () => {
  it("list the pages carrying the labels, with where they live and when they were published", async () => {
    const asked: URLSearchParams[] = [];
    stubApi({
      "GET /labelled-pages": (request) => {
        asked.push(new URL(request.url).searchParams);
        return { status: 200, body: { pages: [labelled("Guide", "2026-10-02T09:00:00Z"), labelled("Runbook", "2026-09-30T09:00:00Z")] } };
      },
    });
    shown(<LabelledPages settings={{ labels: ["howto", "ops"], match: "any", space: "DOCS", sort: "title", limit: 5 }} inEditor />);
    const list = await screen.findByRole("region", { name: "Pages labelled howto or ops in DOCS" });
    await within(list).findAllByRole("listitem");
    expect(String(asked[0])).toBe("label=howto&label=ops&match=any&space=DOCS&sort=title&limit=5");
    expect(
      within(list)
        .getAllByRole("listitem")
        .map((li) => li.getAttribute("data-listed-page")),
    ).toEqual(["Guide", "Runbook"]);
    expect(within(list).getAllByRole("listitem")[0]).toHaveTextContent(/Docs · Oct 2, 2026/);
    expect(await axeViolations()).toEqual([]);
  });

  it("list what was published last with who published it, and say when there is nothing", async () => {
    stubApi({
      "GET /updated-pages": {
        status: 200,
        body: {
          pages: [
            {
              id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01",
              title: "Guide",
              spaceKey: "DOCS",
              spaceName: "Docs",
              publishedAt: "2026-10-02T09:00:00Z",
              authorName: "Ada",
            },
          ],
        },
      },
    });
    shown(<UpdatedPages settings={{ space: null, limit: 10 }} inEditor />);
    const list = await screen.findByRole("region", { name: "Recently updated" });
    expect(await within(list).findByRole("listitem")).toHaveTextContent(/Guide.*Docs · by Ada · Oct 2, 2026/);
    cleanup();
    stubApi({ "GET /updated-pages": { status: 200, body: { pages: [] } } });
    shown(<UpdatedPages settings={{ space: "DOCS", limit: 10 }} inEditor />);
    expect(await screen.findByText("Nothing you can read has been published yet.")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Recently updated in DOCS" })).toHaveAttribute("data-state", "empty");
  });
});

describe("the list dialog", () => {
  it("asks content by label for its labels, how they match, a space, an order and a length", async () => {
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [{ key: "DOCS", name: "Docs" }] } } });
    const user = userEvent.setup();
    const onSave = vi.fn();
    shown(<PageListDialog kind="labelled" initial={labelledSettings({})} isNew onSave={onSave} onClose={() => {}} />);
    const dialog = await screen.findByRole("dialog", { name: "Insert a list of pages by label" });
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(within(dialog).getByText("Name at least one label to list pages by.")).toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Labels"), "HowTo, ops");
    await user.click(within(dialog).getByLabelText("Any of the labels"));
    await within(dialog).findByRole("option", { name: "Docs (DOCS)" });
    await user.selectOptions(within(dialog).getByLabelText("Space"), "DOCS");
    await user.selectOptions(within(dialog).getByLabelText("Order"), "title");
    await user.selectOptions(within(dialog).getByLabelText("Show"), "20");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(onSave).toHaveBeenCalledWith({ labels: ["howto", "ops"], match: "any", space: "DOCS", sort: "title", limit: 20 });
  });

  it("asks recently updated for a space and a length alone", async () => {
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [] } } });
    const user = userEvent.setup();
    const onSave = vi.fn();
    shown(<PageListDialog kind="updated" initial={{ space: null, limit: 10 }} isNew={false} onSave={onSave} onClose={() => {}} />);
    const dialog = await screen.findByRole("dialog", { name: "Edit the list of recently updated pages" });
    expect(within(dialog).queryByLabelText("Labels")).toBeNull();
    await user.selectOptions(within(dialog).getByLabelText("Show"), "5");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(onSave).toHaveBeenCalledWith({ space: null, limit: 5 }));
  });
});
