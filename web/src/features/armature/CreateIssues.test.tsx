import { afterEach, describe, expect, it, vi } from "vitest";

import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ArmatureIssue } from "@/api/armature";
import { Editor } from "@/features/editor/Editor";
import type { IssueSource } from "@/features/editor/armatureIssue";
import type { Doc, DocNode } from "@/features/editor/schema";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { ApiError } from "@/api/client";
import { keysFor, outcomes, refusalText } from "./CreateIssuesDialog";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const BASE = "https://armature.example.com";
const PAGE = "0195f000-0000-7000-8000-0000000000aa";
const BUG = "0195f000-0000-7000-8000-000000000011";

const issue = (key: string, summary: string): ArmatureIssue => ({
  key,
  url: `${BASE}/issues/${key}`,
  projectKey: "CP",
  summary,
  type: { id: BUG, name: "Bug", icon: "bug", level: 0 },
  status: { name: "To do", category: "todo" },
  priority: "medium",
  assignee: null,
  reporter: { id: "0195f000-0000-7000-8000-000000000003", name: "Alice" },
  dueDate: null,
  createdAt: "2026-10-01T10:00:00Z",
  updatedAt: "2026-10-01T10:00:00Z",
});

const account: Answer = {
  status: 200,
  body: {
    account: {
      configured: true,
      baseUrl: BASE,
      connected: true,
      status: "ok",
      user: null,
      checkedAt: null,
    },
  },
};
const projects: Answer = {
  status: 200,
  body: {
    status: "ok",
    projects: [
      { key: "CP", name: "Core platform", canCreate: true },
      { key: "SEC", name: "Security", canCreate: false },
    ],
  },
};
const types: Answer = {
  status: 200,
  body: {
    status: "ok",
    issueTypes: [{ id: BUG, name: "Bug", icon: "bug", level: 0 }],
  },
};

const source = (canCreate = true): IssueSource => ({
  baseUrl: () => BASE,
  knowsProject: () => true,
  canCreate: () => canCreate,
  pageId: () => PAGE,
});

function editorWith(value: Doc, armature: IssueSource) {
  const onChange = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <main>
        <h1>Page</h1>
        <Editor id="page-body" value={value} onChange={onChange} armature={armature} />
      </main>
    </QueryClientProvider>,
  );
  const last = () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined;
  return { box: document.getElementById("page-body")!, last };
}

function selectContents(from: Node, to: Node = from) {
  const range = document.createRange();
  range.setStart(from, 0);
  range.setEnd(to, to.childNodes.length || (to.textContent ?? "").length);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(range);
  document.dispatchEvent(new Event("selectionchange"));
}

const sentence: Doc = {
  type: "doc",
  content: [
    {
      type: "paragraph",
      content: [{ type: "text", text: "Renew the TLS certificate" }],
    },
  ],
};
const list: Doc = {
  type: "doc",
  content: [
    {
      type: "bulletList",
      content: ["Write the guide", "Update the page", "Tell support"].map((value) => ({
        type: "listItem",
        content: [{ type: "paragraph", content: [{ type: "text", text: value }] }],
      })),
    },
  ],
};

describe("creating Armature issues from a selection", () => {
  it("files selected text as one issue in a project the author may file in, and puts its chip in the text's place", async () => {
    const sent = stubApi({
      "GET /armature/account": account,
      "GET /armature/projects": projects,
      "GET /armature/issue-types": types,
      "POST /armature/issues": {
        status: 201,
        body: {
          issues: [issue("CP-6", "Renew the TLS certificate")],
          failed: null,
        },
      },
      "GET /armature/issues": {
        status: 200,
        body: {
          status: "ok",
          issues: [{ key: "CP-6", issue: issue("CP-6", "Renew the TLS certificate") }],
        },
      },
    });
    const user = userEvent.setup();
    const { box, last } = editorWith(sentence, source());
    box.focus();
    selectContents(box.querySelector("p")!);
    await user.click(await screen.findByRole("button", { name: "Create Armature issue" }));

    const dialog = await screen.findByRole("dialog", {
      name: "Create an Armature issue",
    });
    const project = await within(dialog).findByRole("combobox", {
      name: "Project",
    });
    expect(
      within(project)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["CP: Core platform"]);
    expect(within(dialog).getByRole("textbox", { name: "Summary of issue 1" })).toHaveValue("Renew the TLS certificate");
    await user.selectOptions(within(dialog).getByRole("combobox", { name: "Issue type" }), "Bug");
    expect(await axeViolations()).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Create 1 issue" }));

    expect(await within(dialog).findByText("CP-6")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({
      pageId: PAGE,
      projectKey: "CP",
      typeId: BUG,
      items: [{ summary: "Renew the TLS certificate" }],
    });
    expect(last()?.content?.[0]?.content).toEqual([{ type: "armatureIssue", attrs: { key: "CP-6" } }]);
    expect(await axeViolations()).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Done" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("files each list item, shows which one Armature refused and why, and leaves the rest as they were", async () => {
    stubApi({
      "GET /armature/account": account,
      "GET /armature/projects": projects,
      "GET /armature/issue-types": types,
      "POST /armature/issues": {
        status: 201,
        body: {
          issues: [issue("CP-7", "Write the guide")],
          failed: {
            index: 1,
            code: "validation_failed",
            message: "Armature refuses this summary.",
          },
        },
      },
    });
    const user = userEvent.setup();
    const { box, last } = editorWith(list, source());
    box.focus();
    const paragraphs = box.querySelectorAll("li p");
    selectContents(paragraphs[0]!, paragraphs[2]!);
    await user.click(await screen.findByRole("button", { name: "Create 3 Armature issues" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Create 3 Armature issues",
    });
    await within(dialog).findByRole("combobox", { name: "Project" });
    await user.click(within(dialog).getByRole("button", { name: "Create 3 issues" }));

    await within(dialog).findByText("CP-7");
    const results = dialog.querySelectorAll("[data-result]");
    expect([...results].map((r) => r.getAttribute("data-result"))).toEqual(["created", "failed", "skipped"]);
    expect(results[1]).toHaveTextContent("Update the page: Armature refuses this summary.");
    expect(results[2]).toHaveTextContent("Tell support");
    const items = (last()?.content?.[0] as DocNode | undefined)?.content?.map((li) => li.content?.[0]?.content);
    expect(items).toEqual([
      [
        { type: "text", text: "Write the guide " },
        { type: "armatureIssue", attrs: { key: "CP-7" } },
      ],
      [{ type: "text", text: "Update the page" }],
      [{ type: "text", text: "Tell support" }],
    ]);
  });

  it("sends only the items kept, with the summaries as changed", async () => {
    const sent = stubApi({
      "GET /armature/account": account,
      "GET /armature/projects": projects,
      "GET /armature/issue-types": types,
      "POST /armature/issues": {
        status: 201,
        body: {
          issues: [issue("CP-8", "Tell the support team")],
          failed: null,
        },
      },
    });
    const user = userEvent.setup();
    const { box, last } = editorWith(list, source());
    box.focus();
    const paragraphs = box.querySelectorAll("li p");
    selectContents(paragraphs[0]!, paragraphs[2]!);
    await user.click(await screen.findByRole("button", { name: "Create 3 Armature issues" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("combobox", { name: "Project" });
    await user.click(within(dialog).getByRole("button", { name: "Leave out issue 1" }));
    await user.click(within(dialog).getByRole("button", { name: "Leave out issue 2" }));
    const third = within(dialog).getByRole("textbox", {
      name: "Summary of issue 3",
    });
    await user.clear(third);
    await user.type(third, "Tell the support team");
    await user.click(within(dialog).getByRole("button", { name: "Create 1 issue" }));
    await within(dialog).findByText("CP-8");
    expect(sent.find((r) => r.method === "POST")?.body).toMatchObject({
      items: [{ summary: "Tell the support team" }],
    });
    const items = (last()?.content?.[0] as DocNode | undefined)?.content?.map((li) => li.content?.[0]?.content?.length);
    expect(items).toEqual([1, 1, 2]);
  });

  it("says in a sentence that Armature no longer takes the token", async () => {
    stubApi({
      "GET /armature/account": account,
      "GET /armature/projects": projects,
      "GET /armature/issue-types": types,
      "POST /armature/issues": {
        status: 409,
        body: {
          error: {
            code: "armature_rejected",
            message: "Armature no longer accepts your token. Make a new one under Tokens in Armature and paste it under Profile, Armature.",
          },
        },
      },
    });
    const user = userEvent.setup();
    const { box, last } = editorWith(sentence, source());
    box.focus();
    selectContents(box.querySelector("p")!);
    await user.click(await screen.findByRole("button", { name: "Create Armature issue" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("combobox", { name: "Project" });
    await user.click(within(dialog).getByRole("button", { name: "Create 1 issue" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Armature no longer accepts your token.");
    expect(JSON.stringify(last() ?? sentence)).not.toContain("armatureIssue");
  });

  it("says a create that timed out may have reached Armature", async () => {
    stubApi({
      "GET /armature/account": account,
      "GET /armature/projects": projects,
      "GET /armature/issue-types": types,
      "POST /armature/issues": {
        status: 502,
        body: {
          error: {
            code: "armature_unreachable",
            message: "Armature did not answer.",
          },
        },
      },
    });
    const user = userEvent.setup();
    const { box } = editorWith(sentence, source());
    box.focus();
    selectContents(box.querySelector("p")!);
    await user.click(await screen.findByRole("button", { name: "Create Armature issue" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("combobox", { name: "Project" });
    await user.click(within(dialog).getByRole("button", { name: "Create 1 issue" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(/may have filed/);
  });

  it("is not offered without a token or a connection", async () => {
    stubApi({ "GET /armature/account": account });
    const { box } = editorWith(sentence, source(false));
    box.focus();
    selectContents(box.querySelector("p")!);
    await screen.findByRole("button", { name: "Bold" });
    expect(screen.queryByRole("button", { name: /Create.*Armature issue/ })).toBeNull();
  });

  it("names Armature's sentence on a field rather than its general one", () => {
    const refused = new ApiError(422, { code: "validation_failed", message: "Some fields need attention.", fields: { summary: "Armature refuses this summary." } });
    expect(refusalText(refused)).toBe("Armature refuses this summary.");
    expect(refusalText(new ApiError(403, { code: "forbidden", message: "You may not file issues in this project." }))).toBe("You may not file issues in this project.");
    expect(refusalText(null)).toBeNull();
  });

  it("lays the answer over the items sent", () => {
    const answer = {
      issues: [issue("CP-6", "A"), issue("CP-7", "C")],
      failed: { index: 2, code: "forbidden", message: "No." },
    };
    expect(keysFor(4, [0, 2, 3], answer)).toEqual(["CP-6", null, "CP-7", null]);
    expect(
      outcomes(
        [
          { at: 0, summary: "A" },
          { at: 2, summary: "C" },
          { at: 3, summary: "D" },
        ],
        answer,
      ).map((o) => o.kind),
    ).toEqual(["created", "created", "failed"]);
  });
});
