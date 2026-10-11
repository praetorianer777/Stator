import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { RouterProvider, createMemoryHistory, createRootRoute, createRouter } from "@tanstack/react-router";
import type { Task } from "@/api/tasks";
import { TASK_REPORT_DUES, TASK_REPORT_MAX_LIMIT, TASK_REPORT_STATES } from "@/config";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { allowlist } from "@/test/allowlist";
import { TaskReport } from "./TaskReport";
import { TaskReportDialog } from "./TaskReportDialog";
import { taskReportSettings } from "./report";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

// A router of one route, so the report's links to pages have one to resolve against.
function shown(children: ReactNode) {
  const root = createRootRoute({
    component: () => (
      <main>
        <h1>Overview</h1>
        {children}
      </main>
    ),
  });
  const router = createRouter({ routeTree: root, history: createMemoryHistory({ initialEntries: ["/"] }) });
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

const minutes = "0195f000-0000-7000-8000-0000000000d1";
const ada = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01";

function aTask(over: Partial<Task> = {}): Task {
  return {
    id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b01",
    page: { id: minutes, title: "Minutes", spaceKey: "TEAM", spaceName: "Team" },
    path: "/s/TEAM/p/0195f000-0000-7000-8000-0000000000d1#task-0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b01",
    text: "Send the notes",
    done: false,
    dueOn: "2020-01-02",
    assigneeId: ada,
    assigneeName: "Ada Lovelace",
    assignedByName: "Grace Hopper",
    assignedAt: "2019-12-30T09:00:00Z",
    doneAt: null,
    canEdit: true,
    ...over,
  };
}

describe("task report settings", () => {
  it("put right what the server would refuse, as the allowlist says", () => {
    expect(taskReportSettings({ space: "team", assignee: "Ada", due: "later", state: "half", limit: TASK_REPORT_MAX_LIMIT + 1 })).toEqual({
      space: null,
      assignee: null,
      due: "any",
      state: "open",
      limit: 20,
    });
    expect(taskReportSettings({ space: "TEAM", assignee: ada, due: "week", state: "all", limit: 5 })).toEqual({
      space: "TEAM",
      assignee: ada,
      due: "week",
      state: "all",
      limit: 5,
    });
    const attrs = allowlist.nodes.taskReport?.attrs;
    expect(attrs?.due?.enum).toEqual([...TASK_REPORT_DUES]);
    expect(attrs?.state?.enum).toEqual([...TASK_REPORT_STATES]);
    expect(attrs?.limit?.max).toBe(TASK_REPORT_MAX_LIMIT);
    for (const assignee of ["me", "none", ada]) expect(new RegExp(attrs!.assignee!.pattern!).test(assignee)).toBe(true);
  });
});

describe("the task report", () => {
  it("asks for its filter, lists the tasks with their page, assignee and day, and ticks one off", async () => {
    const user = userEvent.setup();
    const asked: URLSearchParams[] = [];
    const sent = stubApi({
      "GET /task-report": (request) => {
        asked.push(new URL(request.url).searchParams);
        const done = asked.length > 1;
        return {
          status: 200,
          body: {
            tasks: [
              aTask({ done, doneAt: done ? "2026-10-02T09:00:00Z" : null }),
              aTask({ id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b02", text: "Book a room", assigneeId: null, assigneeName: "", dueOn: null }),
            ],
            assigneeName: "Ada Lovelace",
            truncated: true,
          },
        };
      },
      [`PATCH /pages/${minutes}/tasks/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b01`]: { status: 200, body: { task: aTask({ done: true }) } },
    });
    shown(<TaskReport settings={{ space: "TEAM", assignee: ada, due: "overdue", state: "all", limit: 2 }} />);
    const report = await screen.findByRole("region", { name: "Tasks in TEAM, assigned to Ada Lovelace, overdue" });
    const rows = await within(report).findAllByRole("listitem");
    const first = rows[0]!;
    expect(String(asked[0])).toBe(`space=TEAM&assignee=${ada}&due=overdue&state=all&limit=2`);
    expect(rows.map((li) => li.getAttribute("data-reported-task"))).toEqual(["Send the notes", "Book a room"]);
    expect(rows[0]).toHaveTextContent(/Minutes.*Team.*Ada Lovelace.*Overdue/);
    expect(rows[1]).toHaveTextContent(/Nobody assigned/);
    expect(within(first).getByRole("link", { name: "Minutes" })).toBeInTheDocument();
    expect(within(report).getByText("Showing the first 2 tasks. Narrow the report to see the rest.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await user.click(within(first).getByRole("checkbox", { name: "Done: Send the notes" }));
    await waitFor(() => expect(sent.some((each) => each.method === "PATCH")).toBe(true));
    // A tick asks the report again, so it shows what the page now says.
    await waitFor(() => expect(asked.length).toBe(2));
    await waitFor(() => expect(within(report).getAllByRole("listitem")[0]).toHaveAttribute("data-task-done", "true"));
  });

  it("in the editor neither ticks nor links, and says when nothing matches", async () => {
    stubApi({ "GET /task-report": { status: 200, body: { tasks: [aTask()], assigneeName: "", truncated: false } } });
    shown(<TaskReport settings={taskReportSettings({ assignee: "me", due: "week" })} inEditor />);
    const report = await screen.findByRole("region", { name: "Open tasks, assigned to you, due in the next 7 days" });
    const row = await within(report).findByRole("listitem");
    expect(within(row).queryByRole("checkbox")).toBeNull();
    expect(within(row).queryByRole("link")).toBeNull();
    cleanup();
    stubApi({ "GET /task-report": { status: 200, body: { tasks: [], assigneeName: "", truncated: false } } });
    shown(<TaskReport settings={taskReportSettings({ state: "done", assignee: "none", due: "none" })} />);
    expect(await screen.findByText("No task on a page you can read matches this report.")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Done tasks, assigned to nobody, without a due date" })).toHaveAttribute("data-state", "empty");
  });
});

describe("the task report dialog", () => {
  it("asks for a space, an assignee, a due day, a state and a length", async () => {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [{ key: "TEAM", name: "Team" }] } },
      "GET /people": { status: 200, body: { people: [{ id: ada, name: "Ada Lovelace", email: "ada@example.test" }] } },
    });
    const user = userEvent.setup();
    const onSave = vi.fn();
    shown(<TaskReportDialog initial={taskReportSettings({})} isNew onSave={onSave} onClose={() => {}} />);
    const dialog = await screen.findByRole("dialog", { name: "Insert a task report" });
    await within(dialog).findByRole("option", { name: "Team (TEAM)" });
    await within(dialog).findByRole("option", { name: "Ada Lovelace" });
    await user.selectOptions(within(dialog).getByLabelText("Space"), "TEAM");
    await user.selectOptions(within(dialog).getByLabelText("Assigned to"), ada);
    await user.selectOptions(within(dialog).getByLabelText("Due"), "today");
    await user.selectOptions(within(dialog).getByLabelText("State"), "all");
    await user.selectOptions(within(dialog).getByLabelText("Show"), "50");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(onSave).toHaveBeenCalledWith({ space: "TEAM", assignee: ada, due: "today", state: "all", limit: 50 });
  });

  it("keeps a person the people offered leave out, by the name the report gave", async () => {
    const gone = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a09";
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [] } }, "GET /people": { status: 200, body: { people: [] } } });
    const user = userEvent.setup();
    const onSave = vi.fn();
    shown(<TaskReportDialog initial={taskReportSettings({ assignee: gone })} assigneeName="Old Hand" isNew={false} onSave={onSave} onClose={() => {}} />);
    const dialog = await screen.findByRole("dialog", { name: "Edit the task report" });
    expect(within(dialog).getByLabelText("Assigned to")).toHaveDisplayValue("Old Hand");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(onSave).toHaveBeenCalledWith({ space: null, assignee: gone, due: "any", state: "open", limit: 20 }));
  });
});
