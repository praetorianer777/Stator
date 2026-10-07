import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { safeHref, type Shortcut } from "@/api/shortcuts";
import { SHORTCUTS_MAX } from "@/config";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const space = aSpace();
const member = aSpace({ can: { editPages: true, administer: false, delete: false, purgeTrash: false, addComments: true, deletePages: true } });
const runbookId = "0195f000-0000-7000-8000-0000000000e1";

const runbook: Shortcut = {
  id: "0195f000-0000-7000-8000-00000000c001",
  kind: "page",
  label: "",
  url: null,
  page: { id: runbookId, title: "Runbook", spaceKey: "DOCS", home: false, archived: false },
};
const status: Shortcut = {
  id: "0195f000-0000-7000-8000-00000000c002",
  kind: "link",
  label: "Status",
  url: "https://status.example.com/now",
  page: null,
};
const sneaky: Shortcut = { ...status, id: "0195f000-0000-7000-8000-00000000c003", label: "Sneaky", url: "javascript:alert(1)" };

type Answers = Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;

function stub(shortcuts: () => Shortcut[], { asMember = false, more = {} }: { asMember?: boolean; more?: Answers } = {}) {
  const shown = asMember ? member : space;
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [shown] } },
    "GET /spaces/DOCS": { status: 200, body: { space: shown } },
    "GET /spaces/DOCS/pages": {
      status: 200,
      body: {
        pages: [{ id: runbookId, parentId: space.homePageId, title: "Runbook", hasChildren: false, archived: false, restricted: false, unpublished: false }],
      },
    },
    "GET /spaces/DOCS/outline": {
      status: 200,
      body: {
        pages: [
          { id: space.homePageId, parentId: null, title: "Handbook", depth: 0 },
          { id: runbookId, parentId: space.homePageId, title: "Runbook", depth: 1 },
        ],
      },
    },
    "GET /spaces/DOCS/shortcuts": () => ({ status: 200, body: { shortcuts: shortcuts() } }),
    ...more,
  });
}

describe("shortcuts in the sidebar", () => {
  it("lists them above the page tree, a page in the app and an address in a new tab", async () => {
    stub(() => [runbook, status, sneaky]);
    await renderAt("/s/DOCS/settings");
    const list = await screen.findByRole("list", { name: "Shortcuts" });
    const links = within(list).getAllByRole("link");
    expect(links.map((each) => each.textContent)).toEqual(["Runbook", "Status(opens in a new tab)"]);
    expect(links[0]).toHaveAttribute("href", `/s/DOCS/p/${runbookId}/runbook`);
    expect(links[0]).not.toHaveAttribute("target");
    expect(links[1]).toHaveAttribute("href", "https://status.example.com/now");
    expect(links[1]).toHaveAttribute("target", "_blank");
    expect(links[1]?.getAttribute("rel")?.split(" ")).toEqual(expect.arrayContaining(["noopener", "noreferrer"]));
    expect(within(list).queryByText("Sneaky")).toBeNull();
    const nav = list.closest("[data-space-nav]") as HTMLElement;
    const tree = await within(nav).findByRole("tree");
    expect(list.compareDocumentPosition(tree) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("shows nothing while the space has none", async () => {
    stub(() => []);
    await renderAt("/s/DOCS/settings");
    await screen.findByRole("tab", { name: "Shortcuts" });
    await waitFor(() => expect(document.querySelector("[data-sidebar-shortcuts]")).toBeNull());
  });

  it("opens only web addresses", () => {
    expect(safeHref("https://example.com/x")).toBe("https://example.com/x");
    expect(safeHref("http://intranet/wiki")).toBe("http://intranet/wiki");
    for (const bad of [
      "javascript:alert(1)",
      " javascript:alert(1)",
      "JAVASCRIPT:alert(1)",
      "data:text/html,x",
      "vbscript:x",
      "/relative",
      "",
      null,
      undefined,
    ]) {
      expect(safeHref(bad)).toBeUndefined();
    }
  });
});

describe("the shortcuts tab", () => {
  it("lets an administrator add an address, and refuses one that is not the web's before asking", async () => {
    let shortcuts = [runbook];
    const sent = stub(() => shortcuts, {
      more: {
        "POST /spaces/DOCS/shortcuts": () => {
          shortcuts = [runbook, status];
          return { status: 201, body: { shortcut: status } };
        },
      },
    });
    await renderAt("/s/DOCS/settings?tab=shortcuts");
    expect(await screen.findByRole("tab", { name: "Shortcuts" })).toHaveAttribute("aria-selected", "true");
    await screen.findByRole("list", { name: "Shortcuts of this space" });
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "A web address" }));
    const address = screen.getByLabelText("Address");
    await userEvent.type(address, "javascript:alert(1)");
    await userEvent.type(screen.getByLabelText("Label"), "Status");
    await userEvent.click(screen.getByRole("button", { name: "Add shortcut" }));
    expect(await screen.findByText("An address starts with https:// or http://, such as https://example.com.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "POST")).toBe(false);

    await userEvent.clear(address);
    await userEvent.type(address, "https://status.example.com/now");
    await userEvent.click(screen.getByRole("button", { name: "Add shortcut" }));
    expect(await screen.findByText("Added Status.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ url: "https://status.example.com/now", label: "Status" });
  });

  it("adds a page of the space, by its title when no label is given", async () => {
    const sent = stub(() => [], { more: { "POST /spaces/DOCS/shortcuts": { status: 201, body: { shortcut: runbook } } } });
    await renderAt("/s/DOCS/settings?tab=shortcuts");
    const page = await screen.findByLabelText("Page");
    await waitFor(() => expect(page).not.toBeDisabled());
    await userEvent.selectOptions(page, runbookId);
    await userEvent.click(screen.getByRole("button", { name: "Add shortcut" }));
    expect(await screen.findByText("Added Runbook.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ pageId: runbookId });
  });

  it("moves a shortcut by keyboard, says where it went and keeps the focus on it", async () => {
    let shortcuts = [runbook, status];
    const sent = stub(() => shortcuts, {
      more: {
        [`POST /spaces/DOCS/shortcuts/${status.id}/move`]: () => {
          shortcuts = [status, runbook];
          return { status: 200, body: { shortcuts } };
        },
      },
    });
    await renderAt("/s/DOCS/settings?tab=shortcuts");
    const up = await screen.findByRole("button", { name: "Move Status up" });
    expect(screen.getByRole("button", { name: "Move Runbook up" })).toBeDisabled();
    up.focus();
    await userEvent.keyboard("{Enter}");
    expect(await screen.findByText("Status is now 1 of 2.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ after: null });
    const rows = within(screen.getByRole("list", { name: "Shortcuts of this space" })).getAllByRole("listitem");
    expect(rows.map((row) => row.getAttribute("data-shortcut-name"))).toEqual(["Status", "Runbook"]);
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Move Status down" })));
  });

  it("removes a shortcut", async () => {
    let shortcuts = [runbook, status];
    stub(() => shortcuts, {
      more: {
        [`DELETE /spaces/DOCS/shortcuts/${runbook.id}`]: () => {
          shortcuts = [status];
          return { status: 204 };
        },
      },
    });
    await renderAt("/s/DOCS/settings?tab=shortcuts");
    await userEvent.click(await screen.findByRole("button", { name: "Remove Runbook" }));
    expect(await screen.findByText("Removed Runbook.")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("button", { name: "Remove Runbook" })).toBeNull());
  });

  it("shows a member the shortcuts and nothing to change them with", async () => {
    stub(() => [runbook, { ...status, label: "Status" }], { asMember: true });
    await renderAt("/s/DOCS/settings?tab=shortcuts");
    await screen.findByRole("list", { name: "Shortcuts of this space" });
    expect(screen.getByText("Only an administrator of this space can add, move or remove its shortcuts. Ask one of them.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Move|Remove/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add shortcut" })).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("says when the space holds as many as it may", async () => {
    const many = Array.from({ length: SHORTCUTS_MAX }, (_, i) => ({
      ...status,
      id: `0195f000-0000-7000-8000-0000000c${String(i).padStart(4, "0")}`,
      label: `Link ${i}`,
    }));
    stub(() => many);
    await renderAt("/s/DOCS/settings?tab=shortcuts");
    expect(await screen.findByText("This space has 30 shortcuts, as many as it may hold. Remove one to add another.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add shortcut" })).toBeNull();
  });
});
