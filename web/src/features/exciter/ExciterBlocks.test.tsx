import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ExciterAccount, ExciterRequirementDetail, ExciterRow } from "@/api/exciter";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { ExciterDocument } from "./ExciterDocument";
import { requirementListSettings } from "./RequirementList";
import { completeWith, RequirementListDialog } from "./RequirementListDialog";
import { RequirementPicker } from "./RequirementPicker";
import { requirementKeysOf, requirementUrl } from "./requirementKeys";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const BASE = "https://exciter.example.com";

const account = (over: Partial<ExciterAccount> = {}): Answer => ({
  status: 200,
  body: { account: { configured: true, baseUrl: BASE, connected: true, status: "ok", user: null, checkedAt: null, missing: [], ...over } },
});

const text = (t: string, marks?: unknown[]) => ({ type: "text", text: t, ...(marks ? { marks } : {}) });
const para = (...content: unknown[]) => ({ type: "paragraph", content });

const STATEMENT = {
  type: "doc",
  content: [
    { type: "heading", attrs: { level: 2 }, content: [text("Keeping the basket")] },
    para(
      text("The "),
      text("basket", [{ type: "term", attrs: { termId: "x" } }]),
      text(" keeps its items for "),
      text("30 days", [{ type: "bold" }]),
      text(", as the "),
      text("shop", [{ type: "link", attrs: { href: "https://shop.example.test/basket" } }]),
      text(" and "),
      text("this", [{ type: "link", attrs: { href: "javascript:alert(1)" } }]),
      text(" say."),
    ),
    { type: "bulletList", content: [{ type: "listItem", content: [para(text("Signed in"))] }] },
    { type: "image", attrs: { attachmentId: "x", alt: "The basket after a month" } },
    { type: "callout", content: [para(text("A node Stator does not know."))] },
  ],
};

const detail = (key: string, over: Partial<ExciterRequirementDetail> = {}): ExciterRequirementDetail => ({
  id: "0195f000-0000-7000-8000-0000000000a1",
  key,
  url: requirementUrl(BASE, key),
  projectKey: "CHK",
  title: "The basket keeps its items for 30 days",
  status: { name: "Approved", category: "approved" },
  revision: 3,
  trashedAt: null,
  statement: STATEMENT,
  rationale: { type: "doc", content: [para(text("Most customers come back within a month."))] },
  ...over,
});

const row = (key: string, title: string): ExciterRow => ({
  id: "0195f000-0000-7000-8000-0000000000a2",
  key,
  url: requirementUrl(BASE, key),
  projectKey: "CHK",
  title,
  status: { name: "Draft", category: "draft" },
  revision: 1,
  trashed: false,
});

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <main>
        <h1>Page</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

const docOf = (...content: unknown[]): Doc => ({ type: "doc", content }) as Doc;

describe("a statement in Exciter's schema", () => {
  it("draws what Stator knows, opens only safe links and shows the rest as its words", () => {
    wrap(<ExciterDocument doc={STATEMENT} />);
    expect(screen.getByText("30 days").tagName).toBe("STRONG");
    expect(screen.getByRole("link", { name: "shop" })).toHaveAttribute("href", "https://shop.example.test/basket");
    expect(screen.queryByRole("link", { name: "this" })).toBeNull();
    expect(screen.getByText("this")).toBeInTheDocument();
    expect(screen.getByRole("listitem")).toHaveTextContent("Signed in");
    // A heading of the requirement's takes no place in the page's outline.
    expect(screen.getAllByRole("heading")).toHaveLength(1);
    expect(screen.getByText("Keeping the basket").closest("p")).toHaveAttribute("data-exciter-heading", "2");
    expect(screen.getByText("A picture in Exciter: The basket after a month")).toBeInTheDocument();
    expect(document.querySelector("[data-exciter-unknown='callout']")).toHaveTextContent("A node Stator does not know.");
  });

  it("draws no links in the editor", () => {
    wrap(<ExciterDocument doc={STATEMENT} links={false} />);
    expect(screen.queryByRole("link")).toBeNull();
  });
});

describe("the requirement block", () => {
  it("shows the requirement with its statement and rationale as the viewer may see it", async () => {
    stubApi({
      "GET /exciter/account": account(),
      "GET /exciter/requirements": { status: 200, body: { status: "ok", requirements: [{ key: "CHK-R1", requirement: detail("CHK-R1") }] } },
      "GET /exciter/requirements/CHK-R1": { status: 200, body: { status: "ok", requirement: detail("CHK-R1") } },
    });
    wrap(<DocView doc={docOf({ type: "exciterRequirementBlock", attrs: { key: "CHK-R1" } })} />);
    const block = await screen.findByRole("group", { name: "Exciter requirement CHK-R1" });
    expect(await within(block).findByRole("region", { name: "Statement" })).toHaveTextContent("keeps its items for 30 days");
    expect(within(block).getByRole("region", { name: "Rationale" })).toHaveTextContent("Most customers come back within a month.");
    expect(within(block).getByText("Approved")).toBeInTheDocument();
    expect(within(block).getByRole("link", { name: "Open in Exciter CHK-R1" })).toHaveAttribute("href", `${BASE}/p/CHK/r/CHK-R1`);
    expect(await axeViolations()).toEqual([]);
  });

  it("is the key and why to a viewer who may not see the requirement", async () => {
    const sent = stubApi({
      "GET /exciter/account": account(),
      "GET /exciter/requirements": { status: 200, body: { status: "ok", requirements: [{ key: "CHK-R2", requirement: null }] } },
    });
    wrap(<DocView doc={docOf({ type: "exciterRequirementBlock", attrs: { key: "CHK-R2" } })} />);
    const block = await screen.findByRole("group", { name: "Exciter requirement CHK-R2" });
    await waitFor(() => expect(block).toHaveAttribute("data-state", "hidden"));
    expect(block).toHaveTextContent("You may not see CHK-R2 in Exciter");
    expect(sent.some((r) => r.path === "/exciter/requirements/CHK-R2")).toBe(false);
  });

  it("names its key among a document's requirements, so the chips' lookup reads it too", () => {
    expect(requirementKeysOf(docOf({ type: "exciterRequirementBlock", attrs: { key: "chk-r9" } }))).toEqual(["CHK-R9"]);
  });
});

describe("the requirement list", () => {
  const listDoc = (project: string, query: string, limit: number) => docOf({ type: "exciterRequirementList", attrs: { project, query, limit } });

  it("shows the rows a window at a time, as the viewer may see them", async () => {
    const all = [row("CHK-R1", "The basket"), row("CHK-R2", "Payment providers"), row("CHK-R4", "An order is confirmed")];
    const sent = stubApi({
      "GET /exciter/account": account(),
      "GET /exciter/projects/CHK/requirements": (request) => {
        const q = new URL(request.url).searchParams;
        const offset = Number(q.get("offset"));
        const limit = Number(q.get("limit"));
        return { status: 200, body: { status: "ok", requirements: all.slice(offset, offset + limit), total: all.length, limit, offset, found: true } };
      },
    });
    wrap(<DocView doc={listDoc("CHK", "owner = currentUser()", 3)} />);
    const table = await screen.findByRole("table", { name: "Exciter requirements in CHK matching owner = currentUser()" });
    expect(within(table).getAllByRole("row")).toHaveLength(4);
    expect(within(table).getByRole("link", { name: "CHK-R2" })).toHaveAttribute("href", `${BASE}/p/CHK/r/CHK-R2`);
    expect(screen.getByText("Showing 3 of 3")).toBeInTheDocument();
    const asked = sent.find((r) => r.path === "/exciter/projects/CHK/requirements");
    expect(asked).toBeDefined();
    expect(await axeViolations()).toEqual([]);
  });

  it("says where Exciter could not read the query", async () => {
    stubApi({
      "GET /exciter/account": account(),
      "GET /exciter/projects/CHK/requirements": {
        status: 422,
        body: { error: { code: "bad_query", message: "There is no field colour. Use key, title, status or owner.", position: 5 } },
      },
    });
    wrap(<DocView doc={listDoc("CHK", "key = colour = red", 20)} />);
    expect(await screen.findByText(/Exciter cannot read this query at character 5: There is no field colour/)).toBeInTheDocument();
    expect(document.querySelector("[data-exciter-requirement-list]")).toHaveAttribute("data-state", "bad_query");
  });

  it("tells a reader who may not view the project so", async () => {
    stubApi({
      "GET /exciter/account": account(),
      "GET /exciter/projects/SEC/requirements": { status: 200, body: { status: "ok", requirements: [], total: 0, limit: 20, offset: 0, found: false } },
    });
    wrap(<DocView doc={listDoc("SEC", "", 20)} />);
    expect(await screen.findByText(/Exciter has no project SEC that you may view/)).toBeInTheDocument();
  });

  it("asks a reader without a token to connect, and Exciter nothing", async () => {
    const sent = stubApi({ "GET /exciter/account": account({ connected: false, status: "not_connected" }) });
    wrap(<DocView doc={listDoc("CHK", "", 20)} />);
    expect(await screen.findByText(/Connect your Exciter account to see the requirements this list finds/)).toBeInTheDocument();
    expect(sent.some((r) => r.path.startsWith("/exciter/projects"))).toBe(false);
  });

  it("keeps its settings in shape", () => {
    expect(requirementListSettings({ project: "chk", query: "status = Draft", limit: 9000 })).toEqual({ project: "CHK", query: "status = Draft", limit: 20 });
    expect(requirementListSettings({ project: "CHK-R1" }).project).toBe("");
  });
});

describe("the requirement picker", () => {
  it("finds requirements by a word of the title and inserts the one picked", async () => {
    const sent = stubApi({
      "GET /exciter/account": account(),
      "GET /exciter/search": {
        status: 200,
        body: { status: "ok", requirements: [{ ...row("CHK-R1", "The basket keeps its items for 30 days"), projectName: "Checkout" }] },
      },
    });
    const onInsert = vi.fn();
    wrap(<RequirementPicker title="Insert an Exciter requirement" onInsert={onInsert} onClose={() => {}} />);
    await userEvent.type(await screen.findByRole("combobox", { name: "Find a requirement" }), "bask");
    const option = await screen.findByRole("option", { name: /CHK-R1.*The basket keeps its items/ });
    expect(screen.getByText("1 requirement found.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    fireEvent.mouseDown(option);
    expect(onInsert).toHaveBeenCalledWith("CHK-R1");
    expect(sent.find((r) => r.path === "/exciter/search")).toBeDefined();
  });

  it("asks an author without a token to connect", async () => {
    stubApi({ "GET /exciter/account": account({ connected: false, status: "not_connected" }) });
    wrap(<RequirementPicker title="Insert an Exciter requirement" onInsert={() => {}} onClose={() => {}} />);
    expect(await screen.findByText(/Connect your Exciter account to find its requirements/)).toBeInTheDocument();
  });
});

describe("the requirement list's editor", () => {
  it("writes a word over what the caret completes, counted in characters", () => {
    expect(completeWith("stat", 0, 4, "status")).toEqual({ text: "status ", caret: 7 });
    expect(completeWith("é = Ap AND x", 4, 6, "Approved")).toEqual({ text: "é = Approved AND x", caret: 12 });
  });

  it("picks the project, completes the query at the caret and saves the list", async () => {
    stubApi({
      "GET /exciter/account": account(),
      "GET /exciter/projects": {
        status: 200,
        body: {
          status: "ok",
          projects: [
            { key: "CHK", name: "Checkout" },
            { key: "SEC", name: "Security" },
          ],
        },
      },
      "GET /exciter/projects/CHK/requirements": { status: 200, body: { status: "ok", requirements: [], total: 2, limit: 1, offset: 0, found: true } },
      "GET /exciter/projects/CHK/requirements/suggest": {
        status: 200,
        body: {
          status: "ok",
          suggestions: {
            completion: { slot: "field", prefix: "sta", from: 0, to: 3, quoted: false, words: ["status"] },
            words: [{ text: "status", label: "status", detail: "The requirement's status" }],
            requirements: [],
          },
        },
      },
    });
    const onSave = vi.fn();
    wrap(<RequirementListDialog initial={{ project: "", query: "", limit: 20 }} isNew onSave={onSave} onClose={() => {}} />);
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: "Project" }), "CHK");
    const query = screen.getByLabelText("Query");
    await userEvent.type(query, "sta");
    const word = await screen.findByRole("option", { name: /status/ });
    expect(await axeViolations()).toEqual([]);
    fireEvent.mouseDown(word);
    expect(query).toHaveValue("status ");
    expect(await screen.findByText("The query finds 2 requirements you can see.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Insert" }));
    expect(onSave).toHaveBeenCalledWith({ project: "CHK", query: "status ", limit: 20 });
  });

  it("refuses to save without a project", async () => {
    stubApi({ "GET /exciter/account": account(), "GET /exciter/projects": { status: 200, body: { status: "ok", projects: [] } } });
    const onSave = vi.fn();
    wrap(<RequirementListDialog initial={{ project: "", query: "", limit: 20 }} isNew onSave={onSave} onClose={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: "Insert" }));
    expect(await screen.findByText("Choose the Exciter project to list requirements of.")).toBeInTheDocument();
    expect(onSave).not.toHaveBeenCalled();
  });
});
