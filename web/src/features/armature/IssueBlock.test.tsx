import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ArmatureAccount, ArmatureIssue } from "@/api/armature";
import { DocDiffView, DocView } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";
import type { IssueSource } from "@/features/editor/armatureIssue";
import type { Doc } from "@/features/editor/schema";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { pickedKey } from "./IssuePicker";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const BASE = "https://armature.example.com";

const cp4: ArmatureIssue = {
  key: "CP-4",
  url: `${BASE}/issues/CP-4`,
  projectKey: "CP",
  summary: "Rotate the signing keys",
  type: { id: "0195f000-0000-7000-8000-000000000001", name: "Task", icon: "task", level: 0 },
  status: { name: "To do", category: "todo" },
  priority: "highest",
  assignee: { id: "0195f000-0000-7000-8000-000000000002", name: "Alice" },
  reporter: { id: "0195f000-0000-7000-8000-000000000003", name: "Bob" },
  dueDate: "2026-10-15T00:00:00Z",
  createdAt: "2026-09-01T10:00:00Z",
  updatedAt: "2026-09-02T10:00:00Z",
};

const account = (over: Partial<ArmatureAccount> = {}): Answer => ({
  status: 200,
  body: { account: { configured: true, baseUrl: BASE, connected: true, status: "ok", user: null, checkedAt: null, ...over } },
});

const lookup = (...found: [string, ArmatureIssue | null][]): Answer => ({
  status: 200,
  body: { status: "ok", issues: found.map(([key, issue]) => ({ key, issue })) },
});

const blockDoc = (...keys: string[]): Doc => ({
  type: "doc",
  content: [{ type: "paragraph", content: [{ type: "text", text: "Tracked in" }] }, ...keys.map((key) => ({ type: "armatureIssueBlock", attrs: { key } }))],
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

const block = (key: string) => document.querySelector<HTMLElement>(`[data-armature-issue-block="${key}"]`);

describe("the Armature issue block in the read view", () => {
  it("shows every detail of an issue the viewer may see, with a link to it", async () => {
    stubApi({ "GET /armature/account": account(), "GET /armature/issues": lookup(["CP-4", cp4]) });
    wrap(<DocView doc={blockDoc("CP-4")} />);
    const card = await screen.findByRole("group", { name: "Armature issue CP-4" });
    await waitFor(() => expect(card).toHaveAttribute("data-state", "issue"));
    expect(card).toHaveTextContent("Rotate the signing keys");
    expect(card.querySelector("[data-status-category='todo']")).toHaveTextContent("To do");
    const field = (name: string) => card.querySelector(`[data-field="${name}"] dd`);
    expect(field("type")).toHaveTextContent("Task");
    expect(field("priority")).toHaveTextContent("Highest");
    expect(field("assignee")).toHaveTextContent("Alice");
    expect(field("reporter")).toHaveTextContent("Bob");
    expect(field("due")).toHaveTextContent(/Oct 15, 2026|15 Oct 2026/);
    const open = within(card).getByRole("link", { name: "Open in Armature CP-4" });
    expect(open).toHaveAttribute("href", `${BASE}/issues/CP-4`);
    expect(open).toHaveAttribute("target", "_blank");
    expect(await axeViolations()).toEqual([]);
  });

  it("says None for what an issue does not have", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/issues": lookup(["CP-4", { ...cp4, assignee: null, dueDate: null }]),
    });
    wrap(<DocView doc={blockDoc("CP-4")} />);
    await waitFor(() => expect(block("CP-4")).toHaveAttribute("data-state", "issue"));
    expect(block("CP-4")?.querySelector('[data-field="assignee"] dd')).toHaveTextContent("Unassigned");
    expect(block("CP-4")?.querySelector('[data-field="due"] dd')).toHaveTextContent("None");
  });

  it("shows the key alone, as a chip does, for an issue the viewer may not see", async () => {
    stubApi({ "GET /armature/account": account(), "GET /armature/issues": lookup(["SEC-1", null]) });
    wrap(<DocView doc={blockDoc("SEC-1")} />);
    await waitFor(() => expect(block("SEC-1")).toHaveAttribute("data-state", "hidden"));
    expect(block("SEC-1")).toHaveTextContent("SEC-1Not available");
    expect(within(block("SEC-1")!).getByRole("link", { name: "SEC-1" })).toHaveAttribute("href", `${BASE}/issues/SEC-1`);
    expect(await axeViolations()).toEqual([]);
  });

  it("shows the key and the way to connect to a viewer without a token, and asks Armature nothing", async () => {
    const sent = stubApi({ "GET /armature/account": account({ connected: false, status: "not_connected" }) });
    wrap(<DocView doc={blockDoc("CP-4")} />);
    await waitFor(() => expect(block("CP-4")).toHaveAttribute("data-state", "connect"));
    expect(within(block("CP-4")!).getByRole("link", { name: "Connect Armature to see this issue" })).toHaveAttribute("href", "/settings/profile#armature");
    expect(sent.some((request) => request.path.startsWith("/armature/issues"))).toBe(false);
    expect(await axeViolations()).toEqual([]);
  });

  it("asks once for the keys of blocks and chips together", async () => {
    const asked: string[][] = [];
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/issues": (request) => {
        asked.push(new URL(request.url).searchParams.getAll("key"));
        return lookup(["CP-1", null], ["CP-4", cp4]);
      },
    });
    const doc = blockDoc("CP-4");
    doc.content?.push({ type: "paragraph", content: [{ type: "armatureIssue", attrs: { key: "CP-1" } }] });
    wrap(<DocView doc={doc} />);
    await waitFor(() => expect(block("CP-4")).toHaveAttribute("data-state", "issue"));
    expect(asked).toEqual([["CP-1", "CP-4"]]);
  });

  it("is described in words in a comparison of versions", async () => {
    stubApi({ "GET /armature/account": account(), "GET /armature/issues": lookup(["CP-4", cp4]) });
    wrap(<DocDiffView blocks={[{ change: "inserted", node: { type: "armatureIssueBlock", attrs: { key: "CP-4" } } }]} />);
    expect(await screen.findByText("Armature issue CP-4")).toBeInTheDocument();
    expect(screen.queryByText("Rotate the signing keys")).toBeNull();
  });
});

describe("the issue picker", () => {
  it("reads a key in any case, or an issue address of the connected Armature", () => {
    expect(pickedKey(" cp-4 ", BASE)).toBe("CP-4");
    expect(pickedKey(`${BASE}/issues/sec-2/`, BASE)).toBe("SEC-2");
    expect(pickedKey("https://elsewhere.example.com/issues/CP-4", BASE)).toBeNull();
    expect(pickedKey("UTF-8x", BASE)).toBeNull();
  });
});

const source: IssueSource = { baseUrl: () => BASE, knowsProject: () => true };

function editorWith(armature?: IssueSource) {
  const onChange = vi.fn();
  wrap(<Editor id="page-body" value={null} onChange={onChange} armature={armature} />);
  const last = () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined;
  return { box: document.getElementById("page-body")!, last };
}

describe("inserting an issue block from the slash menu", () => {
  it("asks for the key, shows what Armature found, and inserts the block by the issue's key", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/issues/CP-4": { status: 200, body: { status: "ok", issue: cp4 } },
      "GET /armature/issues/CP-99": { status: 200, body: { status: "ok", issue: null } },
      "GET /armature/issues": lookup(["CP-4", cp4]),
    });
    const user = userEvent.setup();
    const { box, last } = editorWith(source);
    await user.click(box);
    await user.type(box, "/armature");
    const list = await screen.findByRole("listbox", { name: "Insert a block" });
    await waitFor(() => expect(within(list).getAllByRole("option")).toHaveLength(1));
    await user.keyboard("{Enter}");

    const dialog = await screen.findByRole("dialog", { name: "Insert an Armature issue" });
    const field = within(dialog).getByRole("textbox", { name: "Issue key or address" });
    await waitFor(() => expect(field).toHaveFocus());
    await user.type(field, "cp-99");
    expect(
      await within(dialog).findByText("No issue CP-99 that you can see in Armature. Check the key, or connect your Armature account."),
    ).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    await user.clear(field);
    await user.type(field, "not a key");
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(within(dialog).getByText(/That is not an issue key/)).toBeInTheDocument();
    expect(field).toHaveAttribute("aria-invalid", "true");

    await user.clear(field);
    await user.type(field, "cp-4");
    expect(await within(dialog).findByText("Found CP-4: Rotate the signing keys")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await user.keyboard("{Enter}");

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(last()?.content?.some((node) => node.type === "armatureIssueBlock" && node.attrs?.key === "CP-4")).toBe(true);
    expect(JSON.stringify(last())).not.toContain("/armature");
    expect(JSON.stringify(last())).not.toContain("Rotate the signing keys");
    expect(box.querySelector('[data-armature-issue-block="CP-4"]')).not.toBeNull();
  });

  it("offers no Armature block where the organization has no Armature", async () => {
    stubApi({});
    const user = userEvent.setup();
    const { box } = editorWith({ baseUrl: () => null, knowsProject: () => false });
    await user.click(box);
    await user.type(box, "/armature");
    expect(await screen.findByText("No block matches. Keep typing, or press Escape.")).toBeInTheDocument();
  });

  it("tells an author without a token to connect, and inserts nothing", async () => {
    stubApi({ "GET /armature/account": account({ connected: false, status: "not_connected" }) });
    const user = userEvent.setup();
    const { box } = editorWith(source);
    await user.click(box);
    await user.type(box, "/armature");
    await screen.findByRole("listbox", { name: "Insert a block" });
    await user.keyboard("{Enter}");
    const dialog = await screen.findByRole("dialog", { name: "Insert an Armature issue" });
    expect(await within(dialog).findByRole("link", { name: "Connect your account" })).toHaveAttribute("href", "/settings/profile#armature");
    expect(within(dialog).getByRole("button", { name: "Insert" })).toBeDisabled();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});
