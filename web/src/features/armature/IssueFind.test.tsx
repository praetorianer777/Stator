import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ArmatureAccount, ArmatureIssue } from "@/api/armature";
import { meQueryKey } from "@/api/auth";
import { ARMATURE_FIND_DEBOUNCE_MS, ARMATURE_RECENT_ISSUES, ARMATURE_RECENT_STORAGE_PREFIX } from "@/config";
import { Editor } from "@/features/editor/Editor";
import type { IssueSource } from "@/features/editor/armatureIssue";
import type { Doc } from "@/features/editor/schema";
import { HINT_HREF, signedIn, standOnHintPage, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { IssuePicker } from "./IssuePicker";
import { recentIssueKeys, rememberIssue } from "./recentIssues";

const BASE = "https://armature.example.com";
const RECENT = `${ARMATURE_RECENT_STORAGE_PREFIX}${signedIn.user.id}`;

beforeEach(() => window.localStorage.clear());

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
  window.localStorage.clear();
});

function issue(key: string, summary: string, icon = "task", category: ArmatureIssue["status"]["category"] = "todo"): ArmatureIssue {
  return {
    key,
    url: `${BASE}/issues/${key}`,
    projectKey: key.split("-")[0]!,
    summary,
    type: { id: "0195f000-0000-7000-8000-000000000001", name: icon === "bug" ? "Bug" : "Task", icon, level: 0 },
    status: { name: category === "in_progress" ? "In progress" : "To do", category },
    priority: "medium",
    assignee: null,
    reporter: null,
    dueDate: null,
    createdAt: "2026-09-01T10:00:00Z",
    updatedAt: "2026-09-02T10:00:00Z",
  };
}

const cp4 = issue("CP-4", "Rotate the signing keys");
const cp2 = issue("CP-2", "Sign-in fails with an expired session", "bug", "in_progress");
const cp3 = issue("CP-3", "Write the onboarding guide");

const account = (over: Partial<ArmatureAccount> = {}): Answer => ({
  status: 200,
  body: { account: { configured: true, baseUrl: BASE, connected: true, status: "ok", user: null, checkedAt: null, ...over } },
});

/** Answers each find by its words, and keeps the words asked for. */
function finder(byWords: Record<string, ArmatureIssue[]>, status = "ok") {
  const asked: string[] = [];
  const answer = (request: Request): Answer => {
    const words = new URL(request.url, "http://app.test").searchParams.get("words") ?? "";
    asked.push(words);
    return { status: 200, body: { status, issues: status === "ok" ? (byWords[words.toLowerCase()] ?? []) : [] } };
  };
  return { asked, answer };
}

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(meQueryKey, signedIn);
  return render(
    <QueryClientProvider client={client}>
      <main>
        <h1>Page</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

function picker() {
  const onInsert = vi.fn();
  wrap(<IssuePicker onInsert={onInsert} onClose={() => undefined} />);
  const dialog = screen.getByRole("dialog", { name: "Insert an Armature issue" });
  const field = within(dialog).getByRole("combobox", { name: "Find an issue" });
  const state = () => dialog.querySelector("[data-picker-state]")?.getAttribute("data-picker-state");
  return { dialog, field, onInsert, state };
}

describe("the issue picker finds issues by words", () => {
  it("lists what Armature finds as the author types, newest first, and Enter inserts the chosen one", async () => {
    const find = finder({ sign: [cp4, cp2] });
    stubApi({ "GET /armature/account": account(), "GET /armature/find": find.answer });
    const user = userEvent.setup();
    const { dialog, field, onInsert } = picker();

    await user.type(field, "sign");
    const options = await within(dialog).findAllByRole("option");
    expect(options.map((o) => o.getAttribute("data-issue-option"))).toEqual(["CP-4", "CP-2"]);
    // Typed quickly, the words reach Armature once, when the typing stops.
    expect(find.asked).toEqual(["sign"]);
    expect(options[0]).toHaveTextContent("CP-4");
    expect(options[0]).toHaveTextContent("Rotate the signing keys");
    expect(options[0]).toHaveTextContent("To do");
    expect(options[0]).toHaveTextContent("Project CP");
    expect(options[0]).toHaveAttribute("aria-selected", "true");
    expect(within(dialog).getByRole("status")).toHaveTextContent("2 issues found.");
    expect(await axeViolations()).toEqual([]);

    await user.keyboard("{ArrowDown}");
    expect(options[1]).toHaveAttribute("aria-selected", "true");
    expect(field).toHaveAttribute("aria-activedescendant", options[1]!.id);
    await user.keyboard("{ArrowDown}");
    expect(options[0]).toHaveAttribute("aria-selected", "true");
    await user.keyboard("{ArrowUp}{Enter}");
    expect(onInsert).toHaveBeenCalledWith("CP-2");
    expect(recentIssueKeys(signedIn.user.id)).toEqual(["CP-2"]);
  });

  it("inserts the issue the mouse picks", async () => {
    const find = finder({ onboarding: [cp3] });
    stubApi({ "GET /armature/account": account(), "GET /armature/find": find.answer });
    const user = userEvent.setup();
    const { dialog, field, onInsert } = picker();
    await user.type(field, "onboarding");
    await user.click(await within(dialog).findByRole("option", { name: /CP-3/ }));
    expect(onInsert).toHaveBeenCalledWith("CP-3");
  });

  it("asks once more only after the typing stops again", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const find = finder({ sign: [cp4, cp2], "sign-in": [cp2] });
    stubApi({ "GET /armature/account": account(), "GET /armature/find": find.answer });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { dialog, field } = picker();
    await user.type(field, "sign");
    await act(() => vi.advanceTimersByTimeAsync(ARMATURE_FIND_DEBOUNCE_MS));
    await within(dialog).findByRole("option", { name: /CP-4/ });
    await user.type(field, "-in");
    await act(() => vi.advanceTimersByTimeAsync(ARMATURE_FIND_DEBOUNCE_MS));
    await waitFor(() => expect(within(dialog).getAllByRole("option")).toHaveLength(1));
    expect(find.asked).toEqual(["sign", "sign-in"]);
  });

  it("still goes straight to a key or an issue address", async () => {
    const find = finder({});
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/find": find.answer,
      "GET /armature/issues/CP-4": { status: 200, body: { status: "ok", issue: cp4 } },
      "GET /armature/issues/CP-99": { status: 200, body: { status: "ok", issue: null } },
    });
    const user = userEvent.setup();
    const { dialog, field, onInsert } = picker();
    await user.type(field, "cp-99");
    expect(
      await within(dialog).findByText("No issue CP-99 that you can see in Armature. Check the key, or connect your Armature account."),
    ).toBeInTheDocument();
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(within(dialog).getByRole("button", { name: "Insert" })).toBeDisabled();
    await user.clear(field);
    await user.type(field, `${BASE}/issues/cp-4`);
    expect(await within(dialog).findByRole("option", { name: /Rotate the signing keys/ })).toBeInTheDocument();
    await user.keyboard("{Enter}");
    expect(onInsert).toHaveBeenCalledWith("CP-4");
    expect(find.asked).toEqual([]);
  });

  it("says when nothing matches, and when Armature does not answer", async () => {
    let status = "ok";
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/find": () => ({ status: 200, body: { status, issues: [] } }),
    });
    const user = userEvent.setup();
    const { dialog, field, state } = picker();
    await user.type(field, "x");
    expect(within(dialog).getByRole("status")).toHaveTextContent("Type at least 2 characters to look for.");
    await user.type(field, "yz");
    await waitFor(() => expect(state()).toBe("noMatch"));
    expect(within(dialog).getByRole("status")).toHaveTextContent("No issue you can see in Armature has these words. Try other words, or the issue's key.");
    expect(within(dialog).getByRole("button", { name: "Insert" })).toBeDisabled();

    status = "unreachable";
    await user.type(field, "w");
    await waitFor(() => expect(state()).toBe("unreachable"));
    expect(dialog).toHaveTextContent("Armature did not answer. Try again in a moment.");
  });

  it("tells an author without a token to connect, with a link to the token form", async () => {
    standOnHintPage();
    const sent = stubApi({ "GET /armature/account": account({ connected: false, status: "not_connected" }) });
    const user = userEvent.setup();
    const { dialog, field, state } = picker();
    await waitFor(() => expect(state()).toBe("connect"));
    expect(dialog).toHaveTextContent("Connect your Armature account to find its issues.");
    expect(within(dialog).getByRole("link", { name: "Connect your account" })).toHaveAttribute("href", HINT_HREF);
    await user.type(field, "signing");
    expect(state()).toBe("connect");
    expect(sent.some((r) => r.path === "/armature/find")).toBe(false);
  });

  it("says an Armature without issue-embed@1 needs updating, and asks it nothing", async () => {
    const sent = stubApi({ "GET /armature/account": account({ missing: [{ feature: "issues", capability: "issue-embed@1" }] }) });
    const user = userEvent.setup();
    const { dialog, field, state } = picker();
    await user.type(field, "signing");
    await waitFor(() => expect(state()).toBe("outdated"));
    expect(dialog).toHaveTextContent("issue-embed@1");
    expect(sent.some((r) => r.path === "/armature/find" || r.path.startsWith("/armature/issues"))).toBe(false);
  });

  it("offers the author's recent issues while the field is empty", async () => {
    window.localStorage.setItem(RECENT, JSON.stringify(["CP-3", "CP-4"]));
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/issues": {
        status: 200,
        body: {
          status: "ok",
          issues: [
            { key: "CP-3", issue: cp3 },
            { key: "CP-4", issue: cp4 },
          ],
        },
      },
    });
    const user = userEvent.setup();
    const { dialog, onInsert } = picker();
    const options = await within(dialog).findAllByRole("option");
    expect(options.map((o) => o.getAttribute("data-issue-option"))).toEqual(["CP-3", "CP-4"]);
    expect(within(dialog).getByRole("status")).toHaveTextContent("Your recently inserted issues.");
    await user.click(options[1]!);
    expect(onInsert).toHaveBeenCalledWith("CP-4");
    expect(recentIssueKeys(signedIn.user.id)).toEqual(["CP-4", "CP-3"]);
  });
});

describe("recent issues", () => {
  it("are kept per person, newest first, a few at most, and survive a browser that refuses storage", () => {
    for (let n = 1; n <= ARMATURE_RECENT_ISSUES + 2; n++) rememberIssue("u-1", `CP-${n}`);
    rememberIssue("u-1", "CP-3");
    const kept = recentIssueKeys("u-1");
    expect(kept).toHaveLength(ARMATURE_RECENT_ISSUES);
    expect(kept[0]).toBe("CP-3");
    expect(kept[1]).toBe(`CP-${ARMATURE_RECENT_ISSUES + 2}`);
    expect(recentIssueKeys("u-2")).toEqual([]);
    expect(recentIssueKeys(undefined)).toEqual([]);

    window.localStorage.setItem(`${ARMATURE_RECENT_STORAGE_PREFIX}u-3`, '{"not": "a list"}');
    expect(recentIssueKeys("u-3")).toEqual([]);
    window.localStorage.setItem(`${ARMATURE_RECENT_STORAGE_PREFIX}u-3`, '["cp-1", 7, "not a key"]');
    expect(recentIssueKeys("u-3")).toEqual(["CP-1"]);

    const storage = Object.getOwnPropertyDescriptor(window, "localStorage")!;
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      get: () => {
        throw new Error("denied");
      },
    });
    try {
      expect(recentIssueKeys("u-1")).toEqual([]);
      expect(() => rememberIssue("u-1", "CP-9")).not.toThrow();
    } finally {
      Object.defineProperty(window, "localStorage", storage);
    }
  });
});

const source: IssueSource = { baseUrl: () => BASE, knowsProject: () => true };

function editorWith(armature: IssueSource = source) {
  const onChange = vi.fn();
  wrap(<Editor id="page-body" value={null} onChange={onChange} armature={armature} />);
  const last = () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined;
  const menu = () => document.querySelector<HTMLElement>("[data-issue-menu]");
  return { box: document.getElementById("page-body")!, last, menu };
}

const chips = (doc: Doc | undefined) =>
  (doc?.content ?? []).flatMap((block) => (block.content ?? []).filter((node) => node.type === "armatureIssue").map((node) => node.attrs?.key));

describe("finding an issue inline with two brackets", () => {
  it("opens a menu at the caret that finds by words; arrows and Enter put the chip in place of what was typed", async () => {
    const find = finder({ sign: [cp4, cp2] });
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/find": find.answer,
      "GET /armature/issues": { status: 200, body: { status: "ok", issues: [{ key: "CP-2", issue: cp2 }] } },
    });
    const user = userEvent.setup();
    const { box, last, menu } = editorWith();
    await user.click(box);
    await user.type(box, "See [[[[sign");
    const list = await screen.findByRole("listbox", { name: "Armature issues" });
    await waitFor(() => expect(within(list).getAllByRole("option")).toHaveLength(2));
    expect(box).toHaveAttribute("aria-controls", list.id);
    expect(box).toHaveAttribute("aria-activedescendant", within(list).getAllByRole("option")[0]!.id);
    await user.keyboard("{ArrowDown}");
    expect(box).toHaveAttribute("aria-activedescendant", within(list).getAllByRole("option")[1]!.id);
    await user.keyboard("{Enter}");

    await waitFor(() => expect(menu()).toBeNull());
    expect(chips(last())).toEqual(["CP-2"]);
    expect(JSON.stringify(last())).not.toContain("[[");
    expect(JSON.stringify(last())).not.toContain("Sign-in fails");
    expect(box.querySelector('[data-armature-issue="CP-2"]')).not.toBeNull();
    expect(recentIssueKeys(signedIn.user.id)).toEqual(["CP-2"]);
    expect(find.asked).toEqual(["sign"]);
  });

  it("takes a click on an issue, and a key typed after the brackets", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/find": finder({ onboarding: [cp3] }).answer,
      "GET /armature/issues/CP-4": { status: 200, body: { status: "ok", issue: cp4 } },
      "GET /armature/issues": { status: 200, body: { status: "ok", issues: [] } },
    });
    const user = userEvent.setup();
    const { box, last } = editorWith();
    await user.click(box);
    await user.type(box, "[[[[onboarding");
    await user.click(await screen.findByRole("option", { name: /CP-3/ }));
    await waitFor(() => expect(chips(last())).toEqual(["CP-3"]));

    await user.type(box, "and [[[[cp-4");
    await screen.findByRole("option", { name: /Rotate the signing keys/ });
    await user.keyboard("{Enter}");
    await waitFor(() => expect(chips(last())).toEqual(["CP-3", "CP-4"]));
  });

  it("leaves the typed text alone on Escape, and on closing brackets", async () => {
    stubApi({ "GET /armature/account": account(), "GET /armature/find": finder({ sign: [cp4] }).answer });
    const user = userEvent.setup();
    const { box, last, menu } = editorWith();
    await user.click(box);
    await user.type(box, "[[[[sign");
    await screen.findByRole("option", { name: /CP-4/ });
    await user.keyboard("{Escape}");
    await waitFor(() => expect(menu()).toBeNull());
    await user.type(box, " more");
    expect(menu()).toBeNull();
    expect(JSON.stringify(last())).toContain("[[sign more");
    expect(chips(last())).toEqual([]);

    await user.type(box, " [[[[sign]] done");
    await waitFor(() => expect(menu()).toBeNull());
    expect(JSON.stringify(last())).toContain("[[sign]] done");
  });

  it("says when nothing matches, when Armature does not answer, and does not take Enter then", async () => {
    let status = "ok";
    stubApi({ "GET /armature/account": account(), "GET /armature/find": () => ({ status: 200, body: { status, issues: [] } }) });
    const user = userEvent.setup();
    const { box, last, menu } = editorWith();
    await user.click(box);
    await user.type(box, "[[[[nothing");
    await waitFor(() => expect(menu()).toHaveAttribute("data-issue-menu", "noMatch"));
    expect(menu()).toHaveTextContent("No issue you can see in Armature has these words.");
    expect(screen.queryByRole("listbox", { name: "Armature issues" })).toBeNull();
    status = "unreachable";
    await user.type(box, "s");
    await waitFor(() => expect(menu()).toHaveAttribute("data-issue-menu", "unreachable"));
    expect(menu()).toHaveTextContent("Armature did not answer.");
    await user.keyboard("{Enter}");
    expect(chips(last())).toEqual([]);
  });

  it("asks a reader without a token to connect, and an outdated Armature nothing", async () => {
    standOnHintPage();
    stubApi({ "GET /armature/account": account({ connected: false, status: "not_connected" }) });
    const user = userEvent.setup();
    const { box, menu } = editorWith();
    await user.click(box);
    await user.type(box, "[[[[sign");
    await waitFor(() => expect(menu()).toHaveAttribute("data-issue-menu", "connect"));
    expect(within(menu()!).getByRole("link", { name: "Connect your account" })).toHaveAttribute("href", HINT_HREF);
    cleanup();

    const sent = stubApi({ "GET /armature/account": account({ missing: [{ feature: "issues", capability: "issue-embed@1" }] }) });
    const again = editorWith();
    await user.click(again.box);
    await user.type(again.box, "[[[[sign");
    await waitFor(() => expect(again.menu()).toHaveAttribute("data-issue-menu", "outdated"));
    expect(again.menu()).toHaveTextContent("issue-embed@1");
    expect(sent.some((r) => r.path === "/armature/find")).toBe(false);
  });

  it("offers recent issues right after the brackets, and nothing where the organization has no Armature", async () => {
    window.localStorage.setItem(RECENT, JSON.stringify(["CP-4"]));
    stubApi({ "GET /armature/account": account(), "GET /armature/issues": { status: 200, body: { status: "ok", issues: [{ key: "CP-4", issue: cp4 }] } } });
    const user = userEvent.setup();
    const { box, menu } = editorWith();
    await user.click(box);
    await user.type(box, "[[[[");
    expect(await screen.findByRole("option", { name: /CP-4/ })).toBeInTheDocument();
    expect(menu()).toHaveAttribute("data-issue-menu", "recent");
    cleanup();

    const none = editorWith({ baseUrl: () => null, knowsProject: () => false });
    await user.click(none.box);
    await user.type(none.box, "[[[[sign");
    expect(none.menu()).toBeNull();
  });

  it("does not turn a key typed after the brackets into a chip at the next space", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/find": finder({}).answer,
      "GET /armature/issues": { status: 200, body: { status: "ok", issues: [] } },
    });
    const user = userEvent.setup();
    const { box, last } = editorWith();
    await user.click(box);
    await user.type(box, "[[[[CP-4 x");
    expect(chips(last())).toEqual([]);
    expect(JSON.stringify(last())).toContain("[[CP-4 x");
  });
});
