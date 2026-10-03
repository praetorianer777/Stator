import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ArmatureAccount } from "@/api/armature";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { allowlist } from "@/test/allowlist";
import { ARMATURE_ROADMAP_GROUPINGS } from "@/config";
import { IssueRoadmap, barWords } from "./IssueRoadmap";
import { IssueRoadmapDialog, checkRoadmap } from "./IssueRoadmapDialog";
import { axisOf, axisTicks, dayNumber, labelEvery, newRoadmapSettings, place, roadmapSettings, type RoadmapSettings } from "./roadmap";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const BASE = "https://armature.example.com";
const account = (over: Partial<ArmatureAccount> = {}): Answer => ({
  status: 200,
  body: { account: { configured: true, baseUrl: BASE, connected: true, status: "ok", user: null, checkedAt: null, ...over } },
});
const byEpic: RoadmapSettings = { project: "CP", query: "project = CP", groupBy: "epic" };

function shown(children: ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Plan</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

const bar = (key: string, summary: string, start: string | null, due: string | null, category: "todo" | "in_progress" | "done" = "todo", derived = false) => ({
  key,
  url: `${BASE}/issues/${key}`,
  summary,
  type: { id: "00000000-0000-0000-0000-000000000000", name: "Story", icon: "story", level: 0 },
  status: { name: { todo: "To do", in_progress: "In progress", done: "Done" }[category]!, category },
  start,
  due,
  derived,
});

describe("roadmap settings", () => {
  it("put right what the server would refuse, as the allowlist says", () => {
    expect(roadmapSettings({ project: "cp", query: 4, groupBy: "sprint" })).toEqual({ project: "", query: "", groupBy: "epic" });
    expect(roadmapSettings({ ...byEpic, groupBy: "team" })).toEqual({ ...byEpic, groupBy: "team" });
    expect(allowlist.nodes.armatureRoadmap?.attrs?.groupBy?.enum).toEqual([...ARMATURE_ROADMAP_GROUPINGS]);
    expect(newRoadmapSettings("CP").query).toBe("project = CP AND statusCategory != done");
    expect(checkRoadmap({ ...byEpic, project: "" })).toEqual({ problems: { project: "Choose the project to draw the roadmap of." } });
    expect("problems" in checkRoadmap({ ...byEpic, query: "  " })).toBe(true);
    expect(checkRoadmap(byEpic)).toEqual({ settings: byEpic });
  });
});

describe("the timeline", () => {
  it("places a bar from the start of its first day to the end of its last, and a lone day as a point", () => {
    const axis = { first: dayNumber("2026-10-01"), last: dayNumber("2026-10-10") };
    expect(place("2026-10-01", "2026-10-10", axis)).toEqual({ left: 0, width: 100, shape: "bar" });
    expect(place("2026-10-03", "2026-10-03", axis)).toEqual({ left: 20, width: 10, shape: "bar" });
    // A due day before its start is drawn as the one day.
    expect(place("2026-10-05", "2026-10-02", axis)?.width).toBe(10);
    expect(place(null, "2026-10-10", axis)).toEqual({ left: 95, width: 0, shape: "due" });
    expect(place("2026-10-01", null, axis)?.shape).toBe("start");
    expect(place(null, null, axis)).toBeNull();
    expect(axisOf("2026-10-01", "2026-10-10", 3)).toEqual({ first: axis.first - 3, last: axis.last + 3 });
  });

  it("marks months on a long axis and Mondays on a short one", () => {
    const long = axisTicks({ first: dayNumber("2026-01-20"), last: dayNumber("2026-05-10") });
    expect(long.map((tick) => tick.day)).toEqual(["2026-02-01", "2026-03-01", "2026-04-01", "2026-05-01"]);
    expect(long.every((tick) => tick.month && tick.at > 0 && tick.at < 100)).toBe(true);
    const short = axisTicks({ first: dayNumber("2026-10-01"), last: dayNumber("2026-10-20") });
    expect(short.map((tick) => tick.day)).toEqual(["2026-10-05", "2026-10-12", "2026-10-19"]);
    // A week is 35% of this axis: 350 pixels wide has room for each label, 100 for every other one.
    expect(labelEvery(short, 350, 64)).toBe(1);
    expect(labelEvery(short, 100, 64)).toBe(2);
    expect(labelEvery(short.slice(0, 1), 100, 64)).toBe(1);
  });

  it("says a bar's days in words, and that an epic's came from its issues", () => {
    expect(barWords(bar("CP-1", "x", "2026-10-01", "2026-10-09", "in_progress"))).toBe("Oct 1, 2026 to Oct 9, 2026, In progress");
    expect(barWords(bar("CP-2", "x", null, "2026-10-15", "todo", true))).toBe("due Oct 15, 2026, no start date, taken from its issues, To do");
  });
});

describe("the roadmap block", () => {
  it("draws each epic with its issues under it, the rest last, and says what it left out", async () => {
    vi.useFakeTimers({ now: new Date("2026-10-03T12:00:00Z"), toFake: ["Date"] });
    const asked: URLSearchParams[] = [];
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/roadmap": (request) => {
        asked.push(new URL(request.url).searchParams);
        return {
          status: 200,
          body: {
            status: "ok",
            roadmap: {
              groupBy: "epic",
              from: "2026-09-01",
              to: "2026-10-15",
              groups: [
                {
                  name: "Launch the beta",
                  epic: {
                    ...bar("CP-6", "Launch the beta", "2026-09-01", "2026-10-15", "in_progress", true),
                    type: { id: "x", name: "Epic", icon: "epic", level: 1 },
                  },
                  rows: [bar("CP-1", "Set up the build", "2026-09-01", "2026-09-20", "done"), bar("CP-4", "Rotate the keys", "2026-10-01", null)],
                },
                { name: "", epic: null, rows: [bar("CP-2", "Fix sign-in", null, "2026-09-30", "in_progress")] },
              ],
              unscheduled: 2,
              hidden: 0,
              url: `${BASE}/search?q=project+%3D+CP`,
            },
          },
        };
      },
    });
    const { container } = shown(<IssueRoadmap settings={byEpic} />);
    const epic = await screen.findByRole("region", { name: "Launch the beta" });
    expect(String(asked[0])).toBe("project=CP&q=project+%3D+CP&groupBy=epic");
    expect(
      within(epic)
        .getAllByRole("listitem")
        .map((li) => li.getAttribute("data-roadmap-row")),
    ).toEqual(["CP-1", "CP-4"]);
    expect(within(epic).getByRole("link", { name: "CP-6" })).toHaveAttribute("href", `${BASE}/issues/CP-6`);
    expect(within(epic).getByText(/Sep 1, 2026 to Oct 15, 2026, taken from its issues/)).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Not in an epic" })).toHaveTextContent("Fix sign-in");
    expect(container.querySelector('[data-roadmap-row="CP-1"] .doc-roadmap-bar')).toHaveAttribute("data-category", "done");
    expect(container.querySelector('[data-roadmap-row="CP-4"] .doc-roadmap-bar')).toHaveAttribute("data-shape", "start");
    expect(container.querySelector('[data-roadmap-group="CP-6"] .doc-roadmap-head .doc-roadmap-bar')).toHaveAttribute("data-derived", "true");
    // Today, the 3rd of October, is marked on every track.
    expect(container.querySelectorAll(".doc-roadmap-today").length).toBe(5);
    expect(screen.getByText("Dates taken from its issues")).toBeInTheDocument();
    expect(screen.getByText("2 more issues have no start or due date, so they are not drawn.")).toBeInTheDocument();
    expect(screen.getByText("CP roadmap, by epic")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("names each team, the issues without one last, and leaves links out in the editor", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/roadmap": {
        status: 200,
        body: {
          status: "ok",
          roadmap: {
            groupBy: "team",
            from: "2026-09-01",
            to: "2026-09-20",
            groups: [
              { name: "Platform", epic: null, rows: [bar("CP-1", "Set up the build", "2026-09-01", "2026-09-20")] },
              { name: "", epic: null, rows: [bar("CP-2", "Fix sign-in", "2026-09-02", "2026-09-03")] },
            ],
            unscheduled: 0,
            hidden: 3,
            url: "",
          },
        },
      },
    });
    shown(<IssueRoadmap settings={{ ...byEpic, groupBy: "team" }} inEditor />);
    expect(await screen.findByRole("region", { name: "Platform" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "No team" })).toHaveTextContent("CP-2");
    expect(screen.queryByRole("link", { name: "CP-1" })).toBeNull();
    expect(screen.getByText("3 more issues are left out; narrow the query to see them.")).toBeInTheDocument();
  });

  it("says when nothing has a day to draw, asks a reader without a token to connect, and marks a bad query", async () => {
    const empty = (unscheduled: number): Answer => ({
      status: 200,
      body: { status: "ok", roadmap: { groupBy: "epic", from: null, to: null, groups: [], unscheduled, hidden: 0, url: "" } },
    });
    stubApi({ "GET /armature/account": account(), "GET /armature/roadmap": empty(1) });
    shown(<IssueRoadmap settings={byEpic} />);
    expect(await screen.findByText(/The 1 issue this query finds has no start or due date/)).toBeInTheDocument();
    cleanup();
    stubApi({ "GET /armature/account": account(), "GET /armature/roadmap": empty(0) });
    shown(<IssueRoadmap settings={byEpic} />);
    expect(await screen.findByText("No issues match this query.")).toBeInTheDocument();
    cleanup();
    stubApi({ "GET /armature/account": account({ connected: false }) });
    shown(<IssueRoadmap settings={byEpic} />);
    expect(await screen.findByText(/Connect your Armature account to see this roadmap/)).toBeInTheDocument();
    cleanup();
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/roadmap": { status: 422, body: { error: { code: "bad_query", message: "Expected a value after =.", position: 10 } } },
    });
    const { container } = shown(<IssueRoadmap settings={{ ...byEpic, query: "project =" }} inEditor />);
    await waitFor(() => expect(container.querySelector("[data-armature-roadmap]")).toHaveAttribute("data-state", "bad_query"));
  });
});

describe("the roadmap dialog", () => {
  it("fills a new roadmap's query from the project and saves the grouping chosen", async () => {
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/projects": { status: 200, body: { status: "ok", projects: [{ key: "CP", name: "Core platform", canCreate: true }] } },
    });
    const user = userEvent.setup();
    const onSave = vi.fn();
    shown(<IssueRoadmapDialog initial={newRoadmapSettings()} isNew onSave={onSave} onClose={() => {}} />);
    const dialog = await screen.findByRole("dialog", { name: "Insert an Armature roadmap" });
    await waitFor(() => expect(within(dialog).getByLabelText("Query")).toHaveValue("project = CP AND statusCategory != done"));
    await user.click(within(dialog).getByLabelText(/^Team/));
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(onSave).toHaveBeenCalledWith({ project: "CP", query: "project = CP AND statusCategory != done", groupBy: "team" });
  });
});
