import { describe, expect, it } from "vitest";
import { isTaskAnchor, taskAnchor } from "./taskAnchor";

describe("a task's anchor", () => {
  it("is the task's id behind a prefix, and is told from a heading's anchor", () => {
    const id = "0195f000-0000-7000-8000-00000000c0a1";
    expect(taskAnchor(id)).toBe(`task-${id}`);
    expect(isTaskAnchor(taskAnchor(id))).toBe(true);
    expect(isTaskAnchor("task-")).toBe(false);
    expect(isTaskAnchor("tasks-and-chores")).toBe(false);
    expect(isTaskAnchor("")).toBe(false);
  });
});
