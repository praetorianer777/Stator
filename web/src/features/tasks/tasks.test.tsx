import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor } from "@tiptap/core";
import type { Task } from "@/api/tasks";
import { TASKS_PAGE_SIZE } from "@/config";
import { DocPageContext } from "@/features/editor/BlockViews";
import { DocView } from "@/features/editor/DocView";
import { editorExtensions } from "@/features/editor/extensions";
import type { Doc, DocNode } from "@/features/editor/schema";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { dueState, taskOfItem, utcToday } from "./taskItem";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

const minutes = "0195f000-0000-7000-8000-0000000000d1";
const ada = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01";
const notes = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b01";
const room = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b02";

function aTask(over: Partial<Task> = {}): Task {
  return {
    id: notes,
    page: { id: minutes, title: "Minutes", spaceKey: "TEAM", spaceName: "Team" },
    text: "Send the notes @Ada 2026-10-01",
    done: false,
    dueOn: "2026-10-01",
    assigneeId: ada,
    assigneeName: "Ada Lovelace",
    assignedByName: "Grace Hopper",
    assignedAt: "2026-09-30T09:00:00Z",
    doneAt: null,
    canEdit: true,
    ...over,
  };
}

function item(attrs: Record<string, unknown>, inline: DocNode[], nested?: DocNode[]): DocNode {
  const content: DocNode[] = [{ type: "paragraph", content: inline }];
  if (nested) content.push({ type: "taskList", content: nested });
  return { type: "taskItem", attrs, content };
}

describe("reading a checklist item as a task", () => {
  it("takes the first person and the first date of its own words, not of the items inside it", () => {
    const got = taskOfItem(
      item(
        { checked: false, taskId: notes },
        [
          { type: "text", text: "Send " },
          { type: "mention", attrs: { id: ada, label: "Ada" } },
          { type: "date", attrs: { date: "2026-10-09" } },
          { type: "date", attrs: { date: "2026-10-30" } },
        ],
        [item({ checked: false }, [{ type: "mention", attrs: { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a02", label: "Alan" } }])],
      ),
    );
    expect(got).toEqual({ id: notes, done: false, assigneeId: ada, due: "2026-10-09" });
    expect(taskOfItem(item({ checked: true, taskId: "t1" }, [{ type: "date", attrs: { date: "2026-02-30" } }]))).toEqual({
      id: null,
      done: true,
      assigneeId: null,
      due: null,
    });
  });

  it("says how a task stands against its day, in UTC", () => {
    expect(utcToday(new Date("2026-10-02T23:30:00-02:00"))).toBe("2026-10-03");
    expect(dueState("2026-10-01", false, "2026-10-02")).toBe("overdue");
    expect(dueState("2026-10-02", false, "2026-10-02")).toBe("today");
    expect(dueState("2026-10-03", false, "2026-10-02")).toBe("upcoming");
    expect(dueState("2026-10-01", true, "2026-10-02")).toBeNull();
    expect(dueState(null, false, "2026-10-02")).toBeNull();
  });
});

describe("a task in the editor", () => {
  it("keeps its id from the document, and a new item from Enter starts without one", () => {
    const editor = new Editor({
      element: document.createElement("div"),
      extensions: editorExtensions(),
      content: { type: "doc", content: [{ type: "taskList", content: [item({ checked: false, taskId: notes }, [{ type: "text", text: "Notes" }])] }] },
    });
    editor.commands.focus("end");
    editor.commands.splitListItem("taskItem");
    editor.commands.insertContent("Room");
    const items = (editor.getJSON().content?.[0]?.content ?? []) as DocNode[];
    expect(items.map((each) => each.attrs?.taskId)).toEqual([notes, null]);
    editor.destroy();
  });

  it("does not take an id from markup, so a pasted copy is a task of its own", () => {
    const editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions() });
    editor.commands.setContent(`<ul data-type="taskList"><li data-type="taskItem" data-checked="false" data-task-id="${notes}"><p>Copied</p></li></ul>`);
    const first = editor.getJSON().content?.[0]?.content?.[0] as DocNode;
    expect(first.type).toBe("taskItem");
    expect(first.attrs?.taskId).toBeNull();
    editor.destroy();
  });
});

const page: Doc = {
  type: "doc",
  content: [
    {
      type: "taskList",
      content: [
        item({ checked: false, taskId: notes }, [
          { type: "text", text: "Send the notes " },
          { type: "date", attrs: { date: "2020-01-01" } },
        ]),
        item({ checked: false }, [{ type: "text", text: "Never published" }]),
        item({ checked: true, taskId: room }, [{ type: "text", text: "Book a room" }]),
      ],
    },
  ],
};

describe("a task on the page", () => {
  it("is ticked off where the reader may edit the page, and says when it is overdue", async () => {
    const toggleTask = vi.fn();
    render(
      <DocPageContext value={{ id: minutes, spaceKey: "TEAM", toggleTask }}>
        <DocView doc={page} />
      </DocPageContext>,
    );
    const box = screen.getByRole("checkbox", { name: "Done: Send the notes 2020-01-01" });
    expect(box).toBeEnabled();
    await userEvent.click(box);
    expect(toggleTask).toHaveBeenCalledWith(notes, true);
    await userEvent.click(screen.getByRole("checkbox", { name: "Done: Book a room" }));
    expect(toggleTask).toHaveBeenLastCalledWith(room, false);
    expect(screen.getByRole("checkbox", { name: "Done: Never published" })).toBeDisabled();
    expect(box.closest("li")?.querySelector("[data-task-due]")).toHaveTextContent("Overdue");
    expect(screen.getByRole("checkbox", { name: "Done: Book a room" }).closest("li")?.querySelector("[data-task-due]")).toBeNull();
  });

  it("is still for a reader who may not edit", () => {
    render(<DocView doc={page} />);
    for (const box of screen.getAllByRole("checkbox")) expect(box).toBeDisabled();
  });
});

describe("my tasks", () => {
  it("lists the open tasks with their page and day, ticks one off and says so", async () => {
    const today = utcToday();
    const sent = stubApi({
      "GET /tasks": (request) => {
        const state = new URL(request.url).searchParams.get("state");
        if (state === "done")
          return {
            status: 200,
            body: { tasks: [aTask({ id: room, text: "Book a room", done: true, doneAt: "2026-10-01T10:00:00Z", dueOn: null })], next: null },
          };
        return {
          status: 200,
          body: {
            tasks: [
              aTask(),
              aTask({
                id: room,
                text: "Book a room",
                dueOn: today,
                canEdit: false,
                page: { id: minutes, title: "Minutes", spaceKey: "TEAM", spaceName: "Team" },
              }),
            ],
            next: null,
          },
        };
      },
      [`PATCH /pages/${minutes}/tasks/${notes}`]: { status: 200, body: { task: aTask({ done: true, doneAt: "2026-10-02T09:00:00Z" }) } },
    });
    await renderAt("/tasks");
    const row = (await screen.findByText("Send the notes @Ada 2026-10-01")).closest("li")!;
    expect(within(row).getByRole("link", { name: "Minutes" })).toHaveAttribute("href", `/s/TEAM/p/${minutes}/minutes`);
    expect(row.querySelector("[data-task-due]")).toHaveAttribute("data-task-due", "overdue");
    expect(within(row).getByText("Assigned by Grace Hopper")).toBeInTheDocument();
    const theirs = screen.getByRole("checkbox", { name: "Done: Book a room" });
    expect(theirs).toBeDisabled();
    expect(theirs).toHaveAccessibleDescription("You may not edit this page, so you cannot tick this task off here.");
    expect(theirs.closest("li")?.querySelector("[data-task-due]")).toHaveTextContent("Due today");
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(row).getByRole("checkbox", { name: "Done: Send the notes @Ada 2026-10-01" }));
    await waitFor(() => expect(sent.some((each) => each.method === "PATCH")).toBe(true));
    expect(sent.find((each) => each.method === "PATCH")?.body).toEqual({ done: true });
    expect(await screen.findByText("Send the notes @Ada 2026-10-01 is done.")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("tab", { name: "Done" }));
    const doneRow = (await screen.findByText("Book a room")).closest("li")!;
    expect(doneRow).toHaveAttribute("data-task-done", "true");
    expect(within(doneRow).getByText(/^Done /)).toBeInTheDocument();
    expect(within(doneRow).getByRole("checkbox", { name: "Done: Book a room" })).toBeChecked();
    expect(sent.some((each) => each.method === "GET" && each.path === "/tasks")).toBe(true);
  });

  it("reads the next window when asked, from the cursor it was given", async () => {
    const queries: URLSearchParams[] = [];
    stubApi({
      "GET /tasks": (request) => {
        const query = new URL(request.url).searchParams;
        queries.push(query);
        return query.get("cursor")
          ? { status: 200, body: { tasks: [aTask({ id: room, text: "Book a room" })], next: null } }
          : { status: 200, body: { tasks: [aTask()], next: "c1" } };
      },
    });
    await renderAt("/tasks");
    await screen.findByText("Send the notes @Ada 2026-10-01");
    await userEvent.click(screen.getByRole("button", { name: "Show more" }));
    expect(await screen.findByText("Book a room")).toBeInTheDocument();
    expect(queries[0]?.get("limit")).toBe(String(TASKS_PAGE_SIZE));
    expect(queries.at(-1)?.get("cursor")).toBe("c1");
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("says when there is nothing, and when the list could not be read", async () => {
    stubApi({ "GET /tasks": { status: 200, body: { tasks: [], next: null } } });
    await renderAt("/tasks");
    expect(await screen.findByText(/No open tasks\./)).toBeInTheDocument();
    vi.unstubAllGlobals();
    stubApi({ "GET /tasks": { status: 500, body: { error: { code: "internal", message: "Broken." } } } });
    await renderAt("/tasks");
    expect(await screen.findByText("Your tasks could not be loaded. Try again in a moment.")).toBeInTheDocument();
  });
});
