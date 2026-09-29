import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";

beforeEach(() => localStorage.clear());
afterEach(cleanup);

// jsdom does not place a caret on click, so the selection is set the way a
// browser would report it after one.
function placeCaretIn(el: Element) {
  el.closest<HTMLElement>("[contenteditable]")?.focus();
  const range = document.createRange();
  range.selectNodeContents(el);
  range.collapse(true);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(range);
  act(() => {
    document.dispatchEvent(new Event("selectionchange"));
  });
}

async function openEditor() {
  await renderAt("/dev/editor");
  const box = await screen.findByRole("textbox", { name: "Page content" });
  return { user: userEvent.setup(), box };
}

describe("the editor's development page", () => {
  it("is reachable but linked from nowhere", async () => {
    await openEditor();
    expect(document.querySelector('a[href="/dev/editor"]')).toBeNull();
  });

  it("passes axe with its toolbar, its panels, its code and its read-only view", async () => {
    await openEditor();
    expect(screen.getAllByRole("note", { name: "Info" }).length).toBeGreaterThan(0);
    expect(await axeViolations()).toEqual([]);
  });

  it("passes axe with the slash menu open", async () => {
    const { user, box } = await openEditor();
    await user.click(box);
    await user.keyboard("/");
    await screen.findByRole("listbox", { name: "Insert a block" });
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.keyboard("zzzz");
    await screen.findByText("No block matches. Keep typing, or press Escape.");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
  });

  it("passes axe with the table, code and panel tools out", async () => {
    const { user, box } = await openEditor();
    await user.click(box);
    await user.keyboard("/table{Enter}");
    const tools = await screen.findByRole("toolbar", { name: "Table" });
    // A header with nothing in it names no column; a real table has words there.
    await user.keyboard("Name{Tab}Role{Tab}Team");
    expect(await axeViolations()).toEqual([]);
    await user.click(within(tools).getByRole("button", { name: "Cell background" }));
    await screen.findByRole("menu", { name: "Cell background" });
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.keyboard("{Escape}");

    placeCaretIn(box.querySelector("pre code")!);
    await screen.findByRole("toolbar", { name: "Code block" });
    expect(await axeViolations()).toEqual([]);

    placeCaretIn(box.querySelector("[data-panel] p")!);
    await screen.findByRole("toolbar", { name: "Panel" });
    expect(await axeViolations()).toEqual([]);
  });

  it("gives every control on the page a name", async () => {
    await openEditor();
    for (const button of screen.getAllByRole("button")) expect(button).toHaveAccessibleName();
    for (const toolbar of screen.getAllByRole("toolbar")) expect(toolbar).toHaveAccessibleName();
  });
});
