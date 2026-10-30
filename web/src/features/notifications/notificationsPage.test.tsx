import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Notification } from "@/api/notifications";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const space = aSpace();
const runbookId = "0195f000-0000-7000-8000-0000000000b1";
const runbook = aPage({ id: runbookId, title: "Runbook", home: false });

function aNotification(over: Partial<Notification> = {}): Notification {
  return {
    id: "0195f000-0000-7000-8000-0000000000e1",
    kind: "published",
    actorId: "0195f000-0000-7000-8000-0000000000a0",
    actorName: "Bob Builder",
    page: { id: runbookId, title: "Runbook", spaceKey: "DOCS" },
    threadId: null,
    taskId: null,
    commentId: null,
    version: 3,
    excerpt: "",
    createdAt: "2026-09-30T08:00:00Z",
    readAt: null,
    ...over,
  };
}

const fresh = aNotification();
const old = aNotification({ id: "0195f000-0000-7000-8000-0000000000e2", kind: "commented", version: null, readAt: "2026-09-30T09:00:00Z" });

/** The API as the page asks it: only what is unread, or everything, and marking read. */
function stubNotifications() {
  let read = false;
  const sent = stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${runbookId}`]: { status: 200, body: { page: runbook, space } },
    [`GET /pages/${runbookId}/attachments`]: { status: 200, body: { attachments: [] } },
    [`POST /pages/${runbookId}/visit`]: { status: 204 },
    "GET /notifications/unread-count": () => ({ status: 200, body: { unread: read ? 0 : 1 } }),
    "GET /notifications": (request) => {
      const unread = new URL(request.url).searchParams.get("unread") === "true";
      const all = [read ? { ...fresh, readAt: "2026-10-01T08:00:00Z" } : fresh, old];
      const notifications = unread ? all.filter((n) => n.readAt === null) : all;
      return { status: 200, body: { notifications, total: notifications.length, limit: 50, offset: 0 } };
    },
    "POST /notifications/read": () => {
      read = true;
      return { status: 204 };
    },
  });
  return sent;
}

describe("the Notifications page", () => {
  it("lists what is unread, everything on asking, and marks it all read", async () => {
    const sent = stubNotifications();
    await renderAt("/notifications");
    expect(await screen.findByRole("heading", { name: "Notifications", level: 1 })).toBeInTheDocument();
    const list = await screen.findByRole("list", { name: "Notifications" });
    expect(
      within(list)
        .getAllByRole("button")
        .map((row) => row.getAttribute("data-notification")),
    ).toEqual(["published"]);
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Everything" }));
    await waitFor(() => expect(within(screen.getByRole("list", { name: "Notifications" })).getAllByRole("button")).toHaveLength(2));

    await userEvent.click(screen.getByRole("button", { name: "Mark all as read" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST" && r.path === "/notifications/read")?.body).toEqual({ all: true }));
    await userEvent.click(screen.getByRole("button", { name: "Unread" }));
    expect(await screen.findByText("Nothing unread")).toBeInTheDocument();
  });

  it("opens the page a notification is about, and marks it read", async () => {
    const sent = stubNotifications();
    const router = await renderAt("/notifications");
    const list = await screen.findByRole("list", { name: "Notifications" });
    await userEvent.click(within(list).getByRole("button", { name: /Bob Builder published version 3 of Runbook/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/spaces/DOCS/p/${runbookId}/runbook`));
    expect(sent.find((r) => r.method === "POST" && r.path === "/notifications/read")?.body).toEqual({ ids: [fresh.id] });
  });

  it("is listed in the sidebar with how many are unread, and reached from the bell", async () => {
    stubNotifications();
    const router = await renderAt("/spaces");
    const sidebar = document.querySelector<HTMLElement>("[data-sidebar]")!;
    const entry = await within(sidebar).findByRole("link", { name: /^Notifications\W+1 unread$/ });
    expect(entry).toHaveAttribute("href", "/notifications");

    await userEvent.click(screen.getByRole("button", { name: "Notifications, 1 unread" }));
    await userEvent.click(await screen.findByRole("link", { name: "See all notifications" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/notifications"));
  });
});
