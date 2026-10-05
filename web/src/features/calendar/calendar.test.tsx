import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { CalendarEvent } from "@/api/calendars";
import { CALENDAR_EVENT_KINDS } from "@/config";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { allowlist } from "@/test/allowlist";
import { CalendarDialog } from "./CalendarDialog";
import { TeamCalendar } from "./TeamCalendar";
import { calendarSettings, draftOf, eventDays, inputOf, localDay, monthKey, monthSpan, monthWeeks, newDraft, onDay, shiftMonth } from "./calendar";

const team = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a90";

function shown(children: ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Plans</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

function anEvent(over: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b01",
    calendarId: team,
    title: "Release",
    kind: "event",
    allDay: true,
    start: "2026-10-15T00:00:00Z",
    end: "2026-10-15T00:00:00Z",
    createdByName: "Ada Lovelace",
    updatedAt: "2026-10-01T09:00:00Z",
    ...over,
  };
}

const calendar = (canEdit: boolean) => ({ id: team, spaceKey: "TEAM", spaceName: "Team", name: "Team plans", canEdit, createdAt: "2026-09-01T09:00:00Z" });

beforeEach(() => {
  // Only the clock is fixed, so the queries' and the user's timers still run.
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date(2026, 9, 14, 10, 0));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("calendar settings and months", () => {
  it("put right what the server would refuse, as the allowlist says", () => {
    expect(calendarSettings({ calendarId: "team", project: "cp" })).toEqual({ calendarId: "", project: null });
    expect(calendarSettings({ calendarId: team, project: "CP" })).toEqual({ calendarId: team, project: "CP" });
    const attrs = allowlist.nodes.calendar?.attrs;
    expect(new RegExp(attrs!.calendarId!.pattern!).test(team)).toBe(true);
    expect(new RegExp(attrs!.project!.pattern!).test("CP")).toBe(true);
    expect(attrs?.project?.nullable).toBe(true);
  });

  it("lays a month out in weeks from Monday, and spans its local midnights", () => {
    const october = { year: 2026, month: 9 };
    const weeks = monthWeeks(october);
    // The first of October 2026 is a Thursday.
    expect(weeks[0]).toEqual([null, null, null, "2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04"]);
    expect(weeks.at(-1)).toEqual(["2026-10-26", "2026-10-27", "2026-10-28", "2026-10-29", "2026-10-30", "2026-10-31", null]);
    expect(weeks.flat().filter(Boolean)).toHaveLength(31);
    expect(monthKey(october)).toBe("2026-10");
    expect(monthKey(shiftMonth(october, 3))).toBe("2027-01");
    expect(monthKey(shiftMonth(october, -10))).toBe("2025-12");
    expect(monthSpan(october)).toEqual({ from: new Date(2026, 9, 1).toISOString(), to: new Date(2026, 10, 1).toISOString() });
  });

  it("puts whole days on their dates and meetings on the reader's days", () => {
    expect(eventDays(anEvent({ end: "2026-10-17T00:00:00Z" }))).toEqual({ first: "2026-10-15", last: "2026-10-17" });
    const start = new Date(2026, 9, 15, 23, 0);
    const meeting = { allDay: false, start: start.toISOString(), end: new Date(2026, 9, 16, 1, 0).toISOString() };
    expect(eventDays(meeting)).toEqual({ first: "2026-10-15", last: "2026-10-16" });
    // One that ends at midnight stays on its own day.
    expect(eventDays({ ...meeting, end: new Date(2026, 9, 16).toISOString() })).toEqual({ first: "2026-10-15", last: "2026-10-15" });
    expect(onDay(meeting, "2026-10-16")).toBe(true);
    expect(onDay(meeting, "2026-10-17")).toBe(false);
  });

  it("turns the dialog's draft into what the API takes, or names the field that is wrong", () => {
    const draft = newDraft("2026-10-15");
    expect(inputOf({ ...draft, title: "  " })).toEqual({ problem: "title" });
    expect(inputOf({ ...draft, title: "Release", endDay: "2026-10-14" })).toEqual({ problem: "end" });
    expect(inputOf({ ...draft, title: " Release ", kind: "absence" })).toEqual({
      input: { title: "Release", kind: "absence", allDay: true, start: "2026-10-15T00:00:00Z", end: "2026-10-15T00:00:00Z" },
    });
    const timed = inputOf({ ...draft, title: "Stand-up", allDay: false });
    expect(timed).toEqual({
      input: { title: "Stand-up", kind: "event", allDay: false, start: new Date(2026, 9, 15, 9).toISOString(), end: new Date(2026, 9, 15, 10).toISOString() },
    });
    expect(inputOf({ ...draft, title: "Stand-up", allDay: false, endTime: "2026-10-15T08:00" })).toEqual({ problem: "end" });
    const back = draftOf(anEvent({ kind: "absence", end: "2026-10-16T00:00:00Z" }));
    expect([back.kind, back.allDay, back.startDay, back.endDay]).toEqual(["absence", true, "2026-10-15", "2026-10-16"]);
    expect(CALENDAR_EVENT_KINDS).toEqual(["event", "absence"]);
  });
});

describe("the team calendar", () => {
  it("shows a month's events beside Armature's due issues, and a reader changes nothing", async () => {
    const asked: string[] = [];
    stubApi({
      [`GET /calendars/${team}/events`]: (request) => {
        asked.push(new URL(request.url).search);
        return {
          status: 200,
          body: {
            calendar: calendar(false),
            events: [
              anEvent(),
              anEvent({
                id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b02",
                title: "Ann away",
                kind: "absence",
                start: "2026-10-28T00:00:00Z",
                end: "2026-10-30T00:00:00Z",
              }),
            ],
            truncated: false,
          },
        };
      },
      "GET /armature/account": {
        status: 200,
        body: { account: { configured: true, baseUrl: "https://armature.test", connected: true, status: "ok", user: null, checkedAt: null } },
      },
      "GET /armature/calendar": {
        status: 200,
        body: {
          status: "ok",
          month: {
            month: "2026-10",
            truncated: false,
            issues: [
              {
                key: "CP-4",
                url: "https://armature.test/issues/CP-4",
                summary: "Rotate the signing keys",
                from: "2026-10-15",
                to: "2026-10-15",
                done: false,
                category: "todo",
              },
            ],
          },
        },
      },
    });
    shown(<TeamCalendar settings={{ calendarId: team, project: "CP" }} />);
    const block = await screen.findByRole("figure", { name: "Team plans" });
    await waitFor(() => expect(block).toHaveAttribute("data-state", "month"));
    expect(new URLSearchParams(asked[0])).toEqual(new URLSearchParams({ from: new Date(2026, 9, 1).toISOString(), to: new Date(2026, 10, 1).toISOString() }));
    const fifteenth = block.querySelector('[data-day="2026-10-15"]') as HTMLElement;
    await within(fifteenth).findByRole("link", { name: /CP-4/ });
    expect(
      within(fifteenth)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["Release", "Due: CP-4 Rotate the signing keys"]);
    for (const day of ["2026-10-28", "2026-10-29", "2026-10-30"]) expect(block.querySelector(`[data-day="${day}"]`)).toHaveTextContent("Away: Ann away");
    expect(block.querySelector('[data-day="2026-10-14"]')).toHaveAttribute("data-today", "true");
    expect(within(block).queryByRole("button", { name: /Edit|Add/ })).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("goes from month to month, and asks Armature for each", async () => {
    const user = userEvent.setup();
    const months: string[] = [];
    stubApi({
      [`GET /calendars/${team}/events`]: { status: 200, body: { calendar: calendar(false), events: [], truncated: false } },
      "GET /armature/account": {
        status: 200,
        body: { account: { configured: true, baseUrl: "https://armature.test", connected: true, status: "ok", user: null, checkedAt: null } },
      },
      "GET /armature/calendar": (request) => {
        months.push(new URL(request.url).searchParams.get("month")!);
        return { status: 200, body: { status: "ok", month: { month: "2026-11", truncated: false, issues: [] } } };
      },
    });
    shown(<TeamCalendar settings={{ calendarId: team, project: "CP" }} />);
    const block = await screen.findByRole("figure", { name: "Team plans" });
    await user.click(within(block).getByRole("button", { name: "Next month" }));
    await waitFor(() => expect(block.querySelector("[data-calendar-month]")).toHaveAttribute("data-calendar-month", "2026-11"));
    expect(await within(block).findByText("Nothing this month.")).toBeInTheDocument();
    await waitFor(() => expect(months).toEqual(["2026-10", "2026-11"]));
    await user.click(within(block).getByRole("button", { name: "This month" }));
    expect(block.querySelector("[data-calendar-month]")).toHaveAttribute("data-calendar-month", "2026-10");
  });

  it("lets whoever may change the calendar add an event on a day, change one, and delete one", async () => {
    const user = userEvent.setup();
    const sent = stubApi({
      [`GET /calendars/${team}/events`]: { status: 200, body: { calendar: calendar(true), events: [anEvent()], truncated: false } },
      "GET /armature/account": {
        status: 200,
        body: { account: { configured: false, baseUrl: null, connected: false, status: "not_configured", user: null, checkedAt: null } },
      },
      [`POST /calendars/${team}/events`]: { status: 201, body: { event: anEvent({ id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b03", title: "Ann away" }) } },
      [`PUT /calendars/${team}/events/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b01`]: {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { end: "An event lasts at most 366 days. Split a longer one into several." },
          },
        },
      },
      [`DELETE /calendars/${team}/events/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b01`]: { status: 204 },
    });
    shown(<TeamCalendar settings={calendarSettings({ calendarId: team, project: null })} />);
    const block = await screen.findByRole("figure", { name: "Team plans" });
    const twentieth = await waitFor(() => block.querySelector('[data-day="2026-10-20"]') as HTMLElement);
    await user.click(within(twentieth).getByRole("button", { name: /Add an event on Tuesday, October 20, 2026/ }));
    let dialog = await screen.findByRole("dialog", { name: "Add an event" });
    await user.click(within(dialog).getByRole("button", { name: "Add" }));
    expect(within(dialog).getByText("Give the event a title, such as Release or Ann on holiday.")).toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Title"), "Ann away");
    await user.selectOptions(within(dialog).getByLabelText("Kind"), "absence");
    // A date field takes its day whole, as the browser's picker gives it.
    fireEvent.change(within(dialog).getByLabelText("Last day"), { target: { value: "2026-10-23" } });
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Add" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(sent.find((each) => each.method === "POST")?.body).toEqual({
      title: "Ann away",
      kind: "absence",
      allDay: true,
      start: "2026-10-20T00:00:00Z",
      end: "2026-10-23T00:00:00Z",
    });

    await user.click(within(block).getByRole("button", { name: "Edit Release" }));
    dialog = await screen.findByRole("dialog", { name: "Edit the event" });
    expect(within(dialog).getByLabelText("Title")).toHaveValue("Release");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    // The server's sentence goes under the field it names.
    expect(await within(dialog).findByText("An event lasts at most 366 days. Split a longer one into several.")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Delete event" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(sent.some((each) => each.method === "DELETE")).toBe(true);
  });

  it("says when its calendar is gone, and when no calendar is named", async () => {
    stubApi({
      [`GET /calendars/${team}/events`]: { status: 404, body: { error: { code: "not_found", message: "That calendar was not found." } } },
      "GET /armature/account": {
        status: 200,
        body: { account: { configured: false, baseUrl: null, connected: false, status: "not_configured", user: null, checkedAt: null } },
      },
    });
    const { container } = shown(<TeamCalendar settings={{ calendarId: team, project: null }} />);
    await waitFor(() => expect(container.querySelector("[data-team-calendar]")).toHaveAttribute("data-state", "missing"));
    cleanup();
    const again = shown(<TeamCalendar settings={calendarSettings({})} />);
    expect(again.container.querySelector("[data-team-calendar]")).toHaveAttribute("data-state", "none");
  });

  it("asks a reader without an Armature token to connect, and still shows the events", async () => {
    stubApi({
      [`GET /calendars/${team}/events`]: { status: 200, body: { calendar: calendar(false), events: [anEvent()], truncated: false } },
      "GET /armature/account": {
        status: 200,
        body: { account: { configured: true, baseUrl: "https://armature.test", connected: false, status: "not_connected", user: null, checkedAt: null } },
      },
    });
    shown(<TeamCalendar settings={{ calendarId: team, project: "CP" }} />);
    const block = await screen.findByRole("figure", { name: "Team plans" });
    expect(await within(block).findByText(/Connect your Armature account to see the issues due this month/)).toBeInTheDocument();
    expect(block.querySelector('[data-day="2026-10-15"]')).toHaveTextContent("Release");
  });
});

describe("the calendar dialog", () => {
  it("picks one of the space's calendars, or names a new one, and an Armature project", async () => {
    const user = userEvent.setup();
    const sent = stubApi({
      "GET /spaces": {
        status: 200,
        body: {
          spaces: [
            { key: "TEAM", name: "Team" },
            { key: "OPS", name: "Operations" },
          ],
        },
      },
      "GET /spaces/TEAM/calendars": { status: 200, body: { calendars: [calendar(true)] } },
      "GET /spaces/OPS/calendars": { status: 200, body: { calendars: [] } },
      "POST /spaces/OPS/calendars": {
        status: 201,
        body: { calendar: { ...calendar(true), id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a91", spaceKey: "OPS", name: "On call" } },
      },
      "GET /armature/account": {
        status: 200,
        body: { account: { configured: true, baseUrl: "https://armature.test", connected: true, status: "ok", user: null, checkedAt: null } },
      },
      "GET /armature/projects": { status: 200, body: { status: "ok", projects: [{ key: "CP", name: "Core platform", canCreate: true }] } },
    });
    const onSave = vi.fn();
    shown(<CalendarDialog initial={calendarSettings({})} spaceKey="TEAM" isNew onSave={onSave} onClose={() => {}} />);
    const dialog = await screen.findByRole("dialog", { name: "Insert a calendar" });
    await within(dialog).findByRole("option", { name: "Team plans" });
    expect(within(dialog).getByLabelText("Calendar")).toHaveValue(team);
    await within(dialog).findByRole("option", { name: "Core platform (CP)" });
    await user.selectOptions(within(dialog).getByLabelText("Armature issues"), "CP");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(onSave).toHaveBeenCalledWith({ calendarId: team, project: "CP" });

    await user.selectOptions(within(dialog).getByLabelText("Space"), "OPS");
    await waitFor(() => expect(within(dialog).getByLabelText("Calendar")).toHaveValue("new"));
    await user.selectOptions(within(dialog).getByLabelText("Armature issues"), "");
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(within(dialog).getByText("Name the calendar, such as Team or Releases.")).toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Name of the new calendar"), "On call");
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    await waitFor(() => expect(onSave).toHaveBeenLastCalledWith({ calendarId: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a91", project: null }));
    expect(sent.find((each) => each.method === "POST")?.body).toEqual({ name: "On call" });
  });
});

it("reads a day in the reader's own zone", () => {
  expect(localDay(new Date(2026, 0, 2, 23, 59))).toBe("2026-01-02");
});
