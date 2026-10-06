import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { PageSchedule } from "@/api/versions";
import { collabTransport } from "@/features/collab/transport";
import { arrival, renderAt, stubApi, type Answer } from "@/test/app";
import { aPage, aSpace } from "@/test/spaces";
import { defaultScheduleTime, localInputValue, parseLocalInput } from "./schedule";

// The editor's chunk loads and the editor mounts in jsdom, which is slow on a busy machine.
const EDITOR_TEST_MS = 20_000;

const space = aSpace();
const home = aPage();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000d4";
const PATH = `/s/DOCS/p/${PAGE_ID}/news`;
const news = aPage({
  id: PAGE_ID,
  title: "News",
  home: false,
  parentId: home.id,
  ancestors: [{ id: home.id, title: "Handbook", home: true }],
  version: 2,
});
const draft = { pageId: PAGE_ID, title: "News", body: news.body, baseVersion: 2, updatedAt: "2026-10-06T09:00:00Z" };
const waiting: PageSchedule = {
  publishAt: "2026-10-12T07:00:00Z",
  authorId: "0195f000-0000-7000-8000-0000000000a1",
  authorName: "Ann Planner",
  mine: true,
  comment: "Launch",
  notifyWatchers: true,
  createdAt: "2026-10-06T09:00:00Z",
  failure: null,
  failedAt: null,
};
const was = { ...collabTransport };

afterEach(() => {
  Object.assign(collabTransport, was);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function stubPage(page = news, more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    [`GET /pages/${PAGE_ID}`]: { status: 200, body: { page, space } },
    [`GET /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft: page.draft ? draft : null } },
    ...more,
  });
}

describe("the times a schedule offers and reads", () => {
  it("offers the next full hour at least an hour ahead, on the reader's clock", () => {
    const offered = defaultScheduleTime(new Date(2026, 9, 6, 14, 20, 5));
    expect(localInputValue(offered)).toBe("2026-10-06T16:00");
    expect(localInputValue(defaultScheduleTime(new Date(2026, 9, 6, 14, 0, 0)))).toBe("2026-10-06T15:00");
  });

  it("reads a datetime field in the reader's zone, and nothing else as a time", () => {
    expect(parseLocalInput("2026-10-12T09:30")?.getTime()).toBe(new Date(2026, 9, 12, 9, 30).getTime());
    expect(parseLocalInput("")).toBeNull();
    expect(parseLocalInput("next week")).toBeNull();
  });
});

describe("a page with a publish scheduled", () => {
  it("tells its author when the draft goes out, and lets them call it off", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const sent = stubPage(aPage({ ...news, draft: { baseVersion: 2, updatedAt: draft.updatedAt }, schedule: waiting }), {
      [`DELETE /pages/${PAGE_ID}/schedule`]: { status: 204 },
    });
    const router = await renderAt(PATH);
    await arrival(router, PATH);
    await screen.findByRole("button", { name: "Cancel schedule" });
    const note = document.querySelector("[data-schedule-note]");
    expect(note).toHaveAttribute("data-schedule-note", "waiting");
    expect(note).toHaveTextContent(/^Your draft is scheduled to publish on .*2026/);
    expect(within(note as HTMLElement).getByRole("button", { name: "Change" })).toBeInTheDocument();
    expect(document.querySelector("[data-draft-note]")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Cancel schedule" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === `/pages/${PAGE_ID}/schedule`)).toBe(true));
  });

  it("names whose it is to another editor, and why it did not go out", async () => {
    stubPage(aPage({ ...news, schedule: { ...waiting, mine: false, failure: "conflict", failedAt: "2026-10-12T07:00:05Z" } }));
    const router = await renderAt(PATH);
    await arrival(router, PATH);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveAttribute("data-schedule-note", "conflict");
    expect(alert).toHaveTextContent(/^The draft Ann Planner scheduled to publish on .* did not go out: somebody published the page after the draft began\./);
    expect(within(alert).queryByRole("button", { name: /schedule again|Change/ })).toBeNull();
    expect(within(alert).getByRole("button", { name: "Cancel schedule" })).toBeInTheDocument();
  });

  it("shows a reader nothing", async () => {
    stubPage(aPage({ ...news, can: { ...news.can, edit: false } }));
    const router = await renderAt(PATH);
    await arrival(router, PATH);
    await screen.findByRole("heading", { level: 1, name: "News" });
    expect(document.querySelector("[data-schedule-note]")).toBeNull();
  });
});

describe("scheduling from the editor", () => {
  it(
    "sends the draft's time as an instant, and refuses a time past",
    async () => {
      collabTransport.enabled = false;
      const sent = stubPage(aPage({ ...news, draft: { baseVersion: 2, updatedAt: draft.updatedAt } }), {
        [`PUT /pages/${PAGE_ID}/schedule`]: { status: 200, body: { schedule: waiting } },
      });
      await renderAt(`${PATH}/edit`);
      await screen.findByLabelText("Title");
      await userEvent.click(screen.getByRole("button", { name: "Publish" }));
      const dialog = await screen.findByRole("dialog", { name: "Publish News" });
      expect(within(dialog).getByLabelText("Now")).toBeChecked();
      await userEvent.click(within(dialog).getByLabelText("At a set time"));
      const at = within(dialog).getByLabelText("Publish at");
      expect(within(dialog).getByText(/^In your time zone, /)).toBeInTheDocument();

      fireEvent.change(at, { target: { value: "2020-01-01T09:00" } });
      await userEvent.click(within(dialog).getByRole("button", { name: "Schedule" }));
      expect(await within(dialog).findByText("Choose a time ahead to publish at, or publish now.")).toBeInTheDocument();
      expect(sent.some((r) => r.path === `/pages/${PAGE_ID}/schedule`)).toBe(false);

      const when = new Date(Date.now() + 3 * 24 * 60 * 60 * 1000);
      when.setSeconds(0, 0);
      fireEvent.change(at, { target: { value: localInputValue(when) } });
      await userEvent.type(within(dialog).getByLabelText(/What changed/), "Launch");
      await userEvent.click(within(dialog).getByRole("button", { name: "Schedule" }));
      await waitFor(() => expect(sent.some((r) => r.method === "PUT" && r.path === `/pages/${PAGE_ID}/schedule`)).toBe(true));
      const asked = sent.find((r) => r.method === "PUT" && r.path === `/pages/${PAGE_ID}/schedule`)!.body;
      expect(asked).toEqual({ publishAt: when.toISOString(), comment: "Launch", notifyWatchers: true });
      expect(sent.some((r) => r.path === `/pages/${PAGE_ID}/publish`)).toBe(false);
    },
    EDITOR_TEST_MS,
  );
});
