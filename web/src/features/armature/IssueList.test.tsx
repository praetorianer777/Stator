import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ArmatureAccount, ArmatureIssue } from "@/api/armature";
import { ARMATURE_COLUMNS } from "@/config";
import { DocPageContext } from "@/features/editor/BlockViews";
import { DocDiffView, DocView } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";
import type { IssueSource } from "@/features/editor/armatureIssue";
import type { Doc } from "@/features/editor/schema";
import { allowlist } from "@/test/allowlist";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { sortRows } from "./IssueList";
import { checkSettings } from "./IssueListDialog";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const BASE = "https://armature.example.com";

const issue = (n: number, over: Partial<ArmatureIssue> = {}): ArmatureIssue => ({
  key: `CP-${n}`,
  url: `${BASE}/issues/CP-${n}`,
  projectKey: "CP",
  summary: `Issue number ${n}`,
  type: {
    id: "0195f000-0000-7000-8000-000000000001",
    name: "Task",
    icon: "task",
    level: 0,
  },
  status: { name: "To do", category: "todo" },
  priority: "medium",
  assignee: null,
  reporter: null,
  dueDate: null,
  createdAt: `2026-09-${String(n).padStart(2, "0")}T10:00:00Z`,
  updatedAt: `2026-09-${String(n).padStart(2, "0")}T10:00:00Z`,
  ...over,
});

const account = (over: Partial<ArmatureAccount> = {}): Answer => ({
  status: 200,
  body: {
    account: {
      configured: true,
      baseUrl: BASE,
      connected: true,
      status: "ok",
      user: null,
      checkedAt: null,
      ...over,
    },
  },
});

const QUERY = "project = CP ORDER BY key";

/** A search answering pages of the given issues, as Armature would. */
function searchOf(all: ArmatureIssue[], asked: URLSearchParams[] = []) {
  return (request: Request): Answer => {
    const params = new URL(request.url).searchParams;
    asked.push(params);
    const limit = Number(params.get("limit"));
    const offset = Number(params.get("offset") ?? 0);
    return {
      status: 200,
      body: {
        status: "ok",
        issues: all.slice(offset, offset + limit),
        total: all.length,
        limit,
        offset,
        url: `${BASE}/search?q=x`,
      },
    };
  };
}

const listDoc = (query: string, columns: string[], limit: number): Doc => ({
  type: "doc",
  content: [{ type: "armatureIssueList", attrs: { query, columns, limit } }],
});

function wrap(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <main>
        <h1>Page</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

const list = () => document.querySelector<HTMLElement>("[data-armature-issue-list]");
const rowKeys = () => Array.from(document.querySelectorAll<HTMLElement>("[data-issue-row]")).map((row) => row.dataset.issueRow);

describe("the issue list block in the read view", () => {
  it("shows the chosen columns of the rows the viewer may see, the count and a link to the query", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/search": searchOf([issue(1, { dueDate: "2026-10-15T00:00:00Z" }), issue(2)]),
    });
    wrap(<DocView doc={listDoc(QUERY, ["key", "summary", "due"], 20)} />);
    const table = await screen.findByRole("table", {
      name: `Armature issues matching ${QUERY}`,
    });
    expect(
      within(table)
        .getAllByRole("columnheader")
        .map((th) => th.textContent),
    ).toEqual(["Key", "Summary", "Due"]);
    expect(rowKeys()).toEqual(["CP-1", "CP-2"]);
    expect(within(table).getByRole("link", { name: "CP-1" })).toHaveAttribute("href", `${BASE}/issues/CP-1`);
    expect(table).toHaveTextContent(/Oct 15, 2026|15 Oct 2026/);
    expect(list()).toHaveTextContent("Showing 2 of 2");
    expect(screen.getByRole("link", { name: "Open in Armature" })).toHaveAttribute("href", `${BASE}/search?q=x`);
    expect(await axeViolations()).toEqual([]);
  });

  it("sorts the rows it has by a column head, from the keyboard too", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/search": searchOf([issue(2, { priority: "low" }), issue(10, { priority: "highest" }), issue(1, { priority: "high" })]),
    });
    wrap(<DocView doc={listDoc(QUERY, ["key", "priority"], 20)} />);
    await screen.findByRole("table");
    expect(rowKeys()).toEqual(["CP-2", "CP-10", "CP-1"]);
    const user = userEvent.setup();
    const byKey = screen.getByRole("button", { name: "Key" });
    await user.click(byKey);
    expect(rowKeys()).toEqual(["CP-1", "CP-2", "CP-10"]);
    expect(byKey.closest("th")).toHaveAttribute("aria-sort", "ascending");
    byKey.focus();
    await user.keyboard("{Enter}");
    expect(rowKeys()).toEqual(["CP-10", "CP-2", "CP-1"]);
    expect(byKey.closest("th")).toHaveAttribute("aria-sort", "descending");
    await user.click(screen.getByRole("button", { name: "Priority" }));
    expect(rowKeys()).toEqual(["CP-2", "CP-1", "CP-10"]);
  });

  it("asks for more rows a page at a time, never past the block's limit", async () => {
    const asked: URLSearchParams[] = [];
    const all = Array.from({ length: 30 }, (_, i) => issue(i + 1));
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/search": searchOf(all, asked),
    });
    wrap(<DocView doc={listDoc(QUERY, ["key"], 25)} />);
    await screen.findByRole("table");
    expect(rowKeys()).toHaveLength(20);
    expect(list()).toHaveTextContent("Showing 20 of 30");
    await userEvent.setup().click(screen.getByRole("button", { name: "Show more" }));
    await waitFor(() => expect(rowKeys()).toHaveLength(25));
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
    expect(asked.map((p) => [p.get("q"), p.get("limit"), p.get("offset")])).toEqual([
      [QUERY, "20", "0"],
      [QUERY, "5", "20"],
    ]);
  });

  it("says so when nothing matches", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/search": searchOf([]),
    });
    wrap(<DocView doc={listDoc(QUERY, ["key"], 20)} />);
    await waitFor(() => expect(list()).toHaveAttribute("data-state", "empty"));
    expect(list()).toHaveTextContent("No issues match this query that you can see in Armature.");
  });

  it("explains a query Armature cannot read, and shows an author where", async () => {
    const bad: Answer = {
      status: 422,
      body: {
        error: {
          code: "bad_query",
          message: '"~" cannot be used here.',
          position: 26,
        },
      },
    };
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/search": bad,
    });
    const onEdit = vi.fn();
    wrap(
      <DocPageContext
        value={{
          id: "0195f000-0000-7000-8000-0000000000b1",
          spaceKey: "DOCS",
          onEdit,
        }}
      >
        <DocView doc={listDoc('project = CP AND summary ~ "keys"', ["key"], 20)} />
      </DocPageContext>,
    );
    await waitFor(() => expect(list()).toHaveAttribute("data-state", "bad_query"));
    expect(list()).toHaveTextContent('Armature cannot read this query at character 26: "~" cannot be used here. Edit the query to fix it.');
    expect(list()?.querySelector("[data-query-position='26']")).toHaveTextContent("~");
    await userEvent.setup().click(screen.getByRole("button", { name: "Edit list" }));
    expect(onEdit).toHaveBeenCalled();
    expect(await axeViolations()).toEqual([]);
  });

  it("shows a reader who may not edit the sentence alone", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/search": {
        status: 422,
        body: { error: { code: "bad_query", message: "Bad.", position: 3 } },
      },
    });
    wrap(<DocView doc={listDoc("pr", ["key"], 20)} />);
    await waitFor(() => expect(list()).toHaveAttribute("data-state", "bad_query"));
    expect(list()?.querySelector("[data-marked-query]")).toBeNull();
    expect(screen.queryByRole("button", { name: "Edit list" })).toBeNull();
  });

  it("asks nothing and shows the way to connect to a reader without a token", async () => {
    const sent = stubApi({
      "GET /armature/account": account({
        connected: false,
        status: "not_connected",
      }),
    });
    wrap(<DocView doc={listDoc(QUERY, ["key"], 20)} />);
    await waitFor(() => expect(list()).toHaveAttribute("data-state", "connect"));
    expect(screen.getByRole("link", { name: "Connect your account" })).toHaveAttribute("href", "/settings/profile#armature");
    expect(sent.some((request) => request.path === "/armature/search")).toBe(false);
  });

  it("says when Armature does not answer", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/search": {
        status: 200,
        body: {
          status: "unreachable",
          issues: [],
          total: 0,
          limit: 20,
          offset: 0,
          url: "",
        },
      },
    });
    wrap(<DocView doc={listDoc(QUERY, ["key"], 20)} />);
    await waitFor(() => expect(list()).toHaveAttribute("data-state", "unreachable"));
  });

  it("is described by its query in a comparison of versions, and asks nothing", async () => {
    const sent = stubApi({ "GET /armature/account": account() });
    wrap(
      <DocDiffView
        blocks={[
          {
            change: "inserted",
            node: {
              type: "armatureIssueList",
              attrs: { query: QUERY, columns: ["key"], limit: 20 },
            },
          },
        ]}
      />,
    );
    expect(await screen.findByText(`Armature issue list: ${QUERY}`)).toBeInTheDocument();
    expect(sent.some((request) => request.path === "/armature/search")).toBe(false);
  });
});

describe("the list's settings", () => {
  it("offer the server's columns, in its order", () => {
    expect(allowlist.nodes.armatureIssueList?.attrs?.columns?.enum).toEqual([...ARMATURE_COLUMNS]);
  });

  it("are refused with a sentence for each field that is wrong", () => {
    const wrong = checkSettings(" ", [], "0");
    expect("problems" in wrong && Object.keys(wrong.problems).sort()).toEqual(["columns", "limit", "query"]);
    expect(checkSettings("q", ["key"], "101")).toHaveProperty("problems.limit");
    expect(checkSettings("q", ["key"], "2.5")).toHaveProperty("problems.limit");
    expect(checkSettings("q".repeat(2001), ["key"], "5")).toHaveProperty("problems.query");
    expect(checkSettings("q", ["due", "key"], "5")).toEqual({
      settings: { query: "q", columns: ["key", "due"], limit: 5 },
    });
  });

  it("order rows with empty values last either way", () => {
    const rows = [issue(1, { dueDate: null }), issue(2, { dueDate: "2026-10-02T00:00:00Z" }), issue(3, { dueDate: "2026-10-01T00:00:00Z" })];
    expect(sortRows(rows, { column: "due", ascending: true }).map((r) => r.key)).toEqual(["CP-3", "CP-2", "CP-1"]);
    expect(sortRows(rows, { column: "due", ascending: false }).map((r) => r.key)).toEqual(["CP-2", "CP-3", "CP-1"]);
  });
});

const source: IssueSource = { baseUrl: () => BASE, knowsProject: () => true };

describe("inserting an issue list from the slash menu", () => {
  it("checks the query as it is typed, takes columns and a limit, and stores only the settings", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/search": (request) => {
        const q = new URL(request.url).searchParams.get("q") ?? "";
        if (q.includes("~"))
          return {
            status: 422,
            body: {
              error: {
                code: "bad_query",
                message: '"~" cannot be used here.',
                position: 14,
              },
            },
          };
        return searchOf([issue(1), issue(2), issue(3)])(request);
      },
    });
    const onChange = vi.fn();
    wrap(<Editor id="page-body" value={null} onChange={onChange} armature={source} />);
    const user = userEvent.setup();
    const box = document.getElementById("page-body")!;
    await user.click(box);
    await user.type(box, "/nql");
    await waitFor(() => expect(within(screen.getByRole("listbox", { name: "Insert a block" })).getAllByRole("option")).toHaveLength(1));
    await user.keyboard("{Enter}");

    const dialog = await screen.findByRole("dialog", {
      name: "Insert an Armature issue list",
    });
    const query = within(dialog).getByRole("textbox", { name: "NQL query" });
    await waitFor(() => expect(query).toHaveFocus());
    await user.type(query, "project = CP ~");
    expect(await within(dialog).findByText(/at character 14/)).toBeInTheDocument();
    expect(dialog.querySelector("[data-query-position='14']")).toHaveTextContent("~");
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(within(dialog).getByText("Armature cannot read this query. Fix it where it is marked, then save.")).toBeInTheDocument();

    await user.clear(query);
    await user.type(query, "project = CP");
    expect(await within(dialog).findByText("The query matches 3 issues you can see.")).toBeInTheDocument();
    expect(within(dialog).getByRole("checkbox", { name: "Key" })).toBeChecked();
    await user.click(within(dialog).getByRole("checkbox", { name: "Assignee" }));
    await user.click(within(dialog).getByRole("checkbox", { name: "Due" }));
    const limit = within(dialog).getByRole("spinbutton", { name: "Most rows" });
    await user.clear(limit);
    await user.type(limit, "2");
    expect(await axeViolations()).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const doc = onChange.mock.calls.at(-1)?.[0] as Doc;
    const node = doc.content?.find((n) => n.type === "armatureIssueList");
    expect(node?.attrs).toEqual({
      query: "project = CP",
      columns: ["key", "summary", "status", "due"],
      limit: 2,
    });
    expect(JSON.stringify(doc)).not.toContain("Issue number");
    await waitFor(() => expect(rowKeys()).toEqual(["CP-1", "CP-2"]));
  });
});
