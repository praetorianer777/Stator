import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Notification, Preferences } from "@/api/notifications";
import { UNREAD_POLL_MS } from "@/config";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const space = aSpace();
const home = aPage();
const runbookId = "0195f000-0000-7000-8000-0000000000b1";
const runbook = aPage({ id: runbookId, title: "Runbook", home: false, parentId: home.id, ancestors: [{ id: home.id, title: "Handbook", home: true }] });

function aNotification(over: Partial<Notification> = {}): Notification {
  return {
    id: "0195f000-0000-7000-8000-0000000000e1",
    kind: "published",
    actorId: "0195f000-0000-7000-8000-0000000000a0",
    actorName: "Bob Builder",
    page: { id: runbookId, title: "Runbook", spaceKey: "DOCS" },
    threadId: null,
    commentId: null,
    version: 3,
    excerpt: "Added the rollback steps.",
    createdAt: "2026-09-30T08:00:00Z",
    readAt: null,
    ...over,
  };
}

const allOn = { mentioned: true, shared: true, replied: true, commented: true, resolved: true, published: true, created: true };
const defaults: Preferences = { inApp: { ...allOn }, email: { ...allOn }, digest: "off", autoWatch: true };

function stubShell(more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    [`GET /pages/${home.id}/attachments`]: { status: 200, body: { attachments: [] } },
    [`GET /pages/${runbookId}`]: { status: 200, body: { page: runbook, space } },
    [`GET /pages/${runbookId}/attachments`]: { status: 200, body: { attachments: [] } },
    [`POST /pages/${runbookId}/visit`]: { status: 204 },
    ...more,
  });
}

const unreadAsked = (sent: { method: string; path: string }[]) => sent.filter((each) => each.path === "/notifications/unread-count").length;

describe("the notification bell", () => {
  it("shows the unread count as a badge and says it in its name", async () => {
    stubShell({ "GET /notifications/unread-count": { status: 200, body: { unread: 3 } } });
    await renderAt("/spaces");
    const bell = await screen.findByRole("button", { name: "Notifications, 3 unread" });
    expect(bell).toHaveAttribute("aria-haspopup", "dialog");
    expect(document.querySelector("[data-unread-badge]")).toHaveTextContent("3");
  });

  it("shows no badge while nothing is unread", async () => {
    stubShell({ "GET /notifications/unread-count": { status: 200, body: { unread: 0 } } });
    await renderAt("/spaces");
    expect(await screen.findByRole("button", { name: "Notifications" })).toBeInTheDocument();
    expect(document.querySelector("[data-unread-badge]")).toBeNull();
  });

  it("asks again when the window regains focus and every poll interval", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let unread = 1;
    const sent = stubShell({ "GET /notifications/unread-count": () => ({ status: 200, body: { unread } }) });
    await renderAt("/spaces");
    await screen.findByRole("button", { name: "Notifications, 1 unread" });
    const before = unreadAsked(sent);
    unread = 2;
    window.dispatchEvent(new Event("focus"));
    await screen.findByRole("button", { name: "Notifications, 2 unread" });
    expect(unreadAsked(sent)).toBeGreaterThan(before);
    unread = 5;
    await vi.advanceTimersByTimeAsync(UNREAD_POLL_MS);
    await screen.findByRole("button", { name: "Notifications, 5 unread" });
  });

  it("lists what happened in the reader's words, and following one marks it read", async () => {
    let read = false;
    const sent = stubShell({
      "GET /notifications/unread-count": () => ({ status: 200, body: { unread: read ? 0 : 1 } }),
      "GET /notifications": () => ({
        status: 200,
        body: {
          notifications: [
            aNotification({ readAt: read ? "2026-09-30T09:00:00Z" : null }),
            aNotification({
              id: "0195f000-0000-7000-8000-0000000000e2",
              kind: "created",
              actorName: "",
              version: 1,
              excerpt: "",
              readAt: "2026-09-30T07:00:00Z",
            }),
          ],
          total: 2,
          limit: 20,
          offset: 0,
        },
      }),
      "POST /notifications/read": () => {
        read = true;
        return { status: 204 };
      },
    });
    const router = await renderAt("/spaces");
    await userEvent.click(await screen.findByRole("button", { name: "Notifications, 1 unread" }));
    const panel = await screen.findByRole("dialog", { name: "Notifications" });
    const [first, second] = within(panel).getAllByRole("button", { name: /Runbook/ }) as [HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("Unread: Bob Builder published version 3 of Runbook");
    expect(first).toHaveTextContent("Added the rollback steps.");
    expect(second).toHaveTextContent("Somebody created Runbook");
    expect(second).not.toHaveTextContent("Unread");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(first);
    await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${runbookId}/runbook`));
    expect(sent.find((each) => each.method === "POST" && each.path === "/notifications/read")?.body).toEqual({ ids: ["0195f000-0000-7000-8000-0000000000e1"] });
    expect(screen.queryByRole("dialog", { name: "Notifications" })).toBeNull();
    await screen.findByRole("button", { name: "Notifications" });
  });

  it("leads a reply to its thread, which the page brings into view", async () => {
    const threadId = "0195f000-0000-7000-8000-00000000c0a1";
    stubShell({
      "GET /notifications/unread-count": { status: 200, body: { unread: 1 } },
      "GET /notifications": {
        status: 200,
        body: {
          notifications: [aNotification({ kind: "replied", threadId, commentId: threadId, version: null, excerpt: "Monday." })],
          total: 1,
          limit: 20,
          offset: 0,
        },
      },
      "POST /notifications/read": { status: 204 },
      [`GET /pages/${runbookId}/comments`]: {
        status: 200,
        body: {
          threads: [
            {
              id: threadId,
              pageId: runbookId,
              kind: "page",
              anchor: null,
              resolved: false,
              resolvedByName: "",
              resolvedAt: null,
              can: { reply: true, resolve: false },
              comments: [
                {
                  id: threadId,
                  threadId,
                  authorId: "u-ada",
                  authorName: "Ada Lovelace",
                  body: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "Friday?" }] }] },
                  deleted: false,
                  createdAt: "2026-09-30T08:00:00Z",
                  editedAt: null,
                  reactions: [],
                  can: { edit: true, delete: true },
                },
              ],
            },
          ],
        },
      },
    });
    Element.prototype.scrollIntoView = vi.fn();
    const router = await renderAt("/spaces");
    await userEvent.click(await screen.findByRole("button", { name: "Notifications, 1 unread" }));
    const panel = await screen.findByRole("dialog", { name: "Notifications" });
    const item = within(panel).getByRole("button", { name: /Runbook/ });
    expect(item).toHaveTextContent("Bob Builder replied in a thread on Runbook");
    await userEvent.click(item);
    await waitFor(() => expect(router.state.location.search).toEqual({ thread: threadId }));
    await waitFor(() => expect(document.querySelector(`[data-thread="${threadId}"]`)).toHaveFocus());
  });

  it("marks everything read at once, and closes on Escape with focus back on the bell", async () => {
    const sent = stubShell({
      "GET /notifications/unread-count": { status: 200, body: { unread: 1 } },
      "GET /notifications": { status: 200, body: { notifications: [aNotification()], total: 1, limit: 20, offset: 0 } },
      "POST /notifications/read": { status: 204 },
    });
    await renderAt("/spaces");
    const bell = await screen.findByRole("button", { name: "Notifications, 1 unread" });
    bell.focus();
    await userEvent.keyboard("{Enter}");
    const panel = await screen.findByRole("dialog", { name: "Notifications" });
    await userEvent.click(within(panel).getByRole("button", { name: "Mark all as read" }));
    await waitFor(() => expect(sent.find((each) => each.path === "/notifications/read")?.body).toEqual({ all: true }));
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "Notifications" })).toBeNull();
    expect(bell).toHaveFocus();
  });

  it("says so when there is nothing, and links to the settings", async () => {
    stubShell({
      "GET /notifications/unread-count": { status: 200, body: { unread: 0 } },
      "GET /notifications": { status: 200, body: { notifications: [], total: 0, limit: 20, offset: 0 } },
    });
    await renderAt("/spaces");
    await userEvent.click(await screen.findByRole("button", { name: "Notifications" }));
    const panel = await screen.findByRole("dialog", { name: "Notifications" });
    expect(within(panel).getByText("Nothing new. You hear here about pages you watch.")).toBeInTheDocument();
    expect(within(panel).getByRole("button", { name: "Mark all as read" })).toBeDisabled();
    expect(within(panel).getByRole("link", { name: "Notification settings" })).toHaveAttribute("href", "/settings/notifications");
  });
});

describe("the notification settings", () => {
  it("switch kinds in the app and by email, choose the digest and auto watch, and save the whole", async () => {
    const sent = stubShell({
      "GET /notification-preferences": { status: 200, body: { preferences: defaults } },
      "PUT /notification-preferences": async (request) => ({ status: 200, body: { preferences: await request.json() } }),
    });
    await renderAt("/settings/notifications");
    const inApp = await screen.findByRole("checkbox", { name: "In the app: A page you watch is published with a notice" });
    const email = screen.getByRole("checkbox", { name: "By email: A page you watch is published with a notice" });
    expect(inApp).toBeChecked();
    expect(email).toBeChecked();
    await userEvent.click(inApp);
    expect(email).toBeDisabled();
    expect(email).not.toBeChecked();
    await userEvent.click(screen.getByRole("checkbox", { name: "By email: Somebody mentions you" }));
    await userEvent.selectOptions(screen.getByLabelText("When emails go out"), "daily");
    expect(screen.getByRole("option", { name: "One email a day at 08:00 UTC with everything new" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("checkbox", { name: "Watch the pages I create and publish" }));
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Your notification settings are saved.");
    expect(sent.find((each) => each.method === "PUT")?.body).toEqual({
      inApp: { ...allOn, published: false },
      email: { ...allOn, mentioned: false },
      digest: "daily",
      autoWatch: false,
    });
  });

  it("are reached from the account menu", async () => {
    stubShell({ "GET /notification-preferences": { status: 200, body: { preferences: defaults } } });
    const router = await renderAt("/spaces");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Notifications" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/notifications"));
  });
});
