import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ArmatureAccount, ArmatureIssue } from "@/api/armature";
import { DocDiffView, DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const BASE = "https://armature.example.com";

const issue = (key: string, summary: string, icon = "task", category: ArmatureIssue["status"]["category"] = "done"): ArmatureIssue => ({
  key,
  url: `${BASE}/issues/${key}`,
  projectKey: key.split("-")[0]!,
  summary,
  type: { id: "0195f000-0000-7000-8000-000000000001", name: icon === "bug" ? "Bug" : "Task", icon, level: 0 },
  status: { name: category === "done" ? "Done" : "In progress", category },
  priority: "high",
  assignee: { id: "0195f000-0000-7000-8000-000000000002", name: "Alice" },
  reporter: null,
  dueDate: null,
  createdAt: "2026-09-01T10:00:00Z",
  updatedAt: "2026-09-02T10:00:00Z",
});

const account = (over: Partial<ArmatureAccount>): Answer => ({
  status: 200,
  body: { account: { configured: true, baseUrl: BASE, connected: true, status: "ok", user: null, checkedAt: null, ...over } },
});

const doc: Doc = {
  type: "doc",
  content: [
    {
      type: "paragraph",
      content: [
        { type: "text", text: "Fixed in " },
        { type: "armatureIssue", attrs: { key: "CP-1" } },
        { type: "text", text: ", blocked by " },
        { type: "armatureIssue", attrs: { key: "SEC-1" } },
        { type: "text", text: " and " },
        { type: "armatureIssue", attrs: { key: "CP-1" } },
      ],
    },
  ],
};

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

const chips = () => Array.from(document.querySelectorAll<HTMLElement>("[data-armature-issue]"));

describe("Armature issue chips in the read view", () => {
  it("show type, key, summary and status to a viewer who may see the issue, from one lookup", async () => {
    const sent = stubApi({
      "GET /armature/account": account({}),
      "GET /armature/issues": {
        status: 200,
        body: {
          status: "ok",
          issues: [
            { key: "CP-1", issue: issue("CP-1", "Set up the build pipeline") },
            { key: "SEC-1", issue: null },
          ],
        },
      },
      "GET /armature/issues/CP-1": { status: 200, body: { status: "ok", issue: issue("CP-1", "Set up the build pipeline, renamed") } },
    });
    wrap(<DocView doc={doc} />);
    const [link] = await screen.findAllByRole("link", { name: /CP-1.*Set up the build pipeline/ });
    if (!link) throw new Error("no chip link");
    expect(link).toHaveAttribute("href", `${BASE}/issues/CP-1`);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link.querySelector("[data-icon='task']")).not.toBeNull();
    expect(link.querySelector("[data-status-category='done']")).toHaveTextContent("Done");
    expect(chips().map((chip) => chip.dataset.state)).toEqual(["issue", "hidden", "issue"]);
    const hidden = chips()[1]!;
    expect(hidden).toHaveTextContent("SEC-1Not available");
    expect(hidden).not.toHaveTextContent("vulnerability");

    const lookups = sent.filter((request) => request.path === "/armature/issues");
    expect(lookups).toHaveLength(1);
    expect(await axeViolations()).toEqual([]);

    await userEvent.setup().tab();
    expect(link).toHaveFocus();
    const card = await screen.findByRole("tooltip");
    expect(link).toHaveAttribute("aria-describedby", card.id);
    await waitFor(() => expect(card).toHaveTextContent("Set up the build pipeline, renamed"));
    expect(card).toHaveTextContent("High");
    expect(card).toHaveTextContent("Alice");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
  });

  it("ask in one request which keys the view names, each once", async () => {
    const asked: string[][] = [];
    stubApi({
      "GET /armature/account": account({}),
      "GET /armature/issues": (request) => {
        asked.push(new URL(request.url).searchParams.getAll("key"));
        return { status: 200, body: { status: "ok", issues: [] } };
      },
    });
    wrap(<DocView doc={doc} />);
    await waitFor(() => expect(asked).toHaveLength(1));
    expect(asked[0]).toEqual(["CP-1", "SEC-1"]);
  });

  it("show the key and the way to connect to a viewer without a token, and ask Armature nothing", async () => {
    const sent = stubApi({ "GET /armature/account": account({ connected: false, status: "not_connected" }) });
    wrap(<DocView doc={doc} />);
    const [key] = await screen.findAllByRole("link", { name: "CP-1" });
    expect(key).toHaveAttribute("href", `${BASE}/issues/CP-1`);
    const hints = screen.getAllByRole("link", { name: "Connect Armature to see this issue" });
    expect(hints[0]).toHaveAttribute("href", "/settings/profile#armature");
    expect(chips().map((chip) => chip.dataset.state)).toEqual(["connect", "connect", "connect"]);
    expect(sent.some((request) => request.path.startsWith("/armature/issues"))).toBe(false);
    expect(await axeViolations()).toEqual([]);
  });

  it("show the key alone, as text, when the organization has no Armature", async () => {
    stubApi({ "GET /armature/account": account({ configured: false, baseUrl: null, connected: false, status: "not_configured" }) });
    wrap(<DocView doc={doc} />);
    await waitFor(() => expect(chips()[0]?.dataset.state).toBe("plain"));
    expect(screen.queryByRole("link")).toBeNull();
    expect(chips()[0]).toHaveTextContent(/^CP-1$/);
  });

  it("show the key with its link when Armature does not answer", async () => {
    stubApi({ "GET /armature/account": account({}), "GET /armature/issues": { status: 200, body: { status: "unreachable", issues: [] } } });
    wrap(<DocView doc={doc} />);
    await waitFor(() => expect(chips()[0]?.dataset.state).toBe("unreachable"));
    expect(chips()[0]).toHaveTextContent("Armature did not answer");
  });

  it("are drawn in a comparison of versions, with what changed marked", async () => {
    stubApi({
      "GET /armature/account": account({}),
      "GET /armature/issues": { status: 200, body: { status: "ok", issues: [{ key: "CP-2", issue: issue("CP-2", "Sign-in fails", "bug", "in_progress") }] } },
    });
    wrap(
      <DocDiffView
        blocks={[
          {
            change: "modified",
            node: { type: "paragraph", content: [{ type: "armatureIssue", attrs: { key: "CP-2" }, marks: [{ type: "diffInsert" }] }] },
          },
        ]}
      />,
    );
    const link = await screen.findByRole("link", { name: /CP-2.*Sign-in fails/ });
    expect(link.closest("ins")).not.toBeNull();
    expect(link.querySelector("[data-icon='bug']")).not.toBeNull();
  });

  it("ask nothing about Armature for a page that names no issue", async () => {
    const sent = stubApi({});
    render(<DocView doc={{ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "CP-1 as words" }] }] }} />);
    expect(chips()).toEqual([]);
    expect(sent).toEqual([]);
  });
});
