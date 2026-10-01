import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axeViolations } from "@/test/axe";
import { Editor } from "./Editor";
import type { Doc } from "./schema";

beforeEach(() => localStorage.clear());
afterEach(cleanup);

const page: Doc = {
  type: "doc",
  content: [
    {
      type: "paragraph",
      content: [
        { type: "text", text: "Draft one and a " },
        { type: "text", text: "dra", marks: [{ type: "bold" }] },
        { type: "text", text: "ft." },
      ],
    },
    { type: "paragraph", content: [{ type: "text", text: "The last draft." }] },
  ],
};

function setup(variant: "page" | "comment" = "page") {
  const user = userEvent.setup();
  const onChange = vi.fn();
  render(
    <main>
      <h1>Notes</h1>
      <Editor id="page-body" value={page} onChange={onChange} variant={variant} />
    </main>,
  );
  const box = document.getElementById("page-body")!;
  const text = () => box.textContent;
  return { user, box, text };
}

async function openFind(user: ReturnType<typeof userEvent.setup>, box: HTMLElement) {
  await user.click(box);
  await user.keyboard("{Control>}f{/Control}");
  return screen.findByRole("search", { name: "Find and replace" });
}

describe("find and replace", () => {
  it("opens with Ctrl+F in the editor, counts the matches and steps through them", async () => {
    const { user, box } = setup();
    const bar = await openFind(user, box);
    const query = within(bar).getByRole("searchbox", { name: "Find" });
    expect(query).toHaveFocus();
    await user.keyboard("draft");
    const status = within(bar).getByRole("status");
    expect(status).toHaveTextContent(/of 3 matches$/);
    expect(box.querySelectorAll("[data-find-match]").length).toBeGreaterThanOrEqual(3);
    const at = () => Number(/^(\d+)/.exec(status.textContent ?? "")?.[1]);
    const first = at();
    await user.keyboard("{Enter}");
    expect(at()).toBe((first % 3) + 1);
    await user.keyboard("{Shift>}{Enter}{/Shift}");
    expect(at()).toBe(first);
    await user.click(within(bar).getByRole("button", { name: "Next match" }));
    expect(at()).toBe((first % 3) + 1);
  });

  it("matches case only when asked, and says how to find more", async () => {
    const { user, box } = setup();
    const bar = await openFind(user, box);
    await user.keyboard("DRAFT");
    await user.click(within(bar).getByRole("checkbox", { name: "Match case" }));
    expect(within(bar).getByRole("status")).toHaveTextContent("No matches. Turn off Match case to find more.");
    expect(within(bar).getByRole("button", { name: "Replace all" })).toBeDisabled();
    await user.click(within(bar).getByRole("checkbox", { name: "Match case" }));
    expect(within(bar).getByRole("status")).toHaveTextContent(/of 3 matches$/);
  });

  it("replaces one, then all, and one undo takes the rest back", async () => {
    const { user, box, text } = setup();
    const bar = await openFind(user, box);
    await user.keyboard("draft");
    await user.type(within(bar).getByRole("textbox", { name: "Replace with" }), "plan");
    await user.click(within(bar).getByRole("button", { name: "Replace" }));
    expect(within(bar).getByRole("status")).toHaveTextContent(/of 2 matches$/);
    await user.click(within(bar).getByRole("button", { name: "Replace all" }));
    expect(within(bar).getByRole("status")).toHaveTextContent("Replaced 2 matches.");
    expect(text()).not.toMatch(/draft/i);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("search")).toBeNull();
    expect(box).toHaveFocus();
    await user.keyboard("{Control>}z{/Control}");
    expect(text()?.match(/plan/gi)).toHaveLength(1);
    expect(box.querySelectorAll("[data-find-match]")).toHaveLength(0);
  });

  it("opens and closes from the toolbar", async () => {
    const { user, box } = setup();
    await user.click(box);
    const find = screen.getByRole("button", { name: "Find and replace" });
    expect(find).toHaveAttribute("aria-keyshortcuts", "Control+F");
    expect(find).toHaveAttribute("aria-expanded", "false");
    await user.click(find);
    expect(find).toHaveAttribute("aria-expanded", "true");
    const bar = screen.getByRole("search", { name: "Find and replace" });
    await user.click(within(bar).getByRole("button", { name: "Close find and replace" }));
    expect(screen.queryByRole("search")).toBeNull();
    expect(box).toHaveFocus();
  });

  it("leaves Ctrl+F alone outside the editor and in a comment", async () => {
    const { user } = setup("comment");
    const box = document.getElementById("page-body")!;
    await user.click(box);
    const pressed = new KeyboardEvent("keydown", { key: "f", ctrlKey: true, bubbles: true, cancelable: true });
    box.dispatchEvent(pressed);
    expect(pressed.defaultPrevented).toBe(false);
    expect(screen.queryByRole("search")).toBeNull();
    expect(screen.queryByRole("button", { name: "Find and replace" })).toBeNull();
    cleanup();

    setup();
    const outside = new KeyboardEvent("keydown", { key: "f", ctrlKey: true, bubbles: true, cancelable: true });
    document.body.dispatchEvent(outside);
    expect(outside.defaultPrevented).toBe(false);
    expect(screen.queryByRole("search")).toBeNull();
  });

  it("passes axe with the bar open and matches drawn", async () => {
    const { user, box } = setup();
    await openFind(user, box);
    await user.keyboard("draft");
    expect(await axeViolations()).toEqual([]);
  });
});
