import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ArmatureAccount } from "@/api/armature";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { IssueChart } from "./IssueChart";
import { checkChart } from "./IssueChartDialog";
import { chartSettings, donutPaths, foldSlices, linePoints, percent, readoutAnchor, ticks, type ChartSettings } from "./chart";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const BASE = "https://armature.example.com";
const account = (over: Partial<ArmatureAccount> = {}): Answer => ({
  status: 200,
  body: { account: { configured: true, baseUrl: BASE, connected: true, status: "ok", user: null, checkedAt: null, ...over } },
});
const pie: ChartSettings = { project: "CP", query: "project = CP", chart: "pie", groupBy: "statusCategory", days: 30 };
const flow: ChartSettings = { ...pie, chart: "createdResolved", days: 7 };

function shown(children: ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Report</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

describe("chart settings", () => {
  it("put right what the server would refuse", () => {
    expect(chartSettings({ project: "cp", query: "x", chart: "bar", groupBy: "label", days: 400 })).toEqual({
      project: "",
      query: "x",
      chart: "pie",
      groupBy: "statusCategory",
      days: 30,
    });
    expect(chartSettings({ ...flow })).toEqual(flow);
    expect(checkChart({ ...pie, project: "" })).toEqual({ problems: { project: "Choose the project to count the issues of." } });
    expect("problems" in checkChart({ ...pie, query: " " })).toBe(true);
    expect(checkChart(pie)).toEqual({ settings: pie });
  });
});

describe("drawing", () => {
  it("folds the smallest shares into Other past the most slices, and leaves empty ones out", () => {
    const many = Array.from({ length: 10 }, (_, i) => ({ label: `L${i}`, category: "", count: 10 - i }));
    const folded = foldSlices([...many, { label: "None", category: "", count: 0 }], 8, "Other");
    expect(folded).toHaveLength(8);
    expect(folded[7]).toEqual({ label: "Other", category: "", count: 3 + 2 + 1, other: true });
    expect(folded.reduce((n, s) => n + s.count, 0)).toBe(55);
    expect(foldSlices(many.slice(0, 3), 8)).toHaveLength(3);
  });

  it("cuts a donut into one arc per share, a whole one into two halves", () => {
    const paths = donutPaths([1, 1, 2], 100, 0.5);
    expect(paths).toHaveLength(3);
    expect(paths[0]).toMatch(/^M 50\.00 0\.00 A 50 50 0 0 1 100\.00 50\.00/);
    expect(paths[2]).toContain("A 50 50 0 0 1");
    expect(donutPaths([5], 100, 0.5)[0]!.match(/M /g)).toHaveLength(2);
    expect(donutPaths([0, 0], 100, 0.5)).toEqual(["", ""]);
    expect(percent(1, 3)).toBe(33);
    expect(percent(1, 0)).toBe(0);
  });

  it("puts round ticks from 0 past the highest value, and each day's point in the box", () => {
    expect(ticks(7, 4)).toEqual([0, 2, 4, 6, 8]);
    expect(ticks(130, 4)).toEqual([0, 50, 100, 150]);
    expect(ticks(0, 4)).toEqual([0, 1]);
    expect(ticks(1, 4)).toEqual([0, 1]);
    expect(ticks(3, 4)).toEqual([0, 1, 2, 3]);
    expect(readoutAnchor(10, 300, 120)).toBe("start");
    expect(readoutAnchor(150, 300, 120)).toBe("middle");
    expect(readoutAnchor(290, 300, 120)).toBe("end");
    expect(linePoints([0, 4, 8], 100, 50, 8)).toEqual([
      [0, 50],
      [50, 25],
      [100, 0],
    ]);
  });
});

describe("the chart block", () => {
  it("draws a pie of status categories with a table of every share", async () => {
    const asked: URLSearchParams[] = [];
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/chart": (request) => {
        asked.push(new URL(request.url).searchParams);
        return {
          status: 200,
          body: {
            status: "ok",
            chart: {
              kind: "pie",
              groupBy: "statusCategory",
              total: 4,
              slices: [
                { label: "To do", category: "todo", count: 2 },
                { label: "Done", category: "done", count: 1 },
                { label: "In progress", category: "in_progress", count: 1 },
              ],
              days: [],
              url: `${BASE}/search?q=project+%3D+CP`,
            },
          },
        };
      },
    });
    const user = userEvent.setup();
    const { container } = shown(<IssueChart settings={pie} />);
    const legend = await screen.findByRole("table", { name: "Issues by status category" });
    expect(
      within(legend)
        .getAllByRole("row")
        .map((r) => r.textContent),
    ).toEqual(["Status categoryIssuesShare", "To do250%", "Done125%", "In progress125%"]);
    expect(screen.getByRole("img", { name: /A pie chart of 4 issues by status category/ })).toBeInTheDocument();
    expect(container.querySelector('[data-chart-slice="Done"]')).toHaveAttribute("fill", "var(--color-status-done)");
    expect(String(asked[0])).toBe("project=CP&q=project+%3D+CP&kind=pie&groupBy=statusCategory&days=30");
    await user.hover(container.querySelector('[data-chart-slice="To do"]')!);
    expect(container.querySelector(".doc-chart-hole-label")).toHaveTextContent("50%");
    expect(container.querySelector('[data-chart-slice="Done"]')).toHaveAttribute("data-active", "false");
    expect(screen.getByRole("link", { name: /Open in Armature/ })).toHaveAttribute("href", `${BASE}/search?q=project+%3D+CP`);
    expect(await axeViolations()).toEqual([]);
  });

  it("draws created against resolved, a day at a time from the keyboard, with the counts as a table", async () => {
    const days = ["2026-09-27", "2026-09-28", "2026-09-29", "2026-09-30", "2026-10-01", "2026-10-02", "2026-10-03"].map((day, i) => ({
      day,
      created: i,
      resolved: i % 2,
    }));
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/chart": { status: 200, body: { status: "ok", chart: { kind: "createdResolved", groupBy: "", total: 21, slices: [], days, url: "" } } },
    });
    const user = userEvent.setup();
    const { container } = shown(<IssueChart settings={flow} />);
    const plot = await screen.findByRole("img", { name: /A line chart of the last 7 days: 21 issues created and 3 resolved/ });
    expect(screen.getByText("Created, 21 in all")).toBeInTheDocument();
    plot.focus();
    await user.keyboard("{ArrowLeft}");
    const readout = container.querySelector("[data-chart-readout]");
    expect(readout).toHaveAttribute("data-chart-readout", "2026-10-02");
    expect(readout).toHaveTextContent("5 created");
    expect(readout).toHaveTextContent("1 resolved");
    await user.keyboard("{Home}");
    expect(container.querySelector("[data-chart-readout]")).toHaveAttribute("data-chart-readout", "2026-09-27");
    await user.click(screen.getByText("Show the counts as a table"));
    expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(8);
    expect(await axeViolations()).toEqual([]);
  });

  it("draws a window with only a resolution in it, and calls one with nothing in it empty", async () => {
    const day = (resolved: number) => ({
      kind: "createdResolved",
      groupBy: "",
      total: 0,
      slices: [],
      days: [{ day: "2026-10-03", created: 0, resolved }],
      url: "",
    });
    stubApi({ "GET /armature/account": account(), "GET /armature/chart": { status: 200, body: { status: "ok", chart: day(1) } } });
    const { container } = shown(<IssueChart settings={flow} />);
    await waitFor(() => expect(container.querySelector("[data-armature-chart]")).toHaveAttribute("data-state", "chart"));
    cleanup();
    stubApi({ "GET /armature/account": account(), "GET /armature/chart": { status: 200, body: { status: "ok", chart: day(0) } } });
    const again = shown(<IssueChart settings={flow} />);
    await waitFor(() => expect(again.container.querySelector("[data-armature-chart]")).toHaveAttribute("data-state", "empty"));
    expect(screen.getByText("No issues were created or resolved in the last 7 days.")).toBeInTheDocument();
  });

  it("asks a reader without a token to connect, and says where a query goes wrong", async () => {
    stubApi({ "GET /armature/account": account({ connected: false }) });
    shown(<IssueChart settings={pie} />);
    expect(await screen.findByText(/Connect your Armature account to see this chart/)).toBeInTheDocument();
    cleanup();
    stubApi({
      "GET /armature/account": account(),
      "GET /armature/chart": { status: 422, body: { error: { code: "bad_query", message: "Expected a value after =.", position: 10 } } },
    });
    const { container } = shown(<IssueChart settings={{ ...pie, query: "project =" }} inEditor />);
    await waitFor(() => expect(container.querySelector("[data-armature-chart]")).toHaveAttribute("data-state", "bad_query"));
  });
});
